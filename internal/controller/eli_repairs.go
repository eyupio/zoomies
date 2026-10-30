package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/prrepair/mention"
	"github.com/eyupio/zoomies/internal/redact"
	"github.com/eyupio/zoomies/internal/store"
)

func (c *Controller) repairPolicy(ctx context.Context, repo, installation string) (*store.EliRepairPolicy, error) {
	policies, err := c.st.EliRepairPolicies(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range policies {
		if p.Repo == repo && p.InstallationID == installation && p.Enabled {
			return &p, nil
		}
	}
	return nil, store.ErrNotFound
}

// This runs only after the normal webhook HMAC verification. Model work stays off the HTTP path.
func (c *Controller) enqueueRepairWebhook(ctx context.Context, inst *store.Installation, event string, body []byte) error {
	if inst == nil {
		return nil
	}
	var envelope struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return errMalformedDelivery
	}
	policy, err := c.repairPolicy(ctx, envelope.Repository.FullName, inst.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	r := &store.EliRepair{InstallationID: inst.ID, Repo: policy.Repo, ProviderID: policy.ProviderID}
	// refuse is set for a command a person typed, and answers it on the PR. The
	// other triggers have nobody to answer.
	var refuse func(reason string)
	switch event {
	case "issue_comment":
		var p struct {
			Action string `json:"action"`
			Issue  struct {
				Number      int              `json:"number"`
				PullRequest *json.RawMessage `json:"pull_request"`
			} `json:"issue"`
			Comment struct {
				ID   int64  `json:"id"`
				Body string `json:"body"`
				User struct {
					ID    int64  `json:"id"`
					Login string `json:"login"`
					Type  string `json:"type"`
				} `json:"user"`
			} `json:"comment"`
			Sender struct {
				ID int64 `json:"id"`
			} `json:"sender"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return errMalformedDelivery
		}
		command, ok := mention.Command(p.Comment.Body, inst.AppSlug)
		if !ok || p.Action != "created" || p.Issue.PullRequest == nil || p.Comment.ID <= 0 || p.Issue.Number <= 0 || p.Comment.User.Type != "User" || p.Sender.ID != p.Comment.User.ID {
			return nil
		}
		identities, err := c.st.EliIdentities(ctx)
		if err != nil {
			return err
		}
		linked := false
		for _, id := range identities {
			if id.GitHubUserID != p.Comment.User.ID {
				continue
			}
			linked = true
			if id.Confirmed {
				r.UserID = id.UserID
				break
			}
		}
		// GitHub is asked who the commenter is, and whether they may write, before
		// anything is said back. A command from someone who cannot write is dropped
		// without a word, linked or not: a comment from anyone must not make the
		// App post, and unlinked strangers cannot consume another user's provider or
		// fill the repair budget. Everybody who gets past this is somebody Eli may
		// answer, and a refusal below is never silent for them.
		admissionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		raw, err := c.ClientFor(admissionCtx, inst.ID)
		if err != nil {
			cancel()
			if r.UserID == "" {
				// Nothing is owed to a commenter Eli does not know, so a GitHub that
				// cannot be reached is not a delivery worth redelivering for them.
				return nil
			}
			return err
		}
		client, supported := raw.(github.RepairClient)
		if !supported {
			cancel()
			return nil
		}
		actor, err := client.RepairActor(admissionCtx, r.Repo, p.Comment.User.Login)
		cancel()
		if err != nil {
			if r.UserID == "" {
				return nil
			}
			return err
		}
		if actor.ID != p.Comment.User.ID || !actor.CanWrite {
			return nil
		}
		commentID, pull, login := p.Comment.ID, p.Issue.Number, p.Comment.User.Login
		refuse = func(reason string) {
			c.refuseRepairCommand(ctx, client, r.Repo, pull, commentID, reason)
		}
		if r.UserID == "" {
			if linked {
				refuse(repairNotAllowedReason(login))
			} else {
				refuse(repairNotLinkedReason(login))
			}
			return nil
		}
		user, err := c.st.GetUser(ctx, r.UserID)
		if errors.Is(err, store.ErrNotFound) || err == nil && user.Disabled {
			refuse(repairDisabledReason(login))
			return nil
		}
		if err != nil {
			return err
		}
		r.PullNumber = pull
		r.GitHubUserID = p.Comment.User.ID
		r.GitHubLogin = login
		r.Trigger = "mention"
		r.Instruction = command[:min(len(command), 4000)]
		r.DedupKey = fmt.Sprintf("comment:%s:%d", r.Repo, p.Comment.ID)
	case "workflow_job":
		if !policy.Automatic && !policy.AskFirst {
			return nil
		}
		e, err := github.ParseWorkflowJob(body)
		if err != nil {
			return errMalformedDelivery
		}
		if e.Action != "completed" || e.Conclusion != "failure" && e.Conclusion != "timed_out" || e.HeadSHA == "" || e.RunID <= 0 {
			return nil
		}
		ownCommit, err := c.st.IsEliRepairCommit(ctx, r.Repo, e.HeadSHA)
		if err != nil {
			return err
		}
		if ownCommit {
			return nil
		}
		// A provider that cannot pay would only queue an attempt that fails at once,
		// spend a place of the allowance and comment on the pull request. The
		// repository's panel already tells an administrator why nothing starts.
		if policy.Automatic {
			canPay, err := c.automaticProviderCanPay(ctx, *policy)
			if err != nil {
				return err
			}
			if !canPay {
				c.log.Info("Eli automatic repair skipped: the repository's provider cannot pay", "repo", r.Repo)
				return nil
			}
		}
		// A failing default-branch run must not spend a PR's budget. GitHub's job
		// delivery has no PR association, so resolve the pinned run before admission.
		admissionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		raw, err := c.ClientFor(admissionCtx, inst.ID)
		if err != nil {
			cancel()
			return err
		}
		client, supported := raw.(github.RepairClient)
		if !supported {
			cancel()
			return nil
		}
		pull, err := client.RepairPullForRun(admissionCtx, r.Repo, e.RunID)
		cancel()
		if errors.Is(err, github.ErrRepairUnsupported) {
			return nil
		}
		if err != nil {
			return err
		}
		if pull.HeadSHA != e.HeadSHA {
			return nil
		}
		if policy.AskFirst && !policy.Automatic {
			return c.st.OfferEliRepair(ctx, store.EliRepairOpportunity{Repo: r.Repo, InstallationID: inst.ID, PullNumber: pull.Number, HeadSHA: pull.HeadSHA, RunID: e.RunID, Title: pull.Title})
		}
		r.PullNumber = pull.Number
		r.JobID = e.JobID
		r.RunID = e.RunID
		r.HeadSHA = e.HeadSHA
		r.Trigger = "automatic"
		r.Instruction = "Diagnose the failed PR job and fix its underlying code issue."
		r.DedupKey = "automatic:" + r.Repo + ":" + e.HeadSHA
	default:
		return nil
	}
	_, err = c.st.EnqueueEliRepair(ctx, r, policy.DailyLimit)
	if errors.Is(err, store.ErrConflict) {
		c.log.Info("Eli repair budget reached", "repo", r.Repo, "trigger", r.Trigger)
		if refuse != nil {
			refuse(repairBudgetReason(err, policy.DailyLimit))
		}
		return nil
	}
	return err
}

// refuseRepairCommand answers a command Eli recognised from somebody who may
// write to the repository and then declined. Until it did, the person got
// nothing: no reaction, no comment, and a delivery the log called accepted, so a
// refusal looked exactly like a webhook that never arrived. The face is the
// quick signal and the comment says why. Both are best effort, since a repair
// that was never going to start has nothing to fail by a comment GitHub refused.
func (c *Controller) refuseRepairCommand(ctx context.Context, client github.RepairClient, repo string, pull int, commentID int64, reason string) {
	c.log.Info("Eli did not start a requested repair", "repo", repo, "pull", pull, "reason", reason)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := client.RepairReact(ctx, repo, commentID, "confused"); err != nil {
		c.log.Warn("could not react to a refused repair request", "repo", repo, "pull", pull)
	}
	text := "### 🐶 Eli could not start that\n\n" + reason + "\n\n<sub>🐾 Zoomies. Ask again with `/eli fix` once that is sorted.</sub>"
	if _, err := client.RepairComment(ctx, repo, pull, 0, text); err != nil {
		c.log.Warn("could not tell a commenter why their repair did not start", "repo", repo, "pull", pull)
	}
}

// The reasons below are written for the person who typed the command, and each
// names what they or an administrator can do, because "no" with nothing to try
// is the silence this replaces. Where the setup lives is the Eli AI Assistant
// page, under Eli repairs.

func repairNotLinkedReason(login string) string {
	return fmt.Sprintf("Your GitHub account `@%s` is not linked to a Zoomies user, so Eli cannot tell whose provider and limits this request would use. Ask a Zoomies administrator to link it on the Eli AI Assistant page, under Eli repairs; you then allow the link there yourself, and can ask again.", login)
}

func repairNotAllowedReason(login string) string {
	return fmt.Sprintf("Your GitHub account `@%s` is linked to a Zoomies user, but they have not allowed it to request repairs. They can turn on **Allow this GitHub account to request repairs using my provider** on the Eli AI Assistant page, under Eli repairs. An administrator cannot do that for them.", login)
}

func repairDisabledReason(login string) string {
	return fmt.Sprintf("The Zoomies account linked to `@%s` is disabled or no longer exists, so Eli cannot act for it. Ask a Zoomies administrator to enable it or to link your GitHub account to another.", login)
}

// repairBudgetReason says which of the two limits refused the request. The
// repository's is shared with automatic repairs, which on a repository whose CI
// fails often can use it up before anyone asks, so it says so, and says who can
// raise it.
func repairBudgetReason(err error, repoLimit int) string {
	if errors.Is(err, store.ErrEliPersonBudget) {
		return fmt.Sprintf("You have already asked for %s in the last 24 hours, which is the limit for each person. Ask again when your oldest request is more than 24 hours old.", plural(store.EliPersonDailyLimit, "repair"))
	}
	return fmt.Sprintf("This repository has used its %s for the last 24 hours. Explicit requests and automatic repairs share that limit, and an attempt frees its place when it is more than 24 hours old. An administrator can raise the limit, up to 50, under Repository repair policies on the Eli AI Assistant page.", plural(repoLimit, "repair attempt"))
}

// repairOutputTokens is the room a repair asks a model for when its provider
// leaves the limit to Eli, and where an automatic limit starts.
const repairOutputTokens = 12000

type repairModel struct {
	provider assistant.Provider
	model    string
	// kind is what the provider is, which decides what the messages that end a
	// repair may advise: only some kinds can be told to think less.
	kind       assistant.Kind
	record     func(store.EliRepairStep)
	rejected   func(error)
	sourceUsed func(int)
	// maxTokens is the limit the provider sets, or zero when it leaves the limit
	// to Eli. A number is kept exactly, because it may be there to cap what an
	// attempt costs. Zero is automatic: room starts at repairOutputTokens and
	// completeWithRoom raises it when the model runs out.
	maxTokens int
	// raised is the room an automatic repair has found it needs. It is kept for
	// the steps after the one that needed it, so that only that step pays to find
	// out, and given up if a provider refuses it beside a longer prompt.
	raised int
}

func (m *repairModel) SourceUsed(n int) {
	if m.sourceUsed != nil {
		m.sourceUsed(n)
	}
}

func (m *repairModel) ReplyRejected(err error) {
	if m.rejected != nil {
		m.rejected(err)
	}
}

// room is the ceiling to ask the provider for now: the operator's number when
// they set one, and otherwise where an automatic repair has got to.
func (m *repairModel) room() int {
	switch {
	case m.maxTokens > 0:
		return m.maxTokens
	case m.raised > 0:
		return m.raised
	}
	return repairOutputTokens
}

// Answer asks the model for the next repair step. A repair offers the model no
// tools: every step is a JSON object the system prompt describes. Some models
// answer with a tool call anyway (Ollama cloud did, for a repair whose only
// failing check was an audit script), because the prompt's own replies,
// {"read":[...]} and {"windows":[...]}, look like functions to a model trained
// to call them. A call named after a reply says what that reply would have, so
// it is read as the reply, and the repair goes on. Any other call is answered
// once, the way the chat loop answers a tool it was not given: a result for the
// call it made, saying there are no tools and what to send instead. A model
// that calls again is stopped, and the failure names the tool, so an operator
// can tell a model ignoring its instructions from a prompt that was wrong.
func (m *repairModel) Answer(ctx context.Context, messages []prrepair.Message) (string, error) {
	turns := make([]assistant.Message, 0, len(messages)+2)
	for _, v := range messages {
		turns = append(turns, assistant.Message{Role: assistant.Role(v.Role), Content: v.Content})
	}
	for attempt := 0; ; attempt++ {
		text, calls, err := m.completeWithRoom(ctx, turns)
		if err != nil {
			return "", err
		}
		if len(calls) == 0 {
			return text, nil
		}
		// Only when the model said nothing else: a call beside a reply of its own
		// leaves two answers, and the one it wrote out is the one it meant.
		if strings.TrimSpace(text) == "" {
			if reply, ok := prrepair.ReplyFromCalls(repairCalls(calls)); ok {
				return reply, nil
			}
		}
		if attempt > 0 {
			return "", fmt.Errorf("the provider answered with a tool call named %s, twice, instead of a repair plan; Eli offers no tools during a repair, so the model is ignoring its instructions: choose a model that keeps to a JSON reply, or test the provider in Eli settings", repairToolNames(calls))
		}
		// Every call gets a result of its own: the OpenAI protocol refuses a
		// transcript in which a call went unanswered.
		turns = append(turns, assistant.Message{Role: assistant.RoleAssistant, Content: text, ToolCalls: calls})
		for _, call := range calls {
			turns = append(turns, assistant.Message{Role: assistant.RoleTool, ToolCallID: call.ID, Content: repairToolRefusal(call.Name)})
		}
	}
}

// outputCut is the provider stopping an answer at the room it was given.
type outputCut struct{ limit int }

func (e *outputCut) Error() string {
	return fmt.Sprintf("provider output limit reached at %d tokens", e.limit)
}

// completeWithRoom is complete, asked again with more room each time the
// provider says the model ran out of it, when the room is Eli's to choose. A
// thinking model's reasoning counts against the room, so a repair that fits
// easily in one model can need several times as much in another, and whoever set
// the provider up should not have to learn that from a failed attempt. Each
// attempt is a step of its own, with the usage the provider counted for it.
//
// A provider that refuses the larger request has reached its model's own
// ceiling, or has no context left beside the prompt. It took the room asked for
// before, so that is where the answer was cut, and the repair ends there and
// says so.
func (m *repairModel) completeWithRoom(ctx context.Context, turns []assistant.Message) (string, []assistant.ToolCall, error) {
	cutAt := 0
	for {
		limit := m.room()
		text, calls, err := m.complete(ctx, turns, limit)
		var cut *outputCut
		switch {
		case errors.As(err, &cut):
			if m.maxTokens > 0 {
				return "", nil, fmt.Errorf("provider output limit reached at %d tokens: leave the provider's output limit empty so that Eli raises it as needed, choose a model with more output room, or request a smaller fix", limit)
			}
			if limit >= AssistantOutputTokensMax {
				return "", nil, fmt.Errorf("provider output limit reached at %d tokens, the most Eli asks for: %schoose a model with more output room, or request a smaller fix", limit, m.lowerEffort())
			}
			cutAt = limit
			m.raised = nextOutputLimit(limit)
		case cutAt > 0 && refusedRoom(err):
			return "", nil, fmt.Errorf("provider output limit reached at %d tokens, and the provider would not allow more room: set the provider's Output limit to that model's maximum, %schoose a model with more output room, or request a smaller fix", cutAt, m.lowerEffort())
		case cutAt == 0 && m.maxTokens == 0 && m.raised > repairOutputTokens && refusedRoom(err):
			// The room kept from an earlier step has to fit beside a prompt that is
			// longer now. This step has not run out of room, so it starts again from
			// where a repair starts, once: a refusal there is something else.
			m.raised = 0
		default:
			return text, calls, err
		}
	}
}

// lowerEffort is the advice to make a model think less, for a model that keeps
// running out of room. It is given only to a provider that can be told to: the
// others refuse the setting, and their form has nothing to change.
func (m *repairModel) lowerEffort() string {
	if assistant.SupportsReasoningEffort(m.kind) {
		return "lower the model's reasoning effort, "
	}
	return ""
}

// refusedRoom is whether the provider turned down the request itself, which is
// how a provider answers a ceiling above its model's, or above what its context
// holds beside the prompt. Any other failure is not about room.
func refusedRoom(err error) bool {
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) {
		return false
	}
	code := status.HTTPStatus()
	return code == http.StatusBadRequest || code == http.StatusUnprocessableEntity
}

// complete is one call to the provider, recorded as one step of the repair,
// so the history shows the turn the model spent on a tool call as well as the
// one that answered. limit is how much the model may write, thinking included.
func (m *repairModel) complete(ctx context.Context, turns []assistant.Message, limit int) (_ string, _ []assistant.ToolCall, resultErr error) {
	step := store.EliRepairStep{RequestBytes: len(prrepair.SystemPrompt)}
	var out strings.Builder
	var calls []assistant.ToolCall
	started := time.Now()
	defer func() {
		step.ElapsedMS = time.Since(started).Milliseconds()
		response := out.String()
		if len(calls) > 0 {
			// Keep arguments as text, including malformed JSON that could not
			// become a plan. This is evidence, never a tool to execute.
			for _, call := range calls {
				response += "\nTool call: " + call.Name + "\n" + string(call.Arguments)
			}
		}
		step.Response, step.ResponseTruncated = repairDiagnosticText(response)
		step.ResponseRecorded = true
		if resultErr != nil {
			step.Error, _ = repairDiagnosticText(resultErr.Error())
		}
		if m.record != nil {
			m.record(step)
		}
	}()
	for _, v := range turns {
		step.RequestBytes += len(v.Content)
	}
	stream, err := m.provider.Chat(ctx, assistant.Request{Model: m.model, System: prrepair.SystemPrompt, Messages: turns, MaxTokens: limit})
	if err != nil {
		return "", nil, repairProviderError(err)
	}
	defer stream.Close()
	done := false
	for {
		ev, ok := stream.Next(ctx)
		if !ok {
			break
		}
		if ev.Usage != nil && ev.Usage.Reported {
			step.InputTokens = ev.Usage.InputTokens
			step.OutputTokens = ev.Usage.OutputTokens
			step.UsageReported = true
		}
		if ev.ToolCall != nil {
			calls = append(calls, *ev.ToolCall)
		}
		out.WriteString(ev.Delta)
		if ev.Cut {
			return "", nil, &outputCut{limit: limit}
		}
		if ev.Err != nil {
			return "", nil, repairProviderError(ev.Err)
		}
		if out.Len() > prrepair.MaxReplyBytes {
			return "", nil, fmt.Errorf("the model response exceeded the repair limit")
		}
		if ev.Done {
			done = true
			break
		}
	}
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}
	if !done {
		return "", nil, fmt.Errorf("the model response ended before completion")
	}
	return out.String(), calls, nil
}

// Redact before shortening: cutting a private key or token first could leave
// an unrecognisable fragment in the stored diagnostic. Retain the model's text,
// not its input source or hidden reasoning, within the existing reply bound.
func repairDiagnosticText(raw string) (string, bool) {
	safe, _ := redact.Text(strings.ToValidUTF8(raw, ""))
	if len(safe) > prrepair.MaxReplyBytes {
		return strings.ToValidUTF8(safe[:prrepair.MaxReplyBytes], ""), true
	}
	return safe, false
}

func repairCalls(calls []assistant.ToolCall) []prrepair.Call {
	out := make([]prrepair.Call, len(calls))
	for i, c := range calls {
		out[i] = prrepair.Call{Name: c.Name, Arguments: c.Arguments}
	}
	return out
}

// repairToolRefusal is what a model is told for a tool it called during a
// repair. It names the replies the system prompt allows, because a model that
// reached for a tool was looking for a way to read or change a file and is
// quickest set right by being shown the way it has.
func repairToolRefusal(name string) string {
	return repairToolName(name) + ` is not a tool you were offered. No tools are offered during a repair, so do not call one. Reply with one JSON object as instructed: {"read":["path"]} or {"windows":[...]} to see source, {"summary":"...","edits":[...]} or {"summary":"...","files":[...]} to fix it, or {"cannot_fix":"reason"}.`
}

// repairToolName is a tool name as a PR comment can carry it: the model wrote
// it, so it is bounded and kept to one line, and a backtick in it cannot
// close the code span it is shown in.
func repairToolName(name string) string {
	name = strings.Join(strings.Fields(strings.ReplaceAll(name, "`", "'")), " ")
	if name == "" {
		name = "(unnamed)"
	}
	if len(name) > 64 {
		name = name[:64] + "..."
	}
	return "`" + name + "`"
}

// repairToolNames names the tools a model called, and the names of the arguments
// it gave each, never their values. A call the repair could not read as a reply
// is fixed by teaching it that shape, and the shape is what the comment needs to
// say to make that a one-line change instead of a guess.
func repairToolNames(calls []assistant.ToolCall) string {
	var names []string
	seen := map[string]bool{}
	for _, call := range calls {
		if seen[call.Name] {
			continue
		}
		seen[call.Name] = true
		name := repairToolName(call.Name)
		var args map[string]json.RawMessage
		if json.Unmarshal(call.Arguments, &args) == nil && len(args) > 0 {
			keys := make([]string, 0, len(args))
			for key := range args {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if len(keys) > 6 {
				keys = keys[:6]
			}
			for i, key := range keys {
				keys[i] = repairToolName(key)
			}
			name += " (with " + strings.Join(keys, ", ") + ")"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func (c *Controller) repairLoop(ctx context.Context) {
	if err := c.st.InterruptEliRepairs(ctx); err != nil {
		c.log.Error("could not recover Eli repairs", "error", err)
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	nextOffers := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		if c.mayAct() && !c.transferDraining.Load() {
			if row, err := c.st.NextEliRepair(ctx); err == nil {
				c.runEliRepair(ctx, row)
			}
			c.checkEliRepairs(ctx)
			if time.Now().After(nextOffers) {
				c.reconcileEliOpportunities(ctx)
				nextOffers = time.Now().Add(5 * time.Minute)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// triggerCommentID is the comment that asked for this repair, read back from the
// dedup key the webhook wrote. r.CommentID is Eli's own progress comment, so it
// cannot be used for this.
func triggerCommentID(r *store.EliRepair) int64 {
	rest, ok := strings.CutPrefix(r.DedupKey, "comment:"+r.Repo+":")
	if !ok {
		return 0
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// repairHeading gives each stage of a repair its own face, so a thread of Eli's
// updates reads as one dog doing something rather than a log. The state is the
// repair's, not the text's: the wording of a message can change without the
// heading drifting.
func repairHeading(state string) string {
	switch state {
	case "working":
		return "### 🐕 Eli is sniffing around"
	case "checking":
		return "### 🦴 Eli fetched a fix"
	case "succeeded":
		return "### 🎾 Good dog: the checks passed"
	case "checks_failed":
		return "### 🐾 Eli's fix did not pass the checks"
	case "failed", "blocked":
		return "### 🐶 Eli got stuck"
	case "superseded", "unverified":
		return "### 🐕‍🦺 Eli lost the scent"
	}
	return "### 🐶 Eli"
}

func (c *Controller) repairNotice(ctx context.Context, client github.RepairClient, r *store.EliRepair, text string) {
	if r.PullNumber <= 0 {
		return
	}
	text = repairHeading(r.State) + "\n\n" + text + "\n\n<sub>Repair `" + r.ID + "`. 🐾 Zoomies. Eli fetches fixes but never merges: that part stays with you. Ask again with `/eli fix`.</sub>\n<!-- zoomies-eli-repair:" + r.ID + " -->"
	id, err := client.RepairComment(ctx, r.Repo, r.PullNumber, r.CommentID, text)
	if err != nil {
		c.log.Warn("could not update Eli's PR comment", "repair", r.ID)
		return
	}
	r.CommentID = id
	_ = c.st.SaveEliRepair(ctx, r)
}

// Model deadlines stop work, but must not prevent recording its terminal outcome.
func (c *Controller) persistEliRepair(ctx context.Context, r *store.EliRepair) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return c.st.SaveEliRepair(persistCtx, r)
}

func (c *Controller) runEliRepair(parent context.Context, r *store.EliRepair) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	policy, err := c.repairPolicy(ctx, r.Repo, r.InstallationID)
	if err != nil || r.Trigger == "automatic" && !policy.Automatic {
		r.State = "blocked"
		r.Message = "Repository repairs are disabled or the installation changed."
		if err == nil {
			// Ask me first leaves repairs on, so "disabled" would contradict the page.
			r.Message = "Automatic repairs were stopped for this repository before this one ran."
		}
		_ = c.persistEliRepair(ctx, r)
		return
	}
	raw, err := c.ClientFor(ctx, r.InstallationID)
	if err != nil {
		r.State = "blocked"
		r.Message = "The GitHub installation is unavailable."
		_ = c.persistEliRepair(ctx, r)
		return
	}
	installation, installationErr := c.st.GetInstallation(ctx, r.InstallationID)
	client, ok := raw.(github.RepairClient)
	if !ok || installationErr != nil {
		r.State = "blocked"
		r.Message = "This GitHub client does not support PR repair."
		_ = c.persistEliRepair(ctx, r)
		return
	}
	r.State = "working"
	r.Message = "Reading the PR and failed-job evidence."
	if err := c.persistEliRepair(ctx, r); err != nil {
		return
	}
	fail := func(message string) {
		r.State = "failed"
		r.Message = prrepair.Redact(message)
		r.Details.FailureCode = repairFailureCode(message)
		_ = c.persistEliRepair(ctx, r)
		c.repairNotice(ctx, client, r, r.Message)
	}
	// failWith ends the repair with an error the controller did not write, and
	// keeps the failure code that error carries: a provider that cannot pay is
	// not the same thing as a provider that could not be reached, and the code
	// is what the advice on the page is chosen by.
	failWith := func(err error) {
		r.State = "failed"
		r.Message = prrepair.Redact(err.Error())
		r.Details.FailureCode = repairFailureCode(r.Message)
		var coded interface{ RepairFailureCode() string }
		if errors.As(err, &coded) && coded.RepairFailureCode() != "" {
			r.Details.FailureCode = coded.RepairFailureCode()
		}
		_ = c.persistEliRepair(ctx, r)
		c.repairNotice(ctx, client, r, r.Message)
	}
	var pull github.RepairPull
	if r.Trigger == "automatic" {
		pull, err = client.RepairPullForRun(ctx, r.Repo, r.RunID)
	} else {
		pull, err = client.RepairPull(ctx, r.Repo, r.PullNumber)
	}
	if err != nil {
		fail("The PR is closed, from a fork, stale, or could not be read. No branch was changed.")
		return
	}
	r.PullNumber = pull.Number
	if r.HeadSHA != "" && r.HeadSHA != pull.HeadSHA {
		fail("The PR changed after this failure. No branch was changed.")
		return
	}
	r.HeadSHA = pull.HeadSHA
	// A repair asked for in a comment reaches the worker with no case: the
	// webhook knew the pull request and nothing more. Reading it here gives the
	// attempt page the same facts as one started from the UI. A case a request
	// already recorded is the failure the person saw and is left as it was.
	if r.Details.Case == nil {
		r.Details.Case = c.repairCase(ctx, client, r.Repo, pull, r.RunID)
	}
	stillNeeded := func() bool {
		state, err := client.RepairChecks(ctx, r.Repo, pull.HeadSHA)
		if err != nil {
			fail("GitHub could not confirm whether this failure still needs fixing. No branch was changed.")
			return false
		}
		if state == "passed" {
			r.State = "superseded"
			r.Message = "The latest checks have already passed. No repair commit is needed and no branch was changed."
			_ = c.persistEliRepair(ctx, r)
			c.repairNotice(ctx, client, r, r.Message)
			return false
		}
		return true
	}
	if !stillNeeded() {
		return
	}
	var provider *store.AssistantProvider
	if r.Trigger == "mention" {
		actor, err := client.RepairActor(ctx, r.Repo, r.GitHubLogin)
		if err != nil || actor.ID != r.GitHubUserID || !actor.CanWrite {
			r.State = "blocked"
			r.Message = "The requester needs current repository write permission."
			_ = c.persistEliRepair(ctx, r)
			return
		}
		user, err := c.st.GetUser(ctx, r.UserID)
		if err != nil || user.Disabled {
			fail("The linked Zoomies account is disabled or unavailable.")
			return
		}
		identities, err := c.st.EliIdentities(ctx)
		linked := false
		if err == nil {
			for _, id := range identities {
				if id.UserID == r.UserID && id.GitHubUserID == r.GitHubUserID && id.Confirmed {
					linked = true
				}
			}
		}
		if !linked {
			fail("The GitHub account link was removed or changed.")
			return
		}
		provider, err = c.personalChatProvider(ctx, "", r.UserID)
		if err != nil {
			fail("The requester has no enabled personal provider. Add one in Zoomies, Settings, Assistant, then ask again.")
			return
		}
	} else {
		provider, err = c.st.GetAssistantProvider(ctx, policy.ProviderID)
		if err != nil || !provider.Enabled || provider.OwnerID != "" {
			fail("The repository's installation provider is unavailable. No personal provider was used.")
			return
		}
	}
	if (strings.HasPrefix(r.DedupKey, "ui:") || strings.HasPrefix(r.DedupKey, "offer:")) && assistant.Subscription(assistant.Kind(provider.Kind)) && !r.RequesterAdmin {
		fail("The requesting credential needs administrator permission to run subscription tools.")
		return
	}
	r.ProviderID = provider.ID
	r.Details.Model = provider.Model
	r.Details.Stage = "evidence"
	_ = c.persistEliRepair(ctx, r)
	// An eyes reaction is the quickest thing to show on the comment that asked,
	// and it is best effort: a repair never fails because GitHub refused one.
	if id := triggerCommentID(r); id > 0 {
		if err := client.RepairReact(ctx, r.Repo, id, "eyes"); err != nil {
			c.log.Warn("could not react to the repair request", "repair", r.ID)
		}
	}
	c.repairNotice(ctx, client, r, "Investigating this PR using "+provider.Name+". The repair is limited to one commit and will leave normal review in place.")
	failures, err := client.RepairFailures(ctx, r.Repo, pull, r.JobID)
	if err != nil {
		fail("Failed-job evidence could not be read. Check the App's Actions read permission.")
		return
	}
	if r.Trigger == "automatic" && len(failures) == 0 {
		fail("The failed job has no usable error evidence. No code was changed.")
		return
	}
	r.Details.Stage = "source"
	_ = c.persistEliRepair(ctx, r)
	source, err := client.RepairSource(ctx, r.Repo, pull, policy.AllowWorkflows)
	if err != nil {
		// The reason is the useful part: "within Eli's repair limits" named
		// neither the limit nor the file, and the person could not act on it.
		fail(fmt.Sprintf("The PR source could not be read: %s.", strings.TrimSuffix(err.Error(), ".")))
		return
	}
	source.Snapshot.Failures = failures
	p, err := c.OpenAssistantProvider(provider, "")
	if err != nil {
		fail("The configured model provider could not be opened.")
		return
	}
	r.Details.Stage = "prepare_fix"
	plan, err := prrepair.Generate(ctx, &repairModel{provider: p, model: provider.Model, kind: assistant.Kind(provider.Kind), maxTokens: provider.MaxOutputTokens, sourceUsed: func(n int) { r.Details.SourceBytes = n }, record: func(step store.EliRepairStep) {
		r.Details.Steps = append(r.Details.Steps, step)
		_ = c.persistEliRepair(ctx, r)
	}, rejected: func(err error) {
		if len(r.Details.Steps) > 0 {
			last := &r.Details.Steps[len(r.Details.Steps)-1]
			last.ValidationError, _ = repairDiagnosticText(err.Error())
			_ = c.persistEliRepair(ctx, r)
		}
	}}, source, source.Snapshot, r.Instruction, policy.AllowWorkflows)
	if err != nil {
		failWith(err)
		return
	}
	// Re-read both authorisation and credentials after the model's work, before the first repository write.
	latestPolicy, err := c.repairPolicy(ctx, r.Repo, r.InstallationID)
	if err != nil || !latestPolicy.AllowWorkflows && policy.AllowWorkflows {
		fail("Repository repair policy changed while Eli was working. No branch was changed.")
		return
	}
	latestInstallation, err := c.st.GetInstallation(ctx, r.InstallationID)
	if err != nil || !latestInstallation.UpdatedAt.Equal(installation.UpdatedAt) {
		fail("The GitHub installation changed while Eli was working. No branch was changed.")
		return
	}
	current, err := c.st.GetAssistantProvider(ctx, r.ProviderID)
	if err != nil || !current.Enabled || current.OwnerID != provider.OwnerID || !current.UpdatedAt.Equal(provider.UpdatedAt) {
		fail("The provider was removed or disabled while Eli was working.")
		return
	}
	if r.Trigger == "automatic" && (!latestPolicy.Automatic || latestPolicy.ProviderID != r.ProviderID) {
		fail("Automatic repair was disabled or its provider changed.")
		return
	}
	if r.Trigger == "mention" {
		actor, e := client.RepairActor(ctx, r.Repo, r.GitHubLogin)
		user, u := c.st.GetUser(ctx, r.UserID)
		identities, linkErr := c.st.EliIdentities(ctx)
		linked := false
		for _, id := range identities {
			if id.UserID == r.UserID && id.GitHubUserID == r.GitHubUserID && id.Confirmed {
				linked = true
			}
		}
		if e != nil || actor.ID != r.GitHubUserID || !actor.CanWrite || u != nil || user.Disabled || linkErr != nil || !linked {
			fail("The requester's access changed while Eli was working.")
			return
		}
	}
	if !c.mayAct() || c.transferDraining.Load() || ctx.Err() != nil {
		fail("The repair stopped before publication.")
		return
	}
	if !stillNeeded() {
		return
	}
	r.Details.Stage = "publish"
	_ = c.persistEliRepair(ctx, r)
	sha, err := client.CommitRepair(ctx, r.Repo, pull, plan)
	if err != nil {
		fail("The repair could not advance the PR branch. It may have changed, or the App may lack Contents write permission. No force push was attempted.")
		return
	}
	r.CommitSHA = sha
	published := c.Now()
	r.PublishedAt = &published
	r.State = "checking"
	r.Details.Stage = "watch_ci"
	r.Message = plan.Summary + " CI has not been verified yet."
	if err := c.persistEliRepair(ctx, r); err != nil {
		c.log.Error("could not record the published Eli repair", "repair", r.ID)
		return
	}
	c.repairNotice(ctx, client, r, "Pushed commit `"+sha+"`. "+plan.Summary+"\n\nWaiting for checks on this commit. No local tests were run by Eli.")
}
func (c *Controller) checkEliRepairs(ctx context.Context) {
	rows, err := c.st.CheckingEliRepairs(ctx)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.State != "checking" {
			continue
		}
		checkStarted := r.UpdatedAt
		if r.PublishedAt != nil {
			checkStarted = *r.PublishedAt
		} else {
			r.PublishedAt = &checkStarted
		}
		if c.Now().Sub(r.UpdatedAt) < 30*time.Second {
			continue
		}
		raw, e := c.ClientFor(ctx, r.InstallationID)
		if e != nil {
			if c.Now().Sub(checkStarted) >= time.Hour {
				r.State = "unverified"
				r.Message = "The GitHub installation is unavailable. Check the PR's CI results before accepting the repair."
				_ = c.st.SaveEliRepair(ctx, r)
			}
			continue
		}
		client, ok := raw.(github.RepairClient)
		if !ok {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		p, e := client.RepairPull(checkCtx, r.Repo, r.PullNumber)
		state := "waiting"
		if e == nil && p.HeadSHA != r.CommitSHA {
			state = "superseded"
		} else if e == nil {
			state, e = client.RepairChecks(checkCtx, r.Repo, r.CommitSHA)
		}
		cancel()
		if e != nil && c.Now().Sub(checkStarted) < time.Hour {
			continue
		}
		switch state {
		case "passed":
			r.State = "succeeded"
			r.Message = "The reported checks on Eli's commit passed. Normal PR review still applies."
		case "failed":
			r.State = "checks_failed"
			r.Message = "Checks on Eli's commit failed. Automatic repair will not repeat its own commit; a maintainer can request another attempt."
		case "superseded":
			r.State = "superseded"
			r.Message = "The PR has a newer commit. This repair's checks no longer describe its current head."
		default:
			if c.Now().Sub(checkStarted) < time.Hour {
				r.UpdatedAt = c.Now()
				_ = c.st.SaveEliRepair(ctx, r)
				continue
			}
			r.State = "unverified"
			r.Message = "No complete check result was observed within an hour. If the pull request's checks have passed, the App may lack Checks read or Commit statuses read; grant both and ask for another repair. Otherwise inspect the pull request's checks before accepting the fix."
		}
		_ = c.st.SaveEliRepair(ctx, r)
		c.repairNotice(ctx, client, r, r.Message)
	}
}

// RequestEliRepair is the same owner-scoped operation as a GitHub mention, reachable from the UI.
func (c *Controller) RequestEliRepair(ctx context.Context, userID, repo string, pull int, instruction string, requesterAdmin bool) (*store.EliRepair, error) {
	return c.RequestPinnedEliRepair(ctx, userID, repo, pull, instruction, "", 0, requesterAdmin)
}

// RequestPinnedEliRepair is the request the repair page makes. runID is the run
// whose failure the person was looking at, or 0 when they came from the pull
// request alone; it is what the attempt later says the repair was started for.
func (c *Controller) RequestPinnedEliRepair(ctx context.Context, userID, repo string, pull int, instruction, expectedHead string, runID int64, requesterAdmin bool) (*store.EliRepair, error) {
	return c.requestPinnedEliRepair(ctx, userID, repo, pull, instruction, expectedHead, "", runID, requesterAdmin)
}
func (c *Controller) requestPinnedEliRepair(ctx context.Context, userID, repo string, pull int, instruction, expectedHead, dedup string, runID int64, requesterAdmin bool) (*store.EliRepair, error) {
	repo = normaliseRepairRepo(repo)
	if pull < 1 || len(instruction) > 4000 {
		return nil, store.ErrConflict
	}
	inst, err := c.st.FindInstallationByTarget(ctx, repo)
	if err != nil {
		return nil, err
	}
	policy, err := c.repairPolicy(ctx, repo, inst.ID)
	if err != nil {
		return nil, err
	}
	identities, err := c.st.EliIdentities(ctx)
	if err != nil {
		return nil, err
	}
	var identity *store.EliIdentity
	for _, id := range identities {
		if id.UserID == userID && id.Confirmed {
			identity = &id
			break
		}
	}
	if identity == nil {
		return nil, store.ErrNotFound
	}
	raw, err := c.ClientFor(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		return nil, store.ErrConflict
	}
	actor, err := client.RepairActor(ctx, repo, identity.GitHubLogin)
	if err != nil || !actor.CanWrite || actor.ID != identity.GitHubUserID {
		return nil, store.ErrConflict
	}
	pr, err := client.RepairPull(ctx, repo, pull)
	if err != nil {
		var changed *github.RepairTargetChanged
		if errors.As(err, &changed) {
			return nil, changed
		}
		return nil, store.ErrConflict
	}
	if expectedHead != "" && pr.HeadSHA != expectedHead {
		return nil, &github.RepairTargetChanged{Code: "superseded", Message: "The PR has a newer commit since you reviewed this failure. No repair was started and no attempt was used. Check the current PR before requesting a repair."}
	}
	state, err := client.RepairChecks(ctx, repo, pr.HeadSHA)
	if err != nil {
		return nil, fmt.Errorf("could not recheck the PR's current checks: %w", err)
	}
	if state == "passed" {
		return nil, &github.RepairTargetChanged{Code: "fixed", Message: "The latest checks have passed since you reviewed this failure. No repair was started and no attempt was used."}
	}
	if state != "failed" {
		return nil, &github.RepairTargetChanged{Code: "checks_pending", Message: "There are no current failed checks to repair. No repair was started and no attempt was used. Wait for the checks, then review again."}
	}
	provider, err := c.personalChatProvider(ctx, "", userID)
	if err != nil {
		return nil, err
	}
	if assistant.Subscription(assistant.Kind(provider.Kind)) && !requesterAdmin {
		return nil, ErrAssistantSubscriptionRestricted
	}
	if instruction == "" {
		instruction = "Fix the underlying issue in this PR using the failed-job evidence."
	}
	row := &store.EliRepair{Repo: repo, InstallationID: inst.ID, PullNumber: pull, HeadSHA: pr.HeadSHA, RunID: runID, UserID: userID, GitHubUserID: identity.GitHubUserID, GitHubLogin: identity.GitHubLogin, ProviderID: provider.ID, Trigger: "mention", RequesterAdmin: requesterAdmin, Instruction: instruction, DedupKey: fmt.Sprintf("ui:%s:%d:%s:%s", repo, pull, pr.HeadSHA, userID)}
	row.Details.Case = c.repairCase(ctx, client, repo, pr, runID)
	if dedup != "" {
		row.DedupKey = dedup
	}
	_, err = c.st.EnqueueEliRepair(ctx, row, policy.DailyLimit)
	return row, err
}

func repairFailureCode(message string) string {
	switch {
	// First, because the message carries a tool name the model chose, and a
	// name such as read_file would otherwise read as a source failure.
	case strings.Contains(message, "tool call"):
		return "provider_unavailable"
	case strings.Contains(message, "model did not return a valid repair plan"):
		return "unsupported_fix"
	case strings.Contains(message, "cannot pay") || strings.Contains(message, "out of credit"):
		return "provider_billing"
	case strings.Contains(message, "rate limit"):
		return "rate_limited"
	case strings.Contains(message, "provider credentials"):
		return "provider_credentials"
	case strings.Contains(message, "output limit"):
		return "output_limit"
	case strings.Contains(message, "budget") || strings.Contains(message, "allowance") || strings.Contains(message, "input limit") || strings.Contains(message, "context limit"):
		return "context_limit"
	case strings.Contains(message, "read") || strings.Contains(message, "source"):
		return "source_unavailable"
	case strings.Contains(message, "provider"):
		return "provider_unavailable"
	case strings.Contains(message, "permission") || strings.Contains(message, "account"):
		return "setup_needed"
	default:
		return "unsupported_fix"
	}
}

// repairProviderFailure is a provider's error as a repair words it, with the
// status the provider answered kept. The words are for whoever reads the PR
// comment; the status is for the repair, which has to tell a provider refusing
// the room it was asked for from the other ways a provider fails.
type repairProviderFailure struct {
	message string
	status  int
	// code is the failure the repair records. It is carried rather than read
	// back out of the words, because the words are the controller's own and a
	// provider's answer quoted in them would say what it means to say.
	code string
}

func (e *repairProviderFailure) Error() string   { return e.message }
func (e *repairProviderFailure) HTTPStatus() int { return e.status }

// RepairFailureCode is the code the repair records for this failure.
func (e *repairProviderFailure) RepairFailureCode() string { return e.code }

func repairProviderError(err error) error {
	var carried interface{ HTTPStatus() int }
	status := 0
	if errors.As(err, &carried) {
		status = carried.HTTPStatus()
	}
	fail := func(message, code string) error {
		return &repairProviderFailure{message: message, status: status, code: code}
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "context length") || strings.Contains(message, "context window"):
		return fail("the provider context limit was reached; narrow the investigation or choose a model with more input room", "context_limit")
	case status == http.StatusPaymentRequired || providerOutOfCredit(message):
		// A provider with no credit left is not a provider that could not be
		// reached, and "test its connection" names nothing the person can act
		// on. The status is kept so the provider's own page can be matched.
		paid := "the provider's account cannot pay for this repair"
		if status > 0 {
			paid += " (HTTP " + strconv.Itoa(status) + ")"
		}
		return fail(paid+"; add credit to the provider's account or choose another provider in Eli settings", "provider_billing")
	}
	switch status {
	case 429:
		return fail("the provider rate limited this repair; check quota or try again later", "rate_limited")
	case 401, 403:
		return fail("the provider credentials were refused; update or test your provider in Eli settings", "provider_credentials")
	case 404:
		return fail("the provider model or endpoint was not found; check your provider settings", "provider_unavailable")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail("the provider timed out; try again later", "provider_unavailable")
	}
	return fail("the provider could not complete this repair; test its connection in Eli settings", "provider_unavailable")
}

// providerOutOfCredit is whether a provider said it cannot pay: an account
// with no credit left, a balance too low, or a quota it has already spent.
// The words differ between providers, so the ones that mean it are listed.
// A provider that is merely slow says rate limit, and is left to that.
func providerOutOfCredit(message string) bool {
	for _, phrase := range []string{
		"insufficient funds",
		"insufficient balance",
		"insufficient credit",
		"insufficient account funds",
		"insufficient quota",
		"insufficient_quota",
		"out of credit",
		"out of funds",
		"not enough funds",
		"no funds",
		"credits exhausted",
		"credit balance is too low",
		"balance is too low",
		"payment required",
		"payment failed",
		"check your plan and billing",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
