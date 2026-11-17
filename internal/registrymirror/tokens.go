package registrymirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// tokenSource fetches anonymous pull tokens from a registry's token service,
// one for each repository, and keeps each until shortly before it expires.
//
// The request carries no credential. That is deliberate and it is the whole of
// why the mirror cannot keep a private image: a registry answers an anonymous
// caller's request for a private repository with a refusal, so there is nothing
// to store. Nothing here may ever be given a username or a password.
type tokenSource struct {
	client  *http.Client
	url     string
	service string
	now     func() time.Time
	// timeout bounds a whole token exchange, headers and body. The answer is
	// small, and a service that sends its headers and then stalls would
	// otherwise hold the request open for good: the transport bounds the wait
	// for headers and not for a body.
	timeout time.Duration

	mu    sync.Mutex
	cache map[string]cachedToken
	// maxEntries bounds the cache.
	maxEntries int
}

// count is how many tokens are held.
func (t *tokenSource) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.cache)
}

type cachedToken struct {
	value   string
	expires time.Time
}

// tokenSlack is how long before its stated expiry a token is no longer used, so
// that one that is about to lapse is not sent into a long transfer.
const tokenSlack = 30 * time.Second

// defaultTokenLife is how long a token lasts when the service does not say.
const defaultTokenLife = 60 * time.Second

func (t *tokenSource) get(ctx context.Context, name string) (string, error) {
	t.mu.Lock()
	if c, ok := t.cache[name]; ok && t.now().Before(c.expires) {
		t.mu.Unlock()
		return c.value, nil
	}
	t.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	u, err := url.Parse(t.url)
	if err != nil {
		return "", fmt.Errorf("token service address: %w", err)
	}
	q := u.Query()
	q.Set("service", t.service)
	q.Set("scope", "repository:"+name+":pull")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token service answered %d", resp.StatusCode)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("token service answer: %w", err)
	}
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("token service gave no token")
	}
	life := defaultTokenLife
	if body.ExpiresIn > 0 {
		life = time.Duration(body.ExpiresIn) * time.Second
	}
	t.remember(name, token, t.now().Add(life-tokenSlack))
	return token, nil
}

// defaultMaxTokens bounds the cache. A repository name is chosen by whoever
// sends the request, and a registry issues a token for one that does not exist,
// so the cache is bounded here and not by anything downstream of it.
const defaultMaxTokens = 4096

// remember keeps a token. What has expired is of no use and goes whenever a
// token is added, which is rare enough that the sweep costs nothing; and if the
// cache is still full the token nearest its expiry goes too.
func (t *tokenSource) remember(name, token string, expires time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for k, v := range t.cache {
		if !now.Before(v.expires) {
			delete(t.cache, k)
		}
	}
	if _, ok := t.cache[name]; !ok {
		for len(t.cache) >= t.maxEntries {
			var victim string
			var soonest time.Time
			first := true
			for k, v := range t.cache {
				if first || v.expires.Before(soonest) {
					victim, soonest, first = k, v.expires, false
				}
			}
			delete(t.cache, victim)
		}
	}
	t.cache[name] = cachedToken{value: token, expires: expires}
}
