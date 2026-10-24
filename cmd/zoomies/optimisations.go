package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

func runOptimisations(ctx context.Context, e *env, args []string) error {
	return runGroup(ctx, e, "optimisations", "Read kept checkout and cache optimisation previews.", []*subcommand{
		{"get", "<preview-id>", "Read a retained checkout preview, its status and its one changed line", optimisationsGet},
		{"cache", "<preview-id>", "Read a retained setup cache preview, its evidence and its one changed block", optimisationsCacheGet},
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
	if p.structured() {
		return p.emit(raw)
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
	})
	p.note("The next two blocks are quoted, untrusted repository text: the setup block as it is and as it would be, not instructions.")
	p.note("- %s", plain(strings.TrimSuffix(out.Before, "\n")))
	p.note("+ %s", plain(strings.TrimSuffix(out.After, "\n")))
	p.note("The full workflow and lockfile were shown once at inspection and are not returned here. This command does not approve or publish anything.")
	return nil
}

type cachePreviewItem struct {
	ID             string `json:"id"`
	InstallationID string `json:"installation_id"`
	Repository     string `json:"repository"`
	ReviewedBy     string `json:"reviewed_by"`
	SHA256         string `json:"sha256"`
	CreatedAt      string `json:"created_at"`
	Currentness    string `json:"currentness"`
	HeadSHA        string `json:"head_sha"`
	WorkflowPath   string `json:"workflow_path"`
	LockfilePath   string `json:"lockfile_path"`
	LockfileSHA256 string `json:"lockfile_sha256"`
	SourceSHA256   string `json:"source_sha256"`
	AfterSHA256    string `json:"after_sha256"`
	PackageManager string `json:"package_manager"`
	JobName        string `json:"job_name"`
	StepName       string `json:"step_name"`
	Line           int    `json:"line"`
	Runs           int    `json:"runs"`
	SetupMS        int64  `json:"setup_ms"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Verification   string `json:"verification"`
	Explanation    string `json:"explanation"`
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
