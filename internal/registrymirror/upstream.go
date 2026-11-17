package registrymirror

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	defaultUpstream = "https://registry-1.docker.io"
	defaultTokenURL = "https://auth.docker.io/token"
	defaultService  = "registry.docker.io"

	defaultTagTTL      = 5 * time.Minute
	defaultIdleTimeout = 30 * time.Second
)

// newTransport is what a mirror talks to a registry with: it connects and
// answers its headers quickly or gives up, so a stalled registry costs a
// request a few seconds and never a connection held open indefinitely. A
// transfer that has started is bounded by the idle timeout instead, because a
// large layer legitimately takes longer than any fixed total.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}
}

// noRedirects makes a client hand a redirect back instead of following it: the
// mirror decides where a redirect may go, and does not send the registry's
// token there.
func noRedirects(c *http.Client) *http.Client {
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cc
}

// registryDo sends one request to the registry, with the anonymous token for
// the repository it is about.
func (m *Mirror) registryDo(ctx context.Context, method, path, name string, header http.Header) (*http.Response, error) {
	token, err := m.tokens.get(ctx, name)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, m.cfg.Upstream+path, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return m.client.Do(req)
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

func unavailable(err error) *apiError {
	return &apiError{status: http.StatusBadGateway, code: "UNAVAILABLE", message: fmt.Sprintf("the registry could not be reached: %v", err)}
}

// upstreamStatusError says what the mirror answers for an upstream status that
// is not a success. The answers are chosen for what dockerd does with them:
// each is a structured registry error, which it falls back from by pulling from
// the registry itself.
//
// A 401 or 403 is a 404, never a 401. To an anonymous caller a private
// repository and an absent one look the same, and a 401 would send dockerd off
// to authenticate against the mirror.
func upstreamStatusError(resp *http.Response, notFoundCode string) *apiError {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &apiError{status: http.StatusNotFound, code: "NAME_UNKNOWN", message: "the repository is not available to an anonymous mirror"}
	case http.StatusNotFound:
		return &apiError{status: http.StatusNotFound, code: notFoundCode, message: "the registry does not have it"}
	case http.StatusTooManyRequests:
		return &apiError{status: http.StatusTooManyRequests, code: "TOOMANYREQUESTS", message: "the registry is limiting this mirror", retryAfter: resp.Header.Get("Retry-After")}
	}
	return unavailable(fmt.Errorf("the registry answered %d", resp.StatusCode))
}

// idleBody is a response body that cancels its request if no byte arrives for
// the idle timeout. A fixed total would be wrong for a large layer, which
// legitimately takes a long time; what matters is that the transfer is moving.
type idleBody struct {
	io.ReadCloser
	timer *time.Timer
	d     time.Duration
}

// idle wraps a response body so that a stalled transfer is cancelled through
// cancel, and the request it belongs to fails.
func (m *Mirror) idle(body io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	return &idleBody{ReadCloser: body, d: m.cfg.IdleTimeout, timer: time.AfterFunc(m.cfg.IdleTimeout, cancel)}
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == nil {
		b.timer.Reset(b.d)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	return b.ReadCloser.Close()
}
