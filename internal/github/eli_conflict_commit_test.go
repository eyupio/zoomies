package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

const conflictPull = `{"number":1,"state":"open","head":{"ref":"feature","sha":"head","repo":{"full_name":"acme/repo","default_branch":"main"}},"base":{"ref":"main","sha":"base"}}`

func TestConflictCommitHasTwoParentsAndNeverForcesTheBranch(t *testing.T) {
	pulls := 0
	writes := 0
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/pulls/1":
			pulls++
			_, _ = w.Write([]byte(conflictPull))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/git/commits/head":
			_, _ = w.Write([]byte(`{"sha":"head","tree":{"sha":"head-tree"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/blobs":
			var body struct {
				Encoding string `json:"encoding"`
				Content  string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			decoded, err := base64.StdEncoding.DecodeString(body.Content)
			if body.Encoding != "base64" || err != nil || !bytes.Equal(decoded, []byte{0, 1, 2, 255}) {
				t.Errorf("blob %+v decoded %v: %v", body, decoded, err)
			}
			writes++
			_, _ = w.Write([]byte(`{"sha":"binary-blob"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/trees":
			var body struct {
				BaseTree string           `json:"base_tree"`
				Tree     []map[string]any `json:"tree"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.BaseTree != "head-tree" || len(body.Tree) != 4 {
				t.Errorf("tree = %+v", body)
			}
			byPath := map[string]map[string]any{}
			for _, entry := range body.Tree {
				byPath[entry["path"].(string)] = entry
			}
			if entry := byPath["gone.txt"]; entry["sha"] != nil {
				t.Errorf("deleted entry = %+v, want a null sha", entry)
			}
			if entry := byPath["added.txt"]; entry["content"] != "from base\n" || entry["mode"] != "100755" {
				t.Errorf("added entry = %+v", entry)
			}
			if entry := byPath["asset.bin"]; entry["sha"] != "binary-blob" || entry["content"] != nil {
				t.Errorf("binary entry %+v", entry)
			}
			if entry := byPath["sub"]; entry["type"] != "commit" || entry["sha"] != "sub-sha" {
				t.Errorf("submodule entry = %+v", entry)
			}
			writes++
			_, _ = w.Write([]byte(`{"sha":"merged-tree"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/commits":
			var body struct {
				Message string   `json:"message"`
				Tree    string   `json:"tree"`
				Parents []string `json:"parents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Tree != "merged-tree" || len(body.Parents) != 2 || body.Parents[0] != "head" || body.Parents[1] != "base" {
				t.Errorf("commit = %+v, want head first and base second", body)
			}
			if body.Message == "" {
				t.Error("the merge commit has no message")
			}
			writes++
			_, _ = w.Write([]byte(`{"sha":"merge-commit"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/repo/git/refs/heads/feature":
			var body struct {
				SHA   string `json:"sha"`
				Force bool   `json:"force"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Force || body.SHA != "merge-commit" {
				t.Errorf("ref update = %+v", body)
			}
			writes++
			_, _ = w.Write([]byte(`{"ref":"refs/heads/feature","object":{"sha":"merge-commit"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	sha, err := c.CommitConflictResolution(context.Background(), "acme/repo",
		RepairPull{Number: 1, Branch: "feature", HeadSHA: "head", BaseSHA: "base"}, "base",
		[]ConflictChange{
			{Path: "gone.txt", Deleted: true},
			{Path: "added.txt", Mode: "100755", Content: []byte("from base\n")},
			{Path: "sub", Mode: "160000", SHA: "sub-sha"},
			{Path: "asset.bin", Content: []byte{0, 1, 2, 255}},
		})
	if err != nil || sha != "merge-commit" || writes != 4 || pulls != 2 {
		t.Fatalf("sha %s writes %d pulls %d: %v", sha, writes, pulls, err)
	}
}

func TestConflictCommitRefusesAChangedHeadBeforeWriting(t *testing.T) {
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("wrote after the head changed: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":1,"state":"open","head":{"ref":"feature","sha":"new-head","repo":{"full_name":"acme/repo"}},"base":{"ref":"main","sha":"base"}}`))
	})
	_, err := c.CommitConflictResolution(context.Background(), "acme/repo",
		RepairPull{Number: 1, Branch: "feature", HeadSHA: "head", BaseSHA: "base"}, "base",
		[]ConflictChange{{Path: "a.txt", Mode: "100644", Content: []byte("x\n")}})
	if !errors.Is(err, ErrSetupConflict) {
		t.Fatalf("error = %v, want ErrSetupConflict", err)
	}
}

func TestConflictCommitRefusesATargetTipThatMovedBeforeTheRefUpdate(t *testing.T) {
	pulls := 0
	c := repairTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/pulls/1":
			pulls++
			if pulls == 1 {
				_, _ = w.Write([]byte(conflictPull))
				return
			}
			_, _ = w.Write([]byte(`{"number":1,"state":"open","head":{"ref":"feature","sha":"head","repo":{"full_name":"acme/repo"}},"base":{"ref":"main","sha":"moved"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/git/commits/head":
			_, _ = w.Write([]byte(`{"sha":"head","tree":{"sha":"head-tree"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/trees":
			_, _ = w.Write([]byte(`{"sha":"merged-tree"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/commits":
			_, _ = w.Write([]byte(`{"sha":"merge-commit"}`))
		case r.Method == http.MethodPatch:
			t.Error("the ref was moved after the target tip changed")
		default:
			http.NotFound(w, r)
		}
	})
	_, err := c.CommitConflictResolution(context.Background(), "acme/repo",
		RepairPull{Number: 1, Branch: "feature", HeadSHA: "head", BaseSHA: "base"}, "base",
		[]ConflictChange{{Path: "a.txt", Mode: "100644", Content: []byte("x\n")}})
	if !errors.Is(err, ErrSetupConflict) {
		t.Fatalf("error = %v, want ErrSetupConflict", err)
	}
}

type conflictCommitOptions struct {
	secondPullStatus int
	headStatus       int
	headTree         string
	blobStatus       int
	blobSHA          string
	treeStatus       int
	treeSHA          string
	commitStatus     int
	commitSHA        string
	refStatus        int
}

func conflictCommitServer(t *testing.T, opt *conflictCommitOptions) http.HandlerFunc {
	t.Helper()
	pulls := 0
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/pulls/1":
			pulls++
			if pulls > 1 && opt.secondPullStatus != 0 {
				w.WriteHeader(opt.secondPullStatus)
				_, _ = w.Write([]byte(`{"message":"no"}`))
				return
			}
			_, _ = w.Write([]byte(conflictPull))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/git/commits/head":
			if opt.headStatus != 0 {
				w.WriteHeader(opt.headStatus)
				_, _ = w.Write([]byte(`{"message":"no"}`))
				return
			}
			tree := opt.headTree
			if tree == "" {
				tree = "head-tree"
			}
			if tree == "-" {
				tree = ""
			}
			_, _ = w.Write([]byte(`{"sha":"head","tree":{"sha":"` + tree + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/blobs":
			if opt.blobStatus != 0 {
				w.WriteHeader(opt.blobStatus)
				_, _ = w.Write([]byte(`{"message":"no"}`))
				return
			}
			sha := opt.blobSHA
			if sha == "" {
				sha = "binary-blob"
			}
			if sha == "-" {
				sha = ""
			}
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/trees":
			if opt.treeStatus != 0 {
				w.WriteHeader(opt.treeStatus)
				_, _ = w.Write([]byte(`{"message":"no"}`))
				return
			}
			sha := opt.treeSHA
			if sha == "" {
				sha = "merged-tree"
			}
			if sha == "-" {
				sha = ""
			}
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/commits":
			if opt.commitStatus != 0 {
				w.WriteHeader(opt.commitStatus)
				_, _ = w.Write([]byte(`{"message":"no"}`))
				return
			}
			sha := opt.commitSHA
			if sha == "" {
				sha = "merge-commit"
			}
			if sha == "-" {
				sha = ""
			}
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/repo/git/refs/heads/feature":
			if opt.refStatus != 0 {
				w.WriteHeader(opt.refStatus)
				_, _ = w.Write([]byte(`{"message":"no"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ref":"refs/heads/feature","object":{"sha":"merge-commit"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func TestConflictCommitRefusesEveryInputAndWriteItCannotUse(t *testing.T) {
	text := ConflictChange{Path: "a.txt", Mode: "100644", Content: []byte("x\n")}
	binary := ConflictChange{Path: "asset.bin", Mode: "100644", Content: []byte{0, 1, 2, 255}}
	pull := RepairPull{Number: 1, Branch: "feature", HeadSHA: "head", BaseSHA: "base"}
	cases := []struct {
		name    string
		base    string
		repo    string
		changes []ConflictChange
		opt     conflictCommitOptions
	}{
		{name: "no base", repo: "acme/repo", changes: []ConflictChange{text}},
		{name: "no changes", base: "base", repo: "acme/repo"},
		{name: "bad repository", base: "base", repo: "acme", changes: []ConflictChange{text}},
		{name: "head read failed", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{headStatus: http.StatusInternalServerError}},
		{name: "head has no tree", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{headTree: "-"}},
		{name: "path missing", base: "base", repo: "acme/repo", changes: []ConflictChange{{Mode: "100644", Content: []byte("x\n")}}},
		{name: "submodule has no commit", base: "base", repo: "acme/repo", changes: []ConflictChange{{Path: "sub", Mode: "160000"}}},
		{name: "mode unsupported", base: "base", repo: "acme/repo", changes: []ConflictChange{{Path: "a.txt", Mode: "100600", Content: []byte("x\n")}}},
		{name: "blob write failed", base: "base", repo: "acme/repo", changes: []ConflictChange{binary}, opt: conflictCommitOptions{blobStatus: http.StatusInternalServerError}},
		{name: "blob has no sha", base: "base", repo: "acme/repo", changes: []ConflictChange{binary}, opt: conflictCommitOptions{blobSHA: "-"}},
		{name: "tree write failed", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{treeStatus: http.StatusInternalServerError}},
		{name: "tree has no sha", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{treeSHA: "-"}},
		{name: "commit write failed", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{commitStatus: http.StatusInternalServerError}},
		{name: "commit has no sha", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{commitSHA: "-"}},
		{name: "ref update failed", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{refStatus: http.StatusInternalServerError}},
		{name: "second read failed", base: "base", repo: "acme/repo", changes: []ConflictChange{text}, opt: conflictCommitOptions{secondPullStatus: http.StatusInternalServerError}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := tc.opt
			c := repairTestClient(t, conflictCommitServer(t, &opt))
			if _, err := c.CommitConflictResolution(context.Background(), tc.repo, pull, tc.base, tc.changes); err == nil {
				t.Fatal("the input was accepted")
			}
		})
	}
}
