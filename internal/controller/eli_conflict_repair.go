package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/conflictresolve"
	"github.com/eyupio/zoomies/internal/gitfetch"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

const (
	// conflictClaimWindow keeps an observation from starting a second worker
	// for the same head, base and merge base while the first is still being
	// merged or resolved.
	conflictClaimWindow = 15 * time.Minute
	// A conflict no model may resolve, or one whose resolution cannot be
	// applied, is not retried every couple of minutes: it needs a person, and
	// the watch still records fresh evidence while it waits.
	conflictPermanentCooldown = time.Hour
	conflictPrepareTimeout    = 3 * time.Minute
	conflictModelTimeout      = 10 * time.Minute
	conflictLoopInterval      = 2 * time.Second
)

// conflictJob is the incident a prepared repository belongs to. It is kept
// beside the attempt so the answer can be resolved and published after the
// agent has replied, without the HTTP request that carried the answer knowing
// anything about watchers or providers.
type conflictJob struct {
	WatchID        string
	InstallationID string
	Repo           string
	PullNumber     int
	HeadSHA        string
	BaseSHA        string
	MergeBaseSHA   string
}

func (j *conflictJob) key() string {
	return strings.Join([]string{j.WatchID, j.HeadSHA, j.BaseSHA, j.MergeBaseSHA}, "|")
}

func conflictKey(watchID, head, base, mergeBase string) string {
	return strings.Join([]string{watchID, head, base, mergeBase}, "|")
}

// claimConflict reserves one conflict incident until a deadline. A second
// observation of the same head/base pair is left alone while the first worker
// or the model still holds it.
func (c *Controller) claimConflict(key string, until time.Time) bool {
	c.conflictMu.Lock()
	defer c.conflictMu.Unlock()
	now := c.Now()
	if expiry, ok := c.conflictClaims[key]; ok && now.Before(expiry) {
		return false
	}
	// A hold that was never released (the hour a conflict no model may resolve
	// waits for a person) would otherwise stay in the map for the life of the
	// process, one entry per head and base ever seen.
	for held, expiry := range c.conflictClaims {
		if !now.Before(expiry) {
			delete(c.conflictClaims, held)
		}
	}
	c.conflictClaims[key] = until
	return true
}

// conflictClaimed reports whether an incident is still held, which is what
// tells an observation to keep its next check short without starting again.
func (c *Controller) conflictClaimed(key string) bool {
	c.conflictMu.Lock()
	defer c.conflictMu.Unlock()
	expiry, ok := c.conflictClaims[key]
	return ok && c.Now().Before(expiry)
}

func (c *Controller) releaseConflict(key string) {
	c.conflictMu.Lock()
	defer c.conflictMu.Unlock()
	delete(c.conflictClaims, key)
}

func (c *Controller) holdConflict(key string, for_ time.Duration) {
	c.conflictMu.Lock()
	defer c.conflictMu.Unlock()
	c.conflictClaims[key] = c.Now().Add(for_)
}

// conflictRepairLoop resolves and publishes answers that the agent worker has
// posted. It is separate from the model worker: a merge answer can arrive while
// an ordinary repair is generating, and neither should wait for the other.
func (c *Controller) conflictRepairLoop(ctx context.Context) {
	ticker := time.NewTicker(conflictLoopInterval)
	defer ticker.Stop()
	for {
		c.processPendingConflictAnswers(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Controller) processPendingConflictAnswers(ctx context.Context) {
	if ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
		return
	}
	for _, id := range c.merge.pending() {
		if ctx.Err() != nil || !c.mayAct() || c.transferDraining.Load() {
			return
		}
		c.processConflictAnswer(ctx, id)
	}
}

// startConflictRepair prepares one incident and hands it to a host that
// advertises the worker. The observation has already released its watch lease:
// a fetch of three archives must not hold the observation lease while a slow
// GitHub answers.
func (c *Controller) startConflictRepair(ctx context.Context, key string, raw github.Client, watch *store.EliRepairWatch, pull github.RepairPull, conflict github.RepairConflict) {
	host, ok := c.mergeHost(ctx)
	if !ok {
		c.log.Warn("no host can run the merge worker, so the conflict is not being repaired",
			"watch", watch.ID, "repo", watch.Repo, "pull", watch.PullNumber,
			"fix", "upgrade an agent to one that advertises "+agent.FeatureMergeWorker)
		c.releaseConflict(key)
		return
	}
	prepCtx, cancel := context.WithTimeout(ctx, conflictPrepareTimeout)
	defer cancel()
	prepared, err := c.merge.prepareFor(prepCtx, raw, watch.Repo,
		gitfetch.Pin{Ancestor: conflict.MergeBaseSHA, Base: conflict.BaseSHA, Head: conflict.HeadSHA},
		naming.GitWorkerImage(version.Channel(version.Version)),
		&conflictJob{
			WatchID:        watch.ID,
			InstallationID: watch.InstallationID,
			Repo:           watch.Repo,
			PullNumber:     watch.PullNumber,
			HeadSHA:        conflict.HeadSHA,
			BaseSHA:        conflict.BaseSHA,
			MergeBaseSHA:   conflict.MergeBaseSHA,
		})
	if err != nil {
		c.log.Warn("could not prepare the repository for a conflict repair",
			"watch", watch.ID, "repo", watch.Repo, "pull", watch.PullNumber, "error", err)
		c.releaseConflict(key)
		return
	}
	if !c.QueueMergeConflict(host.ID, prepared) {
		_ = c.merge.abandon(prepared.ID)
		c.releaseConflict(key)
		return
	}
	c.log.Info("started a conflict repair",
		"watch", watch.ID, "repo", watch.Repo, "pull", watch.PullNumber,
		"host", host.ID, "merge", prepared.ID, "head", conflict.HeadSHA,
		"base", conflict.BaseSHA, "merge_base", conflict.MergeBaseSHA)
}

// mergeHost picks a host that advertises the worker. A cordoned host is still
// eligible because cordoning stops new jobs, not the repair of one already open.
func (c *Controller) mergeHost(ctx context.Context) (*store.Host, bool) {
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		return nil, false
	}
	now := c.Now()
	candidates := make([]*store.Host, 0, len(hosts))
	for _, host := range hosts {
		if !host.Healthy(now) || !host.Supports(agent.FeatureMergeWorker) {
			continue
		}
		candidates = append(candidates, host)
	}
	if len(candidates) == 0 {
		return nil, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Cordoned != b.Cordoned {
			return !a.Cordoned
		}
		if a.ActiveRunners != b.ActiveRunners {
			return a.ActiveRunners < b.ActiveRunners
		}
		return a.ID < b.ID
	})
	return candidates[0], true
}

// processConflictAnswer resolves one worker answer and, if every gate still
// holds, publishes the two-parent merge commit.
func (c *Controller) processConflictAnswer(ctx context.Context, id string) {
	job, answer, ok := c.merge.takeAnswer(id)
	if !ok {
		return
	}
	defer func() { _ = c.merge.abandon(id) }()
	if job == nil {
		return
	}
	key := job.key()
	if answer.Result == nil {
		c.log.Warn("the merge worker could not merge a conflict repair",
			"watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber,
			"code", answer.Code, "said", answer.Error)
		c.releaseConflict(key)
		return
	}
	if answer.Result.Truncated || len(answer.Result.Unsupported) > 0 {
		c.log.Warn("the merge has a conflict a model may not resolve, so it needs a person",
			"watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber,
			"unsupported", len(answer.Result.Unsupported), "truncated", answer.Result.Truncated)
		c.holdConflict(key, conflictPermanentCooldown)
		return
	}
	watch, policy, installation, provider, err := c.currentConflictAuthority(ctx, job)
	if err != nil {
		c.log.Info("a conflict repair stopped before the model ran", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "reason", err)
		c.releaseConflict(key)
		return
	}
	raw, err := c.ClientFor(ctx, job.InstallationID)
	if err != nil {
		c.log.Warn("could not reach GitHub for a conflict repair", "watch", job.WatchID, "error", err)
		c.releaseConflict(key)
		return
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		c.releaseConflict(key)
		return
	}
	pull, err := client.RepairPull(ctx, job.Repo, job.PullNumber)
	if err != nil || pull.HeadSHA != job.HeadSHA || pull.BaseSHA != job.BaseSHA {
		c.log.Info("the pull request moved before the conflict could be resolved", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber)
		c.releaseConflict(key)
		return
	}
	providerAPI, err := c.OpenAssistantProvider(provider, "")
	if err != nil {
		c.log.Warn("could not open the provider for a conflict repair", "watch", job.WatchID, "error", err)
		c.releaseConflict(key)
		return
	}
	model := &repairModel{
		provider:  providerAPI,
		model:     provider.Model,
		kind:      assistant.Kind(provider.Kind),
		maxTokens: provider.MaxOutputTokens,
	}
	modelCtx, cancel := context.WithTimeout(ctx, conflictModelTimeout)
	resolved, err := conflictresolve.Resolve(modelCtx, model, answer.Result.Conflicts, policy.AllowWorkflows)
	cancel()
	if err != nil {
		c.logConflictFailure(job, key, err)
		return
	}
	merged, err := conflictresolve.ApplyChanges(answer.Result.Changes, resolved)
	if err != nil {
		c.log.Warn("a conflict resolution could not be put back into the merged tree", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "error", err)
		c.holdConflict(key, conflictPermanentCooldown)
		return
	}

	// The model may have taken minutes. Re-read the authority, the pull request
	// and the conflict before the ref moves; a pause, a provider change or a
	// target tip that moved discards the work.
	latestWatch, latestPolicy, latestInstallation, latestProvider, err := c.currentConflictAuthority(ctx, job)
	if err != nil {
		c.log.Info("a conflict repair stopped before publication", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "reason", err)
		c.releaseConflict(key)
		return
	}
	if latestWatch.HeadSHA != watch.HeadSHA || latestWatch.BaseSHA != watch.BaseSHA ||
		latestPolicy.AllowWorkflows != policy.AllowWorkflows ||
		!latestInstallation.UpdatedAt.Equal(installation.UpdatedAt) ||
		latestProvider.UpdatedAt != provider.UpdatedAt {
		c.log.Info("a conflict repair stopped because its authorisation changed", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber)
		c.releaseConflict(key)
		return
	}
	if !c.mayAct() || c.transferDraining.Load() || ctx.Err() != nil {
		c.releaseConflict(key)
		return
	}
	if conflictClient, ok := client.(github.RepairConflictClient); ok {
		now, err := conflictClient.RepairConflictEvidence(ctx, job.Repo, job.PullNumber)
		if err != nil || !now.MergeableKnown || now.Mergeable || now.MergeBaseSHA != job.MergeBaseSHA ||
			now.HeadSHA != job.HeadSHA || now.BaseSHA != job.BaseSHA {
			c.log.Info("the pull request is no longer conflicted in the incident this repair prepared", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber)
			c.releaseConflict(key)
			return
		}
	}
	writer, ok := client.(github.RepairConflictWriter)
	if !ok {
		c.releaseConflict(key)
		return
	}
	changes := make([]github.ConflictChange, 0, len(merged))
	for _, change := range merged {
		changes = append(changes, github.ConflictChange{
			Path: change.Path, Mode: change.Mode, Content: change.Content, SHA: change.SHA, Deleted: change.Deleted,
		})
	}
	sha, err := writer.CommitConflictResolution(ctx, job.Repo, pull, job.BaseSHA, changes)
	if err != nil {
		if errors.Is(err, github.ErrSetupConflict) {
			c.log.Info("the pull request moved while the conflict was being resolved", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber)
			c.releaseConflict(key)
			return
		}
		c.log.Warn("could not publish a conflict resolution", "watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "error", err)
		c.releaseConflict(key)
		return
	}
	c.releaseConflict(key)
	c.log.Info("published a conflict resolution",
		"watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "commit", sha,
		"files", len(changes), "conflicts", len(answer.Result.Conflicts))
}

// logConflictFailure classifies a model or resolution failure. A conflict no
// model can resolve waits for a person; an error reaching the provider is
// retried by the next observation.
func (c *Controller) logConflictFailure(job *conflictJob, key string, err error) {
	if errors.Is(err, conflictresolve.ErrUnsupported) || errors.Is(err, conflictresolve.ErrUnresolved) || errors.Is(err, prrepair.ErrCannotFix) {
		c.log.Warn("a conflict resolution cannot be used, so it needs a person",
			"watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "error", err)
		c.holdConflict(key, conflictPermanentCooldown)
		return
	}
	c.log.Warn("could not resolve a conflict with the model",
		"watch", job.WatchID, "repo", job.Repo, "pull", job.PullNumber, "error", err)
	c.releaseConflict(key)
}

// currentConflictAuthority is the gate a conflict repair checks before model
// work and again before publication: the watch is still enabled for conflicts,
// the policy is still automatic on the same provider, and the installation and
// provider are still the ones that were admitted.
func (c *Controller) currentConflictAuthority(ctx context.Context, job *conflictJob) (*store.EliRepairWatch, *store.EliRepairPolicy, *store.Installation, *store.AssistantProvider, error) {
	watch, err := c.st.EliRepairWatch(ctx, job.WatchID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("the watch is no longer available")
	}
	if !watch.Enabled || !watch.Conflicts || watch.StopReason != "" {
		return nil, nil, nil, nil, fmt.Errorf("the watch is paused or stops following conflicts")
	}
	if watch.HeadSHA != job.HeadSHA || watch.BaseSHA != job.BaseSHA {
		return nil, nil, nil, nil, fmt.Errorf("the watch moved to another head or base")
	}
	policy, err := c.repairPolicy(ctx, job.Repo, job.InstallationID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("the repository policy is no longer enabled")
	}
	if !policy.Automatic || policy.ProviderID != watch.PolicyProviderID {
		return nil, nil, nil, nil, fmt.Errorf("automatic repairs or their provider changed")
	}
	if canPay, err := c.automaticProviderCanPay(ctx, *policy); err != nil || !canPay {
		return nil, nil, nil, nil, fmt.Errorf("the installation provider is no longer available")
	}
	installation, err := c.st.GetInstallation(ctx, job.InstallationID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("the GitHub installation is no longer available")
	}
	provider, err := c.st.GetAssistantProvider(ctx, policy.ProviderID)
	if err != nil || !provider.Enabled || provider.OwnerID != "" {
		return nil, nil, nil, nil, fmt.Errorf("the provider is disabled or not a team provider")
	}
	return watch, policy, installation, provider, nil
}
