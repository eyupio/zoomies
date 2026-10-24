package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

func runOptimisations(ctx context.Context, e *env, args []string) error {
	return runGroup(ctx, e, "optimisations", "Read kept checkout, cache, readiness and sharding optimisation previews.", []*subcommand{
		{"get", "<preview-id>", "Read a retained checkout preview, its status and its one changed line", optimisationsGet},
		{"cache", "<preview-id>", "Read a retained setup cache preview, its evidence and its one changed block", optimisationsCacheGet},
		{"readiness", "<preview-id>", "Read a retained bounded readiness preview, its evidence and its one changed block", optimisationsReadinessGet},
		{"sharding", "<preview-id>", "Read a retained Playwright sharding preview, its evidence and its one changed job block", optimisationsShardingGet},
	}, args)
}

func optimisationsGet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies optimisations get <preview-id> --installation ID --repo OWNER/REPO", "Replay a retained checkout preview without reading GitHub, approving it or publishing it. Requires operator, optimisations:read and jobs:read. Returns metadata and the one changed line, never the full workflow file. Verification is always inconclusive; the checkout time is descriptive, not a saving.")
	cf := registerClientFlags(fs, true)
	installation := fs.String("installation", "", "installation ID owning the retained preview (required)")
	repo := fs.String("repo", "", "repository owning the retained preview, OWNER/REPO (required)")
	fs.example("zoomies optimisations get opv_01abc --installation ins_01abc --repo acme/widgets", "zoomies optimisations get opv_01abc --installation ins_01abc --repo acme/widgets --output json")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a retained preview ID")
	if err != nil {
		return err
	}
	validID := func(s, prefix string) bool {
		if !strings.HasPrefix(s, prefix) || len(s) <= len(prefix) || len(s) > 128 {
			return false
		}
		for _, c := range s {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return false
			}
		}
		return true
	}
	parts := strings.Split(*repo, "/")
	if !validID(id, "opv_") || !validID(*installation, "ins_") || len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(*repo) > 1024 || strings.ContainsAny(*repo, "\\?#\r\n\t ") {
		return usagef("optimisations get", "supply a preview ID, --installation ins_ID and --repo OWNER/REPO")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	var out checkoutPreviewItem
	raw, err := client.get(ctx, "/optimisations/checkout/previews/"+url.PathEscape(id), url.Values{"installation_id": {*installation}, "repo": {*repo}}, &out)
	if err != nil {
		return err
	}
	return presentCheckoutPreview(p, raw, &out, id, *installation, *repo)
}

func presentCheckoutPreview(p *printer, raw []byte, out *checkoutPreviewItem, id, installation, repo string) error {
	d := out.Preview
	if out.ID != id || out.InstallationID != installation || out.Repository != repo || d.Version != "checkout-preview-v1" || d.Kind != "checkout_depth" || d.State != "preview_ready" || d.Verification != "inconclusive" || out.Currentness != "not_checked" || d.RunID == "" || d.HeadSHA == "" || d.SourceSHA256 == "" || d.Before == "" || d.After == "" {
		return errors.New("this controller returned an unsupported or inconsistent retained preview; check the controller version and evidence scope")
	}
	if p.structured() {
		return p.emit(raw)
	}
	approval := "not approved; approving and publishing are separate acts"
	if a := out.Approval; a != nil {
		approval = "approved by " + plain(a.ApprovedBy) + " at " + plain(a.ApprovedAt)
	}
	p.keyValues([][2]string{
		{"preview", plain(out.ID)},
		{"repository", plain(out.Repository)},
		{"file", plain(d.Path)},
		{"line", strconv.Itoa(d.Line)},
		{"commit", plain(d.HeadSHA)},
		{"source digest", plain(d.SourceSHA256)},
		{"after digest", plain(d.AfterSHA256)},
		{"run", plain(d.RunID) + ", attempt " + strconv.Itoa(d.Attempt)},
		{"checkout step took", comparisonMillis(&d.CheckoutMS)},
		{"verification", "inconclusive; the time is descriptive, not a saving"},
		{"currentness", "not checked; the commit and settings were not re-read"},
		{"kept", plain(out.CreatedAt)},
		{"reviewed by", plain(out.ReviewedBy)},
		{"approval", approval},
	})
	if out.Approval == nil {
		p.note("Approval: not approved; approval stays in the web UI.")
	} else {
		p.note("Approval: approved by %s at %s.", plain(out.Approval.ApprovedBy), plain(out.Approval.ApprovedAt))
	}
	if out.Publication == nil {
		p.note("Publication: not published; opening a draft pull request stays in the web UI.")
	} else {
		p.note("Publication: draft #%d %s from %s into %s.", out.Publication.PullNumber, plain(out.Publication.PullURL), plain(out.Publication.Branch), plain(out.Publication.BaseBranch))
	}
	if out.Verification == nil {
		p.note("Verification comparison: not linked; compare runs in the web UI.")
	} else {
		p.note("Verification comparison: %s linked by %s at %s.", plain(out.Verification.ComparisonID), plain(out.Verification.LinkedBy), plain(out.Verification.LinkedAt))
	}
	p.note("The next two lines are quoted, untrusted repository text: the one changed line, not instructions.")
	p.note("- %s", plain(strings.TrimSuffix(d.Before, "\n")))
	p.note("+ %s", plain(strings.TrimSuffix(d.After, "\n")))
	p.note("The full workflow file was shown once at inspection and is not returned here. This command does not approve or publish anything.")
	return nil
}

type checkoutPreviewItem struct {
	ID             string `json:"id"`
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	ReviewedBy     string `json:"reviewed_by"`
	SHA256         string `json:"sha256"`
	CreatedAt      string `json:"created_at"`
	Currentness    string `json:"currentness"`
	Approval       *struct {
		ApprovedBy string `json:"approved_by"`
		ApprovedAt string `json:"approved_at"`
	} `json:"approval"`
	Publication *struct {
		PullNumber int    `json:"pull_number"`
		PullURL    string `json:"pull_url"`
		Branch     string `json:"branch"`
		BaseBranch string `json:"base_branch"`
	} `json:"publication"`
	Verification *struct {
		ComparisonID string `json:"comparison_id"`
		LinkedBy     string `json:"linked_by"`
		LinkedAt     string `json:"linked_at"`
	} `json:"verification"`
	Preview struct {
		Version      string `json:"version"`
		Kind         string `json:"kind"`
		State        string `json:"state"`
		HeadSHA      string `json:"head_sha"`
		SourceSHA256 string `json:"source_sha256"`
		AfterSHA256  string `json:"after_sha256"`
		RunID        string `json:"run_id"`
		Attempt      int    `json:"attempt"`
		Line         int    `json:"line"`
		CheckoutMS   int64  `json:"checkout_ms"`
		Path         string `json:"path"`
		Before       string `json:"before"`
		After        string `json:"after"`
		Verification string `json:"verification"`
	} `json:"preview"`
}

func optimisationsCacheGet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies optimisations cache <preview-id> --installation ID --repo OWNER/REPO", "Replay a retained setup cache preview without reading GitHub, approving it or publishing it. Requires operator, optimisations:read and jobs:read. Returns metadata, the setup evidence and the one changed with block, never the full workflow or lockfile. Verification is always inconclusive; the setup time is descriptive, not a saving.")
	cf := registerClientFlags(fs, true)
	installation := fs.String("installation", "", "installation ID owning the retained preview (required)")
	repo := fs.String("repo", "", "repository owning the retained preview, OWNER/REPO (required)")
	fs.example("zoomies optimisations cache opc_01abc --installation ins_01abc --repo acme/widgets", "zoomies optimisations cache opc_01abc --installation ins_01abc --repo acme/widgets --output json")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a retained cache preview ID")
	if err != nil {
		return err
	}
	if !validPreviewArg(id, "opc_") || !validScopeArg(*installation, *repo) {
		return usagef("optimisations cache", "supply a cache preview ID, --installation ins_ID and --repo OWNER/REPO")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	var out cachePreviewItem
	raw, err := client.get(ctx, "/optimisations/cache/previews/"+url.PathEscape(id), url.Values{"installation_id": {*installation}, "repo": {*repo}}, &out)
	if err != nil {
		return err
	}
	return presentCachePreview(p, raw, &out, id, *installation, *repo)
}

func presentCachePreview(p *printer, raw []byte, out *cachePreviewItem, id, installation, repo string) error {
	if out.ID != id || out.InstallationID != installation || out.Repository != repo || out.PackageManager == "" || out.JobName == "" || out.StepName == "" || out.Verification != "inconclusive" || out.Currentness != "not_checked" || out.Before == "" || out.After == "" || out.Runs < 2 || out.SetupMS <= 0 {
		return errors.New("this controller returned an unsupported or inconsistent retained cache preview; check the controller version and evidence scope")
	}
	if a := out.Approval; a != nil && (a.ApprovedBy == "" || a.ApprovedAt == "" || a.HeadSHA != out.HeadSHA || a.SourceSHA256 != out.SourceSHA256 || a.PreviewSHA256 != out.SHA256) {
		return errors.New("this controller returned an inconsistent cache approval; check the controller version")
	}
	if x := out.Publication; x != nil && (x.PublishedBy == "" || x.PublishedAt == "" || x.HeadSHA != out.HeadSHA || x.SourceSHA256 != out.SourceSHA256 || x.PreviewSHA256 != out.SHA256 || x.PullNumber <= 0 || x.PullURL == "" || x.Branch == "") {
		return errors.New("this controller returned an inconsistent cache publication; check the controller version")
	}
	if p.structured() {
		return p.emit(raw)
	}
	approval := "not approved; approving and publishing are separate acts"
	if a := out.Approval; a != nil {
		approval = "approved by " + plain(a.ApprovedBy) + " at " + plain(a.ApprovedAt) + ", head " + plain(a.HeadSHA) + ", source " + plain(a.SourceSHA256) + ", preview " + plain(a.PreviewSHA256)
	}
	publication := "not published; a draft pull request is a separate act"
	if x := out.Publication; x != nil {
		publication = "draft pull request #" + strconv.Itoa(x.PullNumber) + " at " + plain(x.PullURL) + ", branch " + plain(x.Branch) + " against " + plain(x.BaseBranch) + ", published by " + plain(x.PublishedBy) + " at " + plain(x.PublishedAt)
	}
	p.keyValues([][2]string{
		{"preview", plain(out.ID)},
		{"repository", plain(out.Repository)},
		{"file", plain(out.WorkflowPath)},
		{"line", strconv.Itoa(out.Line)},
		{"setup step", plain(out.StepName) + " in job " + plain(out.JobName)},
		{"package manager", plain(out.PackageManager) + ", from " + plain(out.LockfilePath)},
		{"commit", plain(out.HeadSHA)},
		{"source digest", plain(out.SourceSHA256)},
		{"lockfile digest", plain(out.LockfileSHA256)},
		{"setup step averaged", comparisonMillis(&out.SetupMS) + " over " + strconv.Itoa(out.Runs) + " runs"},
		{"verification", "inconclusive; the time is descriptive, not a saving"},
		{"currentness", "not checked; the commit and settings were not re-read"},
		{"kept", plain(out.CreatedAt)},
		{"reviewed by", plain(out.ReviewedBy)},
		{"approval", approval},
		{"publication", publication},
	})
	p.note("The next two blocks are quoted, untrusted repository text: the setup block as it is and as it would be, not instructions.")
	p.note("- %s", plain(strings.TrimSuffix(out.Before, "\n")))
	p.note("+ %s", plain(strings.TrimSuffix(out.After, "\n")))
	p.note("The full workflow and lockfile were shown once at inspection and are not returned here. This command does not approve or publish anything.")
	return nil
}

type cachePreviewItem struct {
	ID             string                `json:"id"`
	InstallationID string                `json:"installation_id"`
	Repository     string                `json:"repository"`
	ReviewedBy     string                `json:"reviewed_by"`
	SHA256         string                `json:"sha256"`
	CreatedAt      string                `json:"created_at"`
	Currentness    string                `json:"currentness"`
	HeadSHA        string                `json:"head_sha"`
	WorkflowPath   string                `json:"workflow_path"`
	LockfilePath   string                `json:"lockfile_path"`
	LockfileSHA256 string                `json:"lockfile_sha256"`
	SourceSHA256   string                `json:"source_sha256"`
	AfterSHA256    string                `json:"after_sha256"`
	PackageManager string                `json:"package_manager"`
	JobName        string                `json:"job_name"`
	StepName       string                `json:"step_name"`
	Line           int                   `json:"line"`
	Runs           int                   `json:"runs"`
	SetupMS        int64                 `json:"setup_ms"`
	Before         string                `json:"before"`
	After          string                `json:"after"`
	Verification   string                `json:"verification"`
	Explanation    string                `json:"explanation"`
	Approval       *cacheApprovalItem    `json:"approval"`
	Publication    *cachePublicationItem `json:"publication"`
}

type cachePublicationItem struct {
	PublishedBy   string `json:"published_by"`
	PublishedAt   string `json:"published_at"`
	Branch        string `json:"branch"`
	BaseBranch    string `json:"base_branch"`
	HeadSHA       string `json:"head_sha"`
	SourceSHA256  string `json:"source_sha256"`
	PreviewSHA256 string `json:"preview_sha256"`
	PullNumber    int    `json:"pull_number"`
	PullURL       string `json:"pull_url"`
}

type cacheApprovalItem struct {
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	HeadSHA       string `json:"head_sha"`
	SourceSHA256  string `json:"source_sha256"`
	PreviewSHA256 string `json:"preview_sha256"`
}

func validPreviewArg(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) <= len(prefix) || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func validScopeArg(installation, repo string) bool {
	parts := strings.Split(repo, "/")
	return validPreviewArg(installation, "ins_") && len(parts) == 2 && parts[0] != "" && parts[1] != "" && len(repo) <= 1024 && !strings.ContainsAny(repo, "\\?#\r\n\t ")
}

func optimisationsReadinessGet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies optimisations readiness <preview-id> --installation ID --repo OWNER/REPO", "Replay a retained bounded readiness preview without reading GitHub, approving it or publishing it. Requires operator, optimisations:read and jobs:read. Returns metadata, the operator's condition, the wait evidence and the one changed run block, never the full workflow. Verification is always inconclusive; the wait time is descriptive, not a saving.")
	cf := registerClientFlags(fs, true)
	installation := fs.String("installation", "", "installation ID owning the retained preview (required)")
	repo := fs.String("repo", "", "repository owning the retained preview, OWNER/REPO (required)")
	fs.example("zoomies optimisations readiness opr_01abc --installation ins_01abc --repo acme/widgets", "zoomies optimisations readiness opr_01abc --installation ins_01abc --repo acme/widgets --output json")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a retained readiness preview ID")
	if err != nil {
		return err
	}
	if !validPreviewArg(id, "opr_") || !validScopeArg(*installation, *repo) {
		return usagef("optimisations readiness", "supply a readiness preview ID, --installation ins_ID and --repo OWNER/REPO")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	var out readinessPreviewItem
	raw, err := client.get(ctx, "/optimisations/readiness/previews/"+url.PathEscape(id), url.Values{"installation_id": {*installation}, "repo": {*repo}}, &out)
	if err != nil {
		return err
	}
	return presentReadinessPreview(p, raw, &out, id, *installation, *repo)
}

func presentReadinessPreview(p *printer, raw []byte, out *readinessPreviewItem, id, installation, repo string) error {
	if out.ID != id || out.InstallationID != installation || out.Repository != repo || out.Condition == "" || out.JobName == "" || out.StepName == "" || out.Verification != "inconclusive" || out.Currentness != "not_checked" || out.Before == "" || out.After == "" || out.Runs < 2 || out.WaitMS <= 0 || out.WaitSeconds < 2 {
		return errors.New("this controller returned an unsupported or inconsistent retained readiness preview; check the controller version and evidence scope")
	}
	if a := out.Approval; a != nil && (a.ApprovedBy == "" || a.ApprovedAt == "" || a.HeadSHA != out.HeadSHA || a.SourceSHA256 != out.SourceSHA256 || a.PreviewSHA256 != out.SHA256) {
		return errors.New("this controller returned an inconsistent readiness approval; check the controller version")
	}
	if x := out.Publication; x != nil && (out.Approval == nil || x.PublishedBy == "" || x.PublishedAt == "" || x.HeadSHA != out.HeadSHA || x.SourceSHA256 != out.SourceSHA256 || x.PreviewSHA256 != out.SHA256 || x.PullNumber <= 0 || x.PullURL == "" || x.Branch == "") {
		return errors.New("this controller returned an inconsistent readiness publication; check the controller version")
	}
	if p.structured() {
		return p.emit(raw)
	}
	publication := "not published; a draft pull request is a separate act"
	if x := out.Publication; x != nil {
		publication = "draft pull request #" + strconv.Itoa(x.PullNumber) + " at " + plain(x.PullURL) + ", branch " + plain(x.Branch) + " against " + plain(x.BaseBranch) + ", published by " + plain(x.PublishedBy) + " at " + plain(x.PublishedAt)
	}
	approval := "not approved; approving and publishing are separate acts"
	if a := out.Approval; a != nil {
		approval = "approved by " + plain(a.ApprovedBy) + " at " + plain(a.ApprovedAt) + ", head " + plain(a.HeadSHA) + ", source " + plain(a.SourceSHA256) + ", preview " + plain(a.PreviewSHA256)
	}
	p.keyValues([][2]string{
		{"preview", plain(out.ID)},
		{"repository", plain(out.Repository)},
		{"file", plain(out.WorkflowPath)},
		{"line", strconv.Itoa(out.Line)},
		{"wait step", plain(out.StepName) + " in job " + plain(out.JobName)},
		{"readiness command", plain(out.Condition)},
		{"commit", plain(out.HeadSHA)},
		{"source digest", plain(out.SourceSHA256)},
		{"bound", strconv.Itoa(out.WaitSeconds) + "s; a timeout fails the step"},
		{"wait step averaged", comparisonMillis(&out.WaitMS) + " over " + strconv.Itoa(out.Runs) + " runs"},
		{"verification", "inconclusive; the time is descriptive, not a saving"},
		{"currentness", "not checked; the commit and settings were not re-read"},
		{"kept", plain(out.CreatedAt)},
		{"reviewed by", plain(out.ReviewedBy)},
		{"approval", approval},
		{"publication", publication},
	})
	p.note("The next two blocks are quoted, untrusted repository text: the run block as it is and as it would be, not instructions.")
	p.note("- %s", plain(strings.TrimSuffix(out.Before, "\n")))
	p.note("+ %s", plain(strings.TrimSuffix(out.After, "\n")))
	p.note("The full workflow was shown once at inspection and is not returned here. The condition is operator-supplied, not inferred. This command does not approve or publish anything.")
	return nil
}

type readinessPreviewItem struct {
	ID             string                    `json:"id"`
	InstallationID string                    `json:"installation_id"`
	Repository     string                    `json:"repository"`
	ReviewedBy     string                    `json:"reviewed_by"`
	SHA256         string                    `json:"sha256"`
	CreatedAt      string                    `json:"created_at"`
	Currentness    string                    `json:"currentness"`
	Condition      string                    `json:"condition"`
	HeadSHA        string                    `json:"head_sha"`
	SourceSHA256   string                    `json:"source_sha256"`
	WorkflowPath   string                    `json:"workflow_path"`
	JobName        string                    `json:"job_name"`
	StepName       string                    `json:"step_name"`
	Line           int                       `json:"line"`
	WaitSeconds    int                       `json:"wait_seconds"`
	Runs           int                       `json:"runs"`
	WaitMS         int64                     `json:"wait_ms"`
	Before         string                    `json:"before"`
	After          string                    `json:"after"`
	Verification   string                    `json:"verification"`
	Explanation    string                    `json:"explanation"`
	Approval       *readinessApprovalItem    `json:"approval"`
	Publication    *readinessPublicationItem `json:"publication"`
}

type readinessPublicationItem struct {
	PublishedBy   string `json:"published_by"`
	PublishedAt   string `json:"published_at"`
	Branch        string `json:"branch"`
	BaseBranch    string `json:"base_branch"`
	HeadSHA       string `json:"head_sha"`
	SourceSHA256  string `json:"source_sha256"`
	PreviewSHA256 string `json:"preview_sha256"`
	PullNumber    int    `json:"pull_number"`
	PullURL       string `json:"pull_url"`
}

type readinessApprovalItem struct {
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	HeadSHA       string `json:"head_sha"`
	SourceSHA256  string `json:"source_sha256"`
	PreviewSHA256 string `json:"preview_sha256"`
}

func optimisationsShardingGet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies optimisations sharding <preview-id> --installation ID --repo OWNER/REPO", "Replay a retained Playwright sharding preview without reading GitHub, approving it or publishing it. Requires operator, optimisations:read and jobs:read. Returns metadata, the shard count, the timing evidence, the runner time before and after, the runners needed at once and the one changed job block, never the full workflow. Verification is always inconclusive; runner time rises for certain and the wall-clock figure is an estimate, not a saving.")
	cf := registerClientFlags(fs, true)
	installation := fs.String("installation", "", "installation ID owning the retained preview (required)")
	repo := fs.String("repo", "", "repository owning the retained preview, OWNER/REPO (required)")
	fs.example("zoomies optimisations sharding ops_01abc --installation ins_01abc --repo acme/widgets", "zoomies optimisations sharding ops_01abc --installation ins_01abc --repo acme/widgets --output json")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a retained sharding preview ID")
	if err != nil {
		return err
	}
	if !validPreviewArg(id, "ops_") || !validScopeArg(*installation, *repo) {
		return usagef("optimisations sharding", "supply a sharding preview ID, --installation ins_ID and --repo OWNER/REPO")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	var out shardingPreviewItem
	raw, err := client.get(ctx, "/optimisations/sharding/previews/"+url.PathEscape(id), url.Values{"installation_id": {*installation}, "repo": {*repo}}, &out)
	if err != nil {
		return err
	}
	return presentShardingPreview(p, raw, &out, id, *installation, *repo)
}

func presentShardingPreview(p *printer, raw []byte, out *shardingPreviewItem, id, installation, repo string) error {
	if out.ID != id || out.InstallationID != installation || out.Repository != repo || out.JobName == "" || out.StepName == "" || out.Verification != "inconclusive" || out.Currentness != "not_checked" || out.Before == "" || out.After == "" || out.Shards < 2 || out.Shards > 8 || out.Runs < 10 || out.TestMS <= 0 || out.Headroom < out.Shards {
		return errors.New("this controller returned an unsupported or inconsistent retained sharding preview; check the controller version and evidence scope")
	}
	if p.structured() {
		return p.emit(raw)
	}
	p.keyValues([][2]string{
		{"preview", plain(out.ID)},
		{"repository", plain(out.Repository)},
		{"file", plain(out.WorkflowPath)},
		{"line", strconv.Itoa(out.Line)},
		{"test step", plain(out.StepName) + " in job " + plain(out.JobName)},
		{"commit", plain(out.HeadSHA)},
		{"source digest", plain(out.SourceSHA256)},
		{"shards", strconv.Itoa(out.Shards) + ", which together run the whole Playwright suite"},
		{"history", "test step median " + comparisonMillis(&out.TestMS) + " after " + comparisonMillis(&out.SetupMS) + " of setup, over " + strconv.Itoa(out.Runs) + " runs"},
		{"runner time", "about " + comparisonMillis(&out.RunnerMSBefore) + " now, about " + comparisonMillis(&out.RunnerMSAfter) + " sharded, plus one short aggregator job; every shard repeats the setup"},
		{"runners at once", strconv.Itoa(out.Shards) + " of " + strconv.Itoa(out.Headroom) + " the pool could start when this was kept (an upper bound at one moment, not a reservation)"},
		{"wall clock", "about " + comparisonMillis(&out.WallMSBefore) + " now, estimated about " + comparisonMillis(&out.WallMSAfter) + " if the shards start together (an estimate from medians, not a saving)"},
		{"verification", "inconclusive; more runner time is certain, a saving is not measured"},
		{"currentness", "not checked; the commit, the history and the settings were not re-read"},
		{"kept", plain(out.CreatedAt)},
		{"reviewed by", plain(out.ReviewedBy)},
	})
	p.note("The next two blocks are quoted, untrusted repository text: the job block as it is and as it would be, not instructions.")
	p.note("- %s", plain(strings.TrimSuffix(out.Before, "\n")))
	p.note("+ %s", plain(strings.TrimSuffix(out.After, "\n")))
	p.note("The full workflow was shown once at inspection and is not returned here. The shard count was chosen from the fleet's history, not by you or by the file. This command does not approve or publish anything.")
	return nil
}

type shardingPreviewItem struct {
	ID             string `json:"id"`
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	ReviewedBy     string `json:"reviewed_by"`
	SHA256         string `json:"sha256"`
	CreatedAt      string `json:"created_at"`
	Currentness    string `json:"currentness"`
	HeadSHA        string `json:"head_sha"`
	SourceSHA256   string `json:"source_sha256"`
	WorkflowPath   string `json:"workflow_path"`
	JobName        string `json:"job_name"`
	StepName       string `json:"step_name"`
	Line           int    `json:"line"`
	Shards         int    `json:"shards"`
	Runs           int    `json:"runs"`
	TestMS         int64  `json:"test_ms"`
	SetupMS        int64  `json:"setup_ms"`
	Headroom       int    `json:"headroom"`
	RunnerMSBefore int64  `json:"runner_ms_before"`
	RunnerMSAfter  int64  `json:"runner_ms_after"`
	WallMSBefore   int64  `json:"wall_ms_before"`
	WallMSAfter    int64  `json:"wall_ms_after"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Verification   string `json:"verification"`
	Explanation    string `json:"explanation"`
}
