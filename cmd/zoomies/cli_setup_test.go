package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// A first run with no controller anywhere is asked once, and the answer is
// saved: the next command must not ask again.
func TestFirstRunAsksForTheControllerAndSavesIt(t *testing.T) {
	e, _, _ := newTestEnv(t)
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer errR.Close()
	e.in, e.err = inR, errW

	restoreTerminal := stdTerminal
	stdTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdTerminal = restoreTerminal })
	// The address without a scheme, then the optional token.
	if _, err := io.WriteString(inW, "zoomies.example.com\nzoo_t\n"); err != nil {
		t.Fatal(err)
	}
	inW.Close()

	client, err := parseClientFlags(t, e).client()
	if err != nil {
		t.Fatal(err)
	}
	if client.base != "https://zoomies.example.com" || client.token != "zoo_t" {
		t.Errorf("client = %s / %s, want the saved controller and token", client.base, client.token)
	}
	cfg, err := loadCLIConfig(cliConfigPath())
	if err != nil || cfg.URL != "https://zoomies.example.com" || cfg.Token != "zoo_t" {
		t.Errorf("saved %+v (%v), want the controller and token", cfg, err)
	}

	errW.Close()
	said, _ := io.ReadAll(errR)
	if !strings.Contains(string(said), "Controller URL") || !strings.Contains(string(said), "Saved the controller") {
		t.Errorf("the setup did not ask and confirm:\n%s", said)
	}
	if strings.Contains(string(said), "zoo_t") {
		t.Errorf("the token was written to the terminal:\n%s", said)
	}
}

// A bad address is a question worth asking again, not a failed first run.
func TestFirstRunAsksAgainAfterAnAddressItCannotUse(t *testing.T) {
	e, _, _ := newTestEnv(t)
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer errR.Close()
	e.in, e.err = inR, errW

	restoreTerminal := stdTerminal
	stdTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdTerminal = restoreTerminal })
	if _, err := io.WriteString(inW, "ftp://zoomies.example.com\nhttps://zoomies.example.com\n\n"); err != nil {
		t.Fatal(err)
	}
	inW.Close()

	client, err := parseClientFlags(t, e).client()
	if err != nil {
		t.Fatal(err)
	}
	if client.base != "https://zoomies.example.com" {
		t.Errorf("base = %q, want the second answer", client.base)
	}
	errW.Close()
	said, _ := io.ReadAll(errR)
	if !strings.Contains(string(said), "http:// or https://") {
		t.Errorf("the first answer was not explained:\n%s", said)
	}
}

// A probe run by hand with nothing named is asked where the controller is, as a
// fleet command would be, and the answer is kept for the next command.
func TestHealthcheckAsksForAMissingControllerAndSavesIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("path = %q, want /healthz", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	e, _, _ := newTestEnv(t)
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer errR.Close()
	e.in, e.err = inR, errW

	restoreTerminal := stdTerminal
	stdTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdTerminal = restoreTerminal })

	if _, err := io.WriteString(inW, srv.URL+"\n"); err != nil {
		t.Fatal(err)
	}
	inW.Close()

	if code := dispatch(context.Background(), e, []string{"healthcheck"}); code != exitOK {
		errW.Close()
		said, _ := io.ReadAll(errR)
		t.Fatalf("exit code = %d, want 0:\n%s", code, said)
	}
	cfg, err := loadCLIConfig(cliConfigPath())
	if err != nil || cfg.URL != srv.URL {
		t.Errorf("saved %+v (%v), want the controller that answered", cfg, err)
	}
}

// A controller already named in the CLI's file is what a hand-run probe checks,
// but a --url or ZOOMIES_URL is used as it is and never made to depend on a
// file that may not be there, which is what the container's health check needs.
func TestHealthcheckUsesTheSavedControllerButPrefersANamedOne(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	t.Run("the file when no address is named", func(t *testing.T) {
		e, _, _ := newTestEnv(t)
		writeCLIConfig(t, "url: "+srv.URL+"\n")
		if code := dispatch(context.Background(), e, []string{"healthcheck"}); code != exitOK {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if gotPath != "/healthz" {
			t.Errorf("path = %q, want /healthz", gotPath)
		}
	})

	t.Run("a named address even when the file does not parse", func(t *testing.T) {
		e, _, _ := newTestEnv(t)
		writeCLIConfig(t, "url: [not, a, string\n")
		if code := dispatch(context.Background(), e, []string{"healthcheck", "--url", srv.URL}); code != exitOK {
			t.Fatalf("exit code = %d, want 0", code)
		}
	})
}

// A join run at a terminal with neither the address nor the token given asks for
// both, as reet's agent does. The join token is spent by this command and is
// never written to the CLI's file.
func TestAgentJoinAsksForAMissingAddressAndToken(t *testing.T) {
	e, _, _ := newTestEnv(t)
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer errR.Close()
	e.in, e.err = inR, errW

	restoreTerminal := stdTerminal
	stdTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdTerminal = restoreTerminal })
	if _, err := io.WriteString(inW, "zoomies.example.com\nzoojoin_t\n"); err != nil {
		t.Fatal(err)
	}
	inW.Close()

	fs := newFlagSet(e, "zoomies agent join [controller-url] [--token <token>]", "")
	if err := fs.parse(nil); err != nil {
		t.Fatal(err)
	}
	controller, err := joinControllerURL(e, fs, false)
	if err != nil {
		t.Fatal(err)
	}
	if controller != "https://zoomies.example.com" {
		t.Errorf("controller = %q, want the typed answer as https", controller)
	}
	token, err := joinToken(e, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if token != "zoojoin_t" {
		t.Errorf("token = %q, want the typed join token", token)
	}
	if _, ok := readCLIConfigIfPresent(t); ok {
		t.Error("a join token must not be saved in the CLI's connection file")
	}
}

// A script asks nothing even when its streams happen to be terminals, which is
// what --non-interactive promises.
func TestAgentJoinDoesNotAskWhenNonInteractive(t *testing.T) {
	e, _, _ := newTestEnv(t)
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	defer inW.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer errR.Close()
	e.in, e.err = inR, errW

	restoreTerminal := stdTerminal
	stdTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdTerminal = restoreTerminal })

	fs := newFlagSet(e, "zoomies agent join [controller-url] [--token <token>]", "")
	if err := fs.parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := joinControllerURL(e, fs, true); err == nil || !strings.Contains(err.Error(), "controller URL") {
		t.Errorf("joinControllerURL error = %v, want the missing controller URL", err)
	}
	if _, err := joinToken(e, "", true); err == nil || !strings.Contains(err.Error(), "--token") {
		t.Errorf("joinToken error = %v, want the missing token", err)
	}
}

func readCLIConfigIfPresent(t *testing.T) (cliConfig, bool) {
	t.Helper()
	if _, err := os.Stat(cliConfigPath()); err != nil {
		return cliConfig{}, false
	}
	cfg, err := loadCLIConfig(cliConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return cfg, true
}

func TestAConfiguredControllerIsNeverAskedAbout(t *testing.T) {
	e, _, _ := newTestEnv(t)
	writeCLIConfig(t, "url: https://from-file.example.com\n")

	restoreTerminal := stdTerminal
	stdTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdTerminal = restoreTerminal })
	client, err := parseClientFlags(t, e).client()
	if err != nil {
		t.Fatal(err)
	}
	if client.base != "https://from-file.example.com" {
		t.Errorf("base = %q, want the file's", client.base)
	}
}
