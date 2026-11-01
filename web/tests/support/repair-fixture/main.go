// The documentation's sample repair history. This runs only against the
// screenshot harness's disposable database while its controller is stopped.
// It neither calls a model nor contacts GitHub. Terminal states are saved
// before restart, so the repair worker has nothing to execute.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func main() {
	if err := seed(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func seed() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("expected the screenshot database path")
	}
	path := filepath.Clean(os.Args[1])
	if filepath.Base(path) != "zoomies.db" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "zoomies-screenshots-") {
		return fmt.Errorf("refusing to seed a database outside the screenshot fixture")
	}
	ctx := context.Background()
	// A clock that moves on each time it is read, so the samples began some time ago and
	// took a while, as repairs do, instead of all starting and ending in one instant.
	clock := time.Now().Add(-26 * time.Hour)
	st, err := store.Open(ctx, store.Options{Path: path, Now: func() time.Time {
		clock = clock.Add(41 * time.Second)
		return clock
	}})
	if err != nil {
		return err
	}
	defer st.Close()
	user, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		return err
	}
	inst, err := st.FindInstallationByTarget(ctx, "acme/widgets")
	if err != nil {
		return err
	}
	for _, sample := range []struct {
		pull                             int
		state, message, stage, failure   string
		commit, title, branch, job, step string
	}{
		{42, "succeeded", "Sample repair: corrected a failing assertion; the PR checks passed. Normal review is still required.", "checks_passed", "",
			"7c0d1ab9e3f24b6a1d8c5e0f7a2b4c6d8e1f3a5b", "Retry transient 502s in the registry client", "fix/registry-retry", "Go (api-2)", "Run go test -race ./internal/..."},
		{43, "blocked", "Sample repair: workflow edits are disabled for this repository. Review the policy before another attempt.", "prepare_fix", "unsupported_fix",
			"", "Pin the base image digest in the runner Dockerfile", "chore/pin-base-digest", "Build runner image", "docker build"},
		{44, "failed", "Sample repair: Eli could not produce a fix it was willing to push.", "prepare_fix", "unsupported_fix",
			"", "Show the retention window on the Usage page", "feat/usage-retention", "UI (e2e)", "Playwright"},
	} {
		// What the repair was about, kept as it was when it began, so the pages show the
		// pull request and the job that failed and not only a number.
		const run = 4820
		kept := &store.EliRepairCase{
			PullTitle: sample.title,
			PullURL:   fmt.Sprintf("https://github.com/acme/widgets/pull/%d", sample.pull),
			Author:    "alice", Branch: sample.branch,
			RunURL:   fmt.Sprintf("https://github.com/acme/widgets/actions/runs/%d", run),
			Workflow: "CI", RunNumber: run,
			Jobs: []store.EliRepairCaseJob{{
				Name: sample.job, Step: sample.step, DurationMS: 192000,
				URL: fmt.Sprintf("https://github.com/acme/widgets/actions/runs/%d/job/%d", run, 90000+sample.pull),
			}},
		}
		r := &store.EliRepair{
			DedupKey: fmt.Sprintf("screenshots:%d", sample.pull), InstallationID: inst.ID,
			Repo: "acme/widgets", PullNumber: sample.pull, RunID: run, UserID: user.ID,
			GitHubLogin: "alice", Trigger: "manual", CommitSHA: sample.commit,
			Details: store.EliRepairDetails{Model: "demo (sample history)", Stage: sample.stage, FailureCode: sample.failure, Case: kept},
		}
		// A repair that got as far as the model made calls, and one provider did not report usage
		// for, which the page says is unknown and never shows as zero.
		if sample.state != "blocked" {
			r.Details.SourceBytes = 38912
			r.Details.Steps = []store.EliRepairStep{
				{InputTokens: 21400, OutputTokens: 1800, UsageReported: true, RequestBytes: 80312, ElapsedMS: 41000},
				{RequestBytes: 91234, ElapsedMS: 9000},
			}
		}
		if _, err := st.EnqueueEliRepair(ctx, r); err != nil {
			return err
		}
		r.State, r.Message = sample.state, sample.message
		if err := st.SaveEliRepair(ctx, r); err != nil {
			return err
		}
	}
	return seedAutomatic(ctx, st, inst.ID, &clock)
}

// seedAutomatic makes two repositories repair themselves, with an installation
// provider to pay for it and a few repairs the policy started, so the hub shows its
// Automatic repairs panel and an attempt shows the card that says where its policy
// stands. Every repair is terminal, so the worker has nothing to run.
func seedAutomatic(ctx context.Context, st *store.Store, installation string, clock *time.Time) error {
	provider := &store.AssistantProvider{Name: "Team provider (sample)", Kind: "fake", Model: "demo", Enabled: true}
	if err := st.CreateAssistantProvider(ctx, provider); err != nil {
		return err
	}
	for _, repo := range []string{"acme/gateway", "acme/docs"} {
		if err := st.SetEliRepairPolicy(ctx, store.EliRepairPolicy{
			Repo: repo, InstallationID: installation, ProviderID: provider.ID,
			Enabled: true, Automatic: true,
		}); err != nil {
			return err
		}
	}
	*clock = time.Now().Add(-5 * time.Hour)
	for _, sample := range []struct {
		repo                             string
		pull                             int
		state, message, stage, failure   string
		commit, title, branch, job, step string
	}{
		{"acme/gateway", 109, "checks_failed", "Sample repair: Eli's commit did not make the checks pass. Automatic repair does not try the same commit twice; a maintainer can ask again.", "watch_ci", "",
			"3a9e5c7d1f0b2468ace02468ace13579bdf02468", "Cap the retry budget in the gateway client", "feat/gateway-retry-cap", "Go (gateway)", "Run go test ./..."},
		{"acme/gateway", 112, "succeeded", "Sample repair: restored the missing nil check; the PR checks passed. Normal review is still required.", "checks_passed", "",
			"b2d4f6081a3c5e7092b4d6f8a0c2e4681a3c5e70", "Add a retry budget to the gateway client", "feat/gateway-retry-budget", "Go (gateway)", "Run go test ./..."},
		{"acme/docs", 31, "failed", "Sample repair: Eli could not produce a fix it was willing to push.", "prepare_fix", "unsupported_fix",
			"", "Fix the broken link check on the install page", "docs/fix-link-check", "Docs build", "mkdocs build --strict"},
	} {
		const run = 5210
		// A commit that looks like one: forty hex digits with no run of zeros at the front.
		head := fmt.Sprintf("%016x%016x%08x", uint64(sample.pull)*0x9e3779b97f4a7c15, uint64(sample.pull)*0xc2b2ae3d27d4eb4f, uint32(sample.pull)*0x85ebca6b)
		kept := &store.EliRepairCase{
			PullTitle: sample.title,
			PullURL:   fmt.Sprintf("https://github.com/%s/pull/%d", sample.repo, sample.pull),
			Author:    "alice", Branch: sample.branch,
			RunURL:   fmt.Sprintf("https://github.com/%s/actions/runs/%d", sample.repo, run+sample.pull),
			Workflow: "CI", RunNumber: run + int64(sample.pull),
			Jobs: []store.EliRepairCaseJob{{
				Name: sample.job, Step: sample.step, DurationMS: 143000,
				URL: fmt.Sprintf("https://github.com/%s/actions/runs/%d/job/%d", sample.repo, run+sample.pull, 91000+sample.pull),
			}},
		}
		r := &store.EliRepair{
			DedupKey: "automatic:" + sample.repo + ":" + head, InstallationID: installation,
			Repo: sample.repo, PullNumber: sample.pull, RunID: int64(run + sample.pull), HeadSHA: head,
			ProviderID: provider.ID, Trigger: "automatic", CommitSHA: sample.commit,
			Details: store.EliRepairDetails{Model: "demo (sample history)", Stage: sample.stage, FailureCode: sample.failure, Case: kept,
				SourceBytes: 27648,
				Steps:       []store.EliRepairStep{{InputTokens: 18200, OutputTokens: 1400, UsageReported: true, RequestBytes: 61440, ElapsedMS: 33000}},
			},
		}
		if _, err := st.EnqueueEliRepair(ctx, r); err != nil {
			return err
		}
		r.State, r.Message = sample.state, sample.message
		if err := st.SaveEliRepair(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
