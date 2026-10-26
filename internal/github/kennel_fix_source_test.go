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

const kennelFixWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"

type kennelFixServerOptions struct {
	// heads are the branch head commits returned on successive reads.
	heads    []string
	path     string
	content  string
	treeSHA  string
	archived bool
	redirect bool
}

func kennelFixServer(t *testing.T, o kennelFixServerOptions) *httptest.Server {
	t.Helper()
	if len(o.heads) == 0 {
		o.heads = []string{strings.Repeat("a", 40)}
	}
	if o.path == "" {
		o.path = ".github/workflows/ci.yml"
	}
	if o.content == "" {
		o.content = kennelFixWorkflow
	}
	blob := contextGitBlobSHA(o.content)
	if o.treeSHA == "" {
		o.treeSHA = blob
	}
	var heads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/acme/repo":
			_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "acme/repo", "default_branch": "main", "archived": o.archived})
		case r.URL.Path == "/repos/acme/repo/branches/main":
			if o.redirect {
				http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
				return
			}
			i := min(int(heads.Add(1))-1, len(o.heads)-1)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "main", "commit": map[string]any{"sha": o.heads[i]}})
		case strings.Contains(r.URL.Path, "/git/trees/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.Repeat("c", 40), "truncated": false, "tree": []map[string]any{
				{"path": o.path, "type": "blob", "mode": "100644", "sha": o.treeSHA, "size": len(o.content)},
			}})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/repo/contents/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "path": o.path, "encoding": "base64", "size": len(o.content), "sha": blob, "content": base64.StdEncoding.EncodeToString([]byte(o.content))})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReadKennelFixSourceBindsTheFileToTheBlobTheFindingNames(t *testing.T) {
	srv := kennelFixServer(t, kennelFixServerOptions{})
	blob := contextGitBlobSHA(kennelFixWorkflow)
	src, err := cacheSourceClient(t, srv).ReadKennelFixSource(context.Background(), "acme/repo", ".github/workflows/ci.yml", blob)
	if err != nil {
		t.Fatal(err)
	}
	if src.Branch != "main" || src.HeadSHA != strings.Repeat("a", 40) || src.BlobSHA != blob || src.Workflow.Content != kennelFixWorkflow || src.Workflow.Mode != "100644" {
		t.Fatalf("source %+v", src)
	}
	if !ValidKennelFixSource(src, "acme/repo", ".github/workflows/ci.yml", blob) {
		t.Fatal("source failed its own shape check")
	}
	other := *src
	other.Workflow.Content += "\n"
	if ValidKennelFixSource(&other, "acme/repo", ".github/workflows/ci.yml", blob) {
		t.Error("content that is not the blob was accepted")
	}
}

func TestReadKennelFixSourceSaysStaleWhenTheBranchHoldsADifferentBlob(t *testing.T) {
	srv := kennelFixServer(t, kennelFixServerOptions{})
	_, err := cacheSourceClient(t, srv).ReadKennelFixSource(context.Background(), "acme/repo", ".github/workflows/ci.yml", strings.Repeat("e", 40))
	if !errors.Is(err, ErrKennelFixStale) {
		t.Fatalf("err = %v, want stale", err)
	}
}

func TestReadKennelFixSourceRefusesWhatItCannotStandBehind(t *testing.T) {
	blob := contextGitBlobSHA(kennelFixWorkflow)
	for _, tc := range []struct {
		name string
		opts kennelFixServerOptions
		path string
		blob string
	}{
		{"a branch head that moved during the read", kennelFixServerOptions{heads: []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}}, ".github/workflows/ci.yml", blob},
		{"an archived repository", kennelFixServerOptions{archived: true}, ".github/workflows/ci.yml", blob},
		{"a redirect", kennelFixServerOptions{redirect: true}, ".github/workflows/ci.yml", blob},
		{"a file outside the root workflows", kennelFixServerOptions{}, "docs/ci.yml", blob},
		{"an upper-case blob name", kennelFixServerOptions{}, ".github/workflows/ci.yml", strings.ToUpper(blob)},
		{"content that is not the blob the tree names", kennelFixServerOptions{content: kennelFixWorkflow, treeSHA: strings.Repeat("d", 40)}, ".github/workflows/ci.yml", strings.Repeat("d", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := kennelFixServer(t, tc.opts)
			if _, err := cacheSourceClient(t, srv).ReadKennelFixSource(context.Background(), "acme/repo", tc.path, tc.blob); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
