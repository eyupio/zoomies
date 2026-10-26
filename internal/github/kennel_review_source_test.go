package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type reviewServerOptions struct {
	heads []string // successive head SHAs the pull request reports
	files []map[string]any
	// at maps "ref:path" to content; a path with no entry is a 404.
	at map[string]string
	// size overrides the size a file reports, to model one GitHub will not return whole.
	size map[string]int
}

func reviewServer(t *testing.T, o reviewServerOptions) *httptest.Server {
	t.Helper()
	if len(o.heads) == 0 {
		o.heads = []string{strings.Repeat("b", 40)}
	}
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/acme/repo/pulls/7":
			i := min(int(reads.Add(1))-1, len(o.heads)-1)
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "state": "open",
				"head": map[string]any{"sha": o.heads[i]},
				"base": map[string]any{"sha": strings.Repeat("c", 40), "ref": "main", "repo": map[string]any{"full_name": "acme/repo"}}})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/repo/compare/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"merge_base_commit": map[string]any{"sha": strings.Repeat("a", 40)}})
		case r.URL.Path == "/repos/acme/repo/pulls/7/files":
			_ = json.NewEncoder(w).Encode(o.files)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/repo/contents/"):
			path := strings.TrimPrefix(r.URL.Path, "/repos/acme/repo/contents/")
			content, ok := o.at[r.URL.Query().Get("ref")+":"+path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			size := len(content)
			if s, ok := o.size[path]; ok {
				size = s
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "path": path, "encoding": "base64", "size": size,
				"sha": contextGitBlobSHA(content), "content": base64.StdEncoding.EncodeToString([]byte(content))})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const (
	reviewWF   = ".github/workflows/ci.yml"
	reviewBase = "on: push\njobs:\n  a:\n    runs-on: x\n"
	reviewHead = "on: push\njobs:\n  a:\n    runs-on: x\n    timeout-minutes: 5\n"
)

func TestReadKennelReviewSourceReadsTheMergeBaseAndTheHeadOfTheChangedWorkflows(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	srv := reviewServer(t, reviewServerOptions{
		files: []map[string]any{
			{"filename": "scripts/build.sh", "status": "modified"},
			{"filename": reviewWF, "status": "modified"},
			{"filename": ".github/workflows/new.yml", "status": "added"},
			{"filename": ".github/workflows/old.yml", "status": "removed"},
			{"filename": ".github/workflows/renamed.yml", "status": "renamed", "previous_filename": ".github/workflows/was.yml"},
			{"filename": ".github/workflows/sub/deep.yml", "status": "added"},
		},
		at: map[string]string{
			b + ":" + reviewWF: reviewHead, a + ":" + reviewWF: reviewBase,
			b + ":.github/workflows/new.yml":     reviewHead,
			a + ":.github/workflows/old.yml":     reviewBase,
			b + ":.github/workflows/renamed.yml": reviewHead, a + ":.github/workflows/was.yml": reviewBase,
		},
	})
	src, err := cacheSourceClient(t, srv).ReadKennelReviewSource(context.Background(), "acme/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if src.BaseSHA != a || src.HeadSHA != b || src.BaseBranch != "main" || !src.Open || src.FilesCut {
		t.Fatalf("source = %+v", src)
	}
	if len(src.Changed) != 6 || src.Changed[0] != ".github/workflows/ci.yml" {
		t.Errorf("changed = %v, want every changed path in order", src.Changed)
	}
	heads, bases := map[string]string{}, map[string]string{}
	for _, f := range src.Head {
		heads[f.Path] = string(f.Data)
	}
	for _, f := range src.Base {
		bases[f.Path] = string(f.Data)
	}
	if heads[reviewWF] != reviewHead || bases[reviewWF] != reviewBase {
		t.Errorf("the modified file: head %q base %q", heads[reviewWF], bases[reviewWF])
	}
	if _, ok := bases[".github/workflows/new.yml"]; ok || heads[".github/workflows/new.yml"] == "" {
		t.Error("an added file has a base or lacks a head")
	}
	if _, ok := heads[".github/workflows/old.yml"]; ok || bases[".github/workflows/old.yml"] == "" {
		t.Error("a removed file has a head or lacks a base")
	}
	if bases[".github/workflows/renamed.yml"] != reviewBase {
		t.Error("a renamed file's base is not read from its previous name")
	}
	if _, ok := heads[".github/workflows/sub/deep.yml"]; ok {
		t.Error("a nested workflow, which GitHub does not run, was read")
	}
	if len(src.Unreadable) != 0 {
		t.Errorf("unreadable = %v", src.Unreadable)
	}
}

// A push that lands while the files are read would mix two heads in one review.
func TestReadKennelReviewSourceFailsWhenTheHeadMovesWhileItReads(t *testing.T) {
	srv := reviewServer(t, reviewServerOptions{
		heads: []string{strings.Repeat("b", 40), strings.Repeat("d", 40)},
		files: []map[string]any{{"filename": reviewWF, "status": "modified"}},
		at:    map[string]string{strings.Repeat("b", 40) + ":" + reviewWF: reviewHead, strings.Repeat("a", 40) + ":" + reviewWF: reviewBase},
	})
	if _, err := cacheSourceClient(t, srv).ReadKennelReviewSource(context.Background(), "acme/repo", 7); !errors.Is(err, ErrSetupCacheChanged) {
		t.Fatalf("err = %v, want the head to have changed", err)
	}
}

func TestReadKennelReviewSourceSaysWhichFilesItCouldNotReadRatherThanCallingThemDeleted(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	srv := reviewServer(t, reviewServerOptions{
		files: []map[string]any{
			{"filename": reviewWF, "status": "modified"},
			{"filename": ".github/workflows/big.yml", "status": "modified"},
		},
		// ci.yml has no base to read at all; big.yml is more than the parser takes.
		at: map[string]string{b + ":" + reviewWF: reviewHead,
			b + ":.github/workflows/big.yml": reviewHead, a + ":.github/workflows/big.yml": reviewBase},
		size: map[string]int{".github/workflows/big.yml": 5 << 20},
	})
	src, err := cacheSourceClient(t, srv).ReadKennelReviewSource(context.Background(), "acme/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Unreadable) != 2 {
		t.Fatalf("unreadable = %v, want both", src.Unreadable)
	}
}

func TestReadKennelReviewSourceRefusesAnIdentityItDoesNotKnow(t *testing.T) {
	srv := reviewServer(t, reviewServerOptions{})
	c := cacheSourceClient(t, srv)
	for _, tc := range []struct {
		repo   string
		number int
	}{{"acme/repo", 0}, {"nonsense", 7}, {"acme/other", 7}} {
		if _, err := c.ReadKennelReviewSource(context.Background(), tc.repo, tc.number); err == nil {
			t.Errorf("%s #%d was read", tc.repo, tc.number)
		}
	}
}

// A template renamed into the workflows folder turns a file on that was never a
// workflow. Its old content is not the new workflow's base, or every finding it
// shares with the template would be hidden.
func TestReadKennelReviewSourceTreatsARenameIntoTheWorkflowsFolderAsNew(t *testing.T) {
	b := strings.Repeat("b", 40)
	srv := reviewServer(t, reviewServerOptions{
		files: []map[string]any{{"filename": ".github/workflows/ci.yml", "status": "renamed", "previous_filename": "templates/ci.yml"}},
		at:    map[string]string{b + ":.github/workflows/ci.yml": reviewHead, strings.Repeat("a", 40) + ":templates/ci.yml": reviewHead},
	})
	src, err := cacheSourceClient(t, srv).ReadKennelReviewSource(context.Background(), "acme/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Base) != 0 || len(src.Head) != 1 || len(src.Unreadable) != 0 {
		t.Fatalf("base %d head %d unreadable %v, want a head and no base", len(src.Base), len(src.Head), src.Unreadable)
	}
}
