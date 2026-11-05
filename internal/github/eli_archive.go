package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/eyupio/zoomies/internal/gitbare"
)

// MaxArchiveBytes bounds the compressed archive this client will read. The
// decompressed budget is the caller's (gitbare.Budget); this is the ceiling on
// what can arrive before anything is expanded at all, so a runaway download
// costs a fixed amount of memory rather than the host's.
const MaxArchiveBytes = 64 << 20

// ErrArchiveTooLarge says the archive is bigger than this client will read.
var ErrArchiveTooLarge = errors.New("github: the archive is larger than this client will read")

// ArchiveError says why one commit's archive could not be read. It names the
// commit because the operator's next question is which of the three the repair
// pinned has gone.
type ArchiveError struct {
	Commit  string
	Status  int
	Message string
	Err     error
}

func (e *ArchiveError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "github: the archive of commit %s", e.Commit)
	switch {
	case e.Status != 0:
		fmt.Fprintf(&b, " answered HTTP %d", e.Status)
	case e.Err != nil:
		b.WriteString(" failed")
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.Err != nil && e.Message == "" {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return b.String()
}

func (e *ArchiveError) Unwrap() error { return e.Err }

// ArchiveOf reads one commit's tree from the repository's archive and returns
// the files the merge worker's repository is built from.
//
// The download takes two requests, deliberately. The API request carries the
// installation token and is made with redirects refused, because the archive
// endpoint answers with a redirect to a storage host whose URL is already
// signed; following that redirect with the client's headers would hand the token
// to whoever the redirect names. The second request carries no credential at
// all, and both are bounded.
func (c *appClient) ArchiveOf(ctx context.Context, repo, commit string, opts gitbare.ArchiveOptions) ([]gitbare.File, error) {
	owner, name, err := c.repository(repo)
	if err != nil {
		return nil, &ArchiveError{Commit: commit, Err: err}
	}
	if strings.TrimSpace(commit) == "" {
		return nil, &ArchiveError{Err: errors.New("no commit was named")}
	}

	path := fmt.Sprintf("repos/%s/%s/tarball/%s",
		url.PathEscape(owner), url.PathEscape(name), url.PathEscape(commit))
	location, err := c.archiveLocation(ctx, path, commit)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, &ArchiveError{Commit: commit, Err: err}
	}
	response, err := c.archiveHTTP.Do(request)
	if err != nil {
		return nil, &ArchiveError{Commit: commit, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &ArchiveError{Commit: commit, Status: response.StatusCode, Message: "the archive is not there"}
	}
	if response.ContentLength > MaxArchiveBytes {
		return nil, &ArchiveError{Commit: commit, Err: ErrArchiveTooLarge}
	}
	files, err := gitbare.ReadTarGz(&boundedBody{body: response.Body, remaining: MaxArchiveBytes, commit: commit}, opts)
	if err != nil {
		return nil, &ArchiveError{Commit: commit, Err: err}
	}
	return files, nil
}

// archiveLocation asks the API where one commit's archive is.
//
// A provider answers with a redirect to the storage that holds it, which is what
// GitHub does. A GHES instance that serves storage from its own host may answer
// with the archive itself, so both are handled: the API URL is returned in that
// case and the caller fetches it, without a token, on the same terms.
func (c *appClient) archiveLocation(ctx context.Context, path, commit string) (string, error) {
	request, err := c.installationRequest(ctx, path)
	if err != nil {
		return "", &ArchiveError{Commit: commit, Err: err}
	}
	response, err := c.archiveAPI().Do(request)
	if err != nil {
		return "", &ArchiveError{Commit: commit, Err: err}
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		return request.URL.String(), nil
	case http.StatusFound, http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusSeeOther:
		location := response.Header.Get("Location")
		if location == "" {
			return "", &ArchiveError{Commit: commit, Status: response.StatusCode, Message: "the redirect named nowhere"}
		}
		return location, nil
	default:
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return "", &ArchiveError{
			Commit:  commit,
			Status:  response.StatusCode,
			Message: strings.TrimSpace(string(message)),
		}
	}
}

// installationRequest builds the API request for a path, authenticated by the
// installation transport.
func (c *appClient) installationRequest(ctx context.Context, path string) (*http.Request, error) {
	base, err := url.Parse(c.asInstallation.BaseURL())
	if err != nil {
		return nil, err
	}
	target := base.ResolveReference(&url.URL{Path: path})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return request, nil
}

// archiveAPI is the authenticated client with redirects refused, so the token
// goes only to the API host it was minted for.
func (c *appClient) archiveAPI() *http.Client {
	api := *c.http
	api.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &api
}

// boundedBody reads at most its budget and says so, rather than truncating: a
// half archive would fail the tree check with a message about a missing file,
// which is the wrong thing to tell an operator.
type boundedBody struct {
	body      io.Reader
	remaining int64
	commit    string
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, &ArchiveError{Commit: b.commit, Err: ErrArchiveTooLarge}
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	return n, err
}
