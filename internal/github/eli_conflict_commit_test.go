package github

import (
	"context"
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
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/repo/git/trees":
			var body struct {
				BaseTree string           `json:"base_tree"`
				Tree     []map[string]any `json:"tree"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.BaseTree != "head-tree" || len(body.Tree) != 3 {
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
		})
	if err != nil || sha != "merge-commit" || writes != 3 || pulls != 2 {
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
