package backend

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type lifetimeTransport func(*http.Request) (*http.Response, error)

func (f lifetimeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type lifetimeBody struct {
	ctx context.Context
	io.Reader
}

func (b *lifetimeBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.Reader.Read(p)
}

func (b *lifetimeBody) Close() error { return nil }

func TestDockerResponseOwnsItsContextUntilTheBodyIsClosed(t *testing.T) {
	c, err := NewAPIClient("http://docker.invalid")
	if err != nil {
		t.Fatal(err)
	}
	var requestCtx context.Context
	c.http.Transport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		requestCtx = r.Context()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &lifetimeBody{ctx: requestCtx, Reader: strings.NewReader("body after headers")},
		}, nil
	})
	resp, err := c.doRaw(context.Background(), http.MethodGet, "/version", nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if requestCtx.Err() != nil {
		t.Fatal("request was cancelled before the caller could read its body")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "body after headers" {
		t.Fatalf("reading delayed response: %q, %v", body, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(requestCtx.Err(), context.Canceled) {
		t.Fatal("closing the body did not release its deadline")
	}
}

func TestDockerCallsPreserveExplicitLifecycleBudgets(t *testing.T) {
	c, err := NewAPIClient("http://docker.invalid")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	c.http.Transport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		want, _ := ctx.Deadline()
		if !ok || !deadline.Equal(want) {
			t.Fatal("an explicit lifecycle budget was replaced")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &lifetimeBody{ctx: r.Context(), Reader: strings.NewReader("{}")},
		}, nil
	})
	if _, err := c.Version(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDockerStreamingCallsKeepTheCallersDeadline(t *testing.T) {
	c, err := NewAPIClient("http://docker.invalid")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	want, _ := ctx.Deadline()
	c.http.Transport = lifetimeTransport(func(r *http.Request) (*http.Response, error) {
		got, ok := r.Context().Deadline()
		if !ok || !got.Equal(want) {
			t.Fatal("streaming call lost its caller's deadline")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	resp, err := c.doRaw(ctx, http.MethodGet, "/logs", nil, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}
