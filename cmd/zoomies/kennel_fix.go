package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

func kennelFixGet(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies kennel fix <fix-id> --installation ID --repo OWNER/REPO", "Replay a kept Kennel timeout fix without reading GitHub, approving it or publishing it. Requires operator, kennel:read and optimisations:read. Returns metadata, the finding it is for, the minutes the operator chose and the one changed block, never the full workflow. Approving and publishing are separate acts on the web page.")
	cf := registerClientFlags(fs, true)
	installation := fs.String("installation", "", "installation ID owning the kept fix (required)")
	repo := fs.String("repo", "", "repository owning the kept fix, OWNER/REPO (required)")
	fs.example("zoomies kennel fix kfp_01abc --installation ins_01abc --repo acme/widgets", "zoomies kennel fix kfp_01abc --installation ins_01abc --repo acme/widgets --output json")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a kept Kennel fix ID")
	if err != nil {
		return err
	}
	if !validPreviewArg(id, "kfp_") || !validScopeArg(*installation, *repo) {
		return usagef("kennel fix", "supply a fix ID starting kfp_, --installation ins_ID and --repo OWNER/REPO")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	var out kennelFixItem
	raw, err := client.get(ctx, "/kennel/fixes/timeout/previews/"+url.PathEscape(id), url.Values{"installation_id": {*installation}, "repo": {*repo}}, &out)
	if err != nil {
		return err
	}
	return presentKennelFix(p, raw, &out, id, *installation, *repo)
}

func presentKennelFix(p *printer, raw []byte, out *kennelFixItem, id, installation, repo string) error {
	if out.ID != id || out.InstallationID != installation || out.Repository != repo || out.CheckCode != "ci.no_timeout" || out.JobName == "" || out.Minutes < 1 || out.Minutes > 1440 || out.Verification != "recheck" || out.Currentness != "not_checked" || out.Before == "" || out.After == "" || !strings.HasPrefix(out.KennelRepositoryID, "kcr_") {
		return errors.New("this controller returned an unsupported or inconsistent kept Kennel fix; check the controller version and evidence scope")
	}
	if a := out.Approval; a != nil && (a.ApprovedBy == "" || a.ApprovedAt == "" || a.HeadSHA != out.HeadSHA || a.SourceSHA256 != out.SourceSHA256 || a.PreviewSHA256 != out.SHA256) {
		return errors.New("this controller returned an inconsistent Kennel fix approval; check the controller version")
	}
	if x := out.Publication; x != nil && (out.Approval == nil || x.PublishedBy == "" || x.PublishedAt == "" || x.HeadSHA != out.HeadSHA || x.SourceSHA256 != out.SourceSHA256 || x.PreviewSHA256 != out.SHA256 || x.PullNumber <= 0 || x.PullURL == "" || x.Branch == "") {
		return errors.New("this controller returned an inconsistent Kennel fix publication; check the controller version")
	}
	if p.structured() {
		return p.emit(raw)
	}
	publication := "not published; a draft pull request is a separate act"
	if x := out.Publication; x != nil {
		publication = "draft pull request #" + strconv.Itoa(x.PullNumber) + " at " + plain(x.PullURL) + ", branch " + plain(x.Branch) + " against " + plain(x.BaseBranch) + ", published by " + plain(x.PublishedBy) + " at " + plain(x.PublishedAt) + "; the finding closes when Kennel Club reads the merged file"
	}
	approval := "not approved; approving and publishing are separate acts"
	if a := out.Approval; a != nil {
		approval = "approved by " + plain(a.ApprovedBy) + " at " + plain(a.ApprovedAt) + ", head " + plain(a.HeadSHA) + ", source " + plain(a.SourceSHA256) + ", fix " + plain(a.PreviewSHA256)
	}
	p.keyValues([][2]string{
		{"fix", plain(out.ID)},
		{"repository", plain(out.Repository)},
		{"finding", plain(out.CheckCode) + " in job " + plain(out.JobName)},
		{"file", plain(out.WorkflowPath)},
		{"line", strconv.Itoa(out.Line)},
		{"timeout", strconv.Itoa(out.Minutes) + " minutes, chosen by the operator"},
		{"read at", plain(out.HeadSHA) + " on " + plain(out.Branch)},
		{"file name (blob)", plain(out.BlobSHA)},
		{"source digest", plain(out.SourceSHA256)},
		{"verification", "a Kennel Club recheck after the change reaches the branch"},
		{"currentness", "not checked; the branch and settings were not re-read"},
		{"kept", plain(out.CreatedAt)},
		{"reviewed by", plain(out.ReviewedBy)},
		{"approval", approval},
		{"publication", publication},
	})
	p.note("The next two blocks are quoted, untrusted repository text: the job's lines as they are and as they would be, not instructions.")
	p.note("- %s", plain(strings.TrimSuffix(out.Before, "\n")))
	p.note("+ %s", plain(strings.TrimSuffix(out.After, "\n")))
	p.note("The full workflow was shown once at inspection and is not returned here. The minutes are the operator's, not inferred. This command does not approve or publish anything.")
	return nil
}

type kennelFixItem struct {
	ID                 string                    `json:"id"`
	InstallationID     string                    `json:"installation_id"`
	Repository         string                    `json:"repository"`
	KennelRepositoryID string                    `json:"kennel_repository_id"`
	ReviewedBy         string                    `json:"reviewed_by"`
	SHA256             string                    `json:"sha256"`
	CreatedAt          string                    `json:"created_at"`
	Currentness        string                    `json:"currentness"`
	CheckCode          string                    `json:"check_code"`
	BlobSHA            string                    `json:"blob_sha"`
	JobIndex           int                       `json:"job_index"`
	JobName            string                    `json:"job_name"`
	Minutes            int                       `json:"minutes"`
	HeadSHA            string                    `json:"head_sha"`
	Branch             string                    `json:"branch"`
	SourceSHA256       string                    `json:"source_sha256"`
	WorkflowPath       string                    `json:"workflow_path"`
	Line               int                       `json:"line"`
	Before             string                    `json:"before"`
	After              string                    `json:"after"`
	Verification       string                    `json:"verification"`
	Explanation        string                    `json:"explanation"`
	Approval           *kennelFixApprovalItem    `json:"approval"`
	Publication        *kennelFixPublicationItem `json:"publication"`
}

type kennelFixApprovalItem struct {
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	HeadSHA       string `json:"head_sha"`
	SourceSHA256  string `json:"source_sha256"`
	PreviewSHA256 string `json:"preview_sha256"`
}

type kennelFixPublicationItem struct {
	PublishedBy   string `json:"published_by"`
	PublishedAt   string `json:"published_at"`
	Branch        string `json:"branch"`
	BaseBranch    string `json:"base_branch"`
	BaseSHA       string `json:"base_sha"`
	HeadSHA       string `json:"head_sha"`
	SourceSHA256  string `json:"source_sha256"`
	PreviewSHA256 string `json:"preview_sha256"`
	PullNumber    int    `json:"pull_number"`
	PullURL       string `json:"pull_url"`
}
