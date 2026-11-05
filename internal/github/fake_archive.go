package github

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// fakeArchive is one commit's tree as the fake serves it, and what happened when
// it was asked for.
type fakeArchive struct {
	// files is path to content; the fake decides the mode by the extension, so a
	// test that cares about an executable says .sh.
	files map[string]string
	// status overrides the answer, so a test can serve a 404 or a 500.
	status int
	// authorization holds the Authorization header of every storage request,
	// which is how a test proves the token never travelled there.
	authorization []string
	// downloads counts the storage requests.
	downloads int
	mu        sync.Mutex
}

// Archive registers one commit's tree with the fake. The commit is served as a
// tarball wrapped in a directory, which is the shape GitHub serves.
func (f *FakeGitHub) Archive(repo, commit string, files map[string]string) {
	f.archiveMu.Lock()
	defer f.archiveMu.Unlock()
	if f.archives == nil {
		f.archives = map[string]*fakeArchive{}
	}
	f.archives[repo+"@"+commit] = &fakeArchive{files: files}
}

// ArchiveStatus makes the fake answer every archive request for a commit with a
// status instead of a tree.
func (f *FakeGitHub) ArchiveStatus(repo, commit string, status int) {
	f.archiveMu.Lock()
	defer f.archiveMu.Unlock()
	if f.archives == nil {
		f.archives = map[string]*fakeArchive{}
	}
	f.archives[repo+"@"+commit] = &fakeArchive{status: status}
}

// ArchiveTokensSeen returns the Authorization headers the fake saw on storage
// requests, newest last, so a test can assert there were none.
func (f *FakeGitHub) ArchiveTokensSeen(repo, commit string) []string {
	f.archiveMu.Lock()
	defer f.archiveMu.Unlock()
	entry := f.archives[repo+"@"+commit]
	if entry == nil {
		return nil
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return append([]string(nil), entry.authorization...)
}

func (f *FakeGitHub) archiveFor(repo, commit string) *fakeArchive {
	f.archiveMu.Lock()
	defer f.archiveMu.Unlock()
	return f.archives[repo+"@"+commit]
}

// serveTarball is the API half: it answers with a redirect to the storage the
// fake serves, as GitHub does, so the client's two-request split is exercised.
func (f *FakeGitHub) serveTarball(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	commit := r.PathValue("ref")
	if f.archiveFor(repo, commit) == nil {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	http.Redirect(w, r, f.srv.URL+"/archives/"+repo+"/"+commit, http.StatusFound)
}

// serveArchive is the storage half. It refuses a request that carries an
// Authorization header and records the header, so a client that forwarded the
// token is caught rather than trusted.
func (f *FakeGitHub) serveArchive(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	commit := r.PathValue("ref")
	entry := f.archiveFor(repo, commit)
	if entry == nil {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	entry.mu.Lock()
	entry.downloads++
	entry.authorization = append(entry.authorization, r.Header.Get("Authorization"))
	entry.mu.Unlock()

	if r.Header.Get("Authorization") != "" {
		http.Error(w, "a storage request carried a credential", http.StatusUnauthorized)
		return
	}
	if entry.status != 0 {
		http.Error(w, "the fake was told to answer this status", entry.status)
		return
	}
	body, err := archiveBody(repo, commit, entry.files)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-gzip")
	_, _ = w.Write(body)
}

// archiveBody builds the tarball the fake serves: a wrapper directory, then the
// files, with an executable bit for anything named .sh.
func archiveBody(repo, commit string, files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	writer := tar.NewWriter(zipper)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := []byte(files[name])
		mode := int64(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		header := &tar.Header{
			// One wrapper directory, named the way GitHub names it: the
			// repository with its slash flattened, then the commit.
			Name:     strings.ReplaceAll(repo, "/", "-") + "-" + commit + "/" + name,
			Mode:     mode,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := writer.Write(content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := zipper.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
