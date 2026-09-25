package installer

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
)

const composeHealthCheck = `{"Test":["CMD","/usr/local/bin/zoomies","healthcheck","--url","http://127.0.0.1:8080"],"Interval":30000000000}`

// waitFixture is a Compose upgrade whose controller fails its health check
// `failures` times before it answers, and reports `status` while it does.
func waitFixture(t *testing.T, failures int, status, healthcheck string) (UpgradeOptions, *bytes.Buffer, *[]string) {
	t.Helper()
	opts, rec := upgradeFixture(t, DeploymentCompose)
	// A controller: an agent serves nothing, so it has nothing to wait for.
	rec.Mode, rec.Image = ModeSingle, stockControllerRepository+":v0.1"
	if _, err := WriteDeploymentRecord(opts.ConfigDir, rec); err != nil {
		t.Fatal(err)
	}
	opts.Mode, opts.Image = ModeSingle, stockControllerRepository+":v9.0"
	var out bytes.Buffer
	opts.Out = &out
	opts.servePoll = time.Millisecond
	opts.progressEvery = time.Nanosecond
	opts.serveTimeout = time.Minute
	var calls []string
	probes := 0
	opts.run = func(_ context.Context, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		switch {
		case strings.Contains(line, "config --images"):
			return opts.Image, nil
		case strings.Contains(line, "{{json .Config.Healthcheck}}"):
			return healthcheck, nil
		case name == "docker" && len(args) > 1 && args[0] == "exec":
			probes++
			if probes <= failures {
				return "", errors.New("connection refused")
			}
			return "", nil
		case strings.Contains(line, "{{.State.Status}}"):
			return status, nil
		case strings.Contains(line, "logs --tail 1"):
			return `zoomies  | {"time":"2026-09-24T18:47:19Z","level":"INFO","msg":"copied the database before migrating it","migrations":1}`, nil
		case name == "docker" && len(args) > 0 && args[0] == "inspect":
			return "true", nil
		}
		return "", nil
	}
	return opts, &out, &calls
}

// The upgrade used to report itself complete while the controller was still
// migrating its database and answering nothing, which reads as an outage with
// no explanation. It waits for the controller now, and says why it is waiting.
func TestAnUpgradeWaitsForTheControllerToAnswer(t *testing.T) {
	opts, out, calls := waitFixture(t, 3, "running", composeHealthCheck)
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	waiting := strings.Index(text, "Waiting for the controller to answer")
	answering := strings.Index(text, "The controller is answering")
	complete := strings.Index(text, "Upgrade complete")
	if waiting < 0 || answering < waiting || complete < answering {
		t.Fatalf("output does not wait, then answer, then complete:\n%s", text)
	}
	// The latest log line is what shows a slow start is a migration.
	if !strings.Contains(text, "latest: copied the database before migrating it") {
		t.Fatalf("output never said what the controller was doing:\n%s", text)
	}
	// The container's own check, run in it, rather than a guess at a URL.
	want := "docker exec zoomies /usr/local/bin/zoomies healthcheck --url http://127.0.0.1:8080"
	if !slices.Contains(*calls, want) {
		t.Fatalf("calls = %q, want %q", *calls, want)
	}
}

func TestAnAgentUpgradeDoesNotWait(t *testing.T) {
	opts, _ := upgradeFixture(t, DeploymentCompose)
	var out bytes.Buffer
	opts.Out = &out
	opts.run = func(_ context.Context, name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		switch {
		case strings.Contains(line, "config --images"):
			return opts.Image, nil
		case name == "docker" && len(args) > 0 && args[0] == "exec":
			t.Errorf("an agent was health-checked: %s", line)
		case name == "docker" && len(args) > 0 && args[0] == "inspect":
			return "true", nil
		}
		return "", nil
	}
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
}

func TestAControllerThatAnswersAtOnceIsNotWaitedOnAloud(t *testing.T) {
	opts, out, _ := waitFixture(t, 0, "running", composeHealthCheck)
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Waiting for the controller") {
		t.Fatalf("output = %q, announced a wait there was no need for", out.String())
	}
}

// A controller that stops after the upgrade is an error, and one that must
// not be rolled back: it may already have migrated the database, which the
// older image cannot open.
func TestAControllerThatStopsAfterTheUpgradeFailsIt(t *testing.T) {
	opts, out, calls := waitFixture(t, 1000, "exited", composeHealthCheck)
	err := Upgrade(context.Background(), opts)
	if err == nil {
		t.Fatal("an upgrade whose controller stopped reported success")
	}
	for _, want := range []string{"container is exited", "zoomies logs", "not rolled back"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	if strings.Contains(out.String(), "Upgrade complete") {
		t.Fatalf("output = %q, called the upgrade complete", out.String())
	}
	ups := 0
	for _, c := range *calls {
		if strings.Contains(c, " up -d ") {
			ups++
		}
	}
	if ups != 1 {
		t.Fatalf("the service was brought up %d times, want once and no rollback: %q", ups, *calls)
	}
}

func TestAControllerStillStartingAtTheDeadlineSaysNotToStopIt(t *testing.T) {
	opts, _, _ := waitFixture(t, 1000, "running", composeHealthCheck)
	opts.serveTimeout = 20 * time.Millisecond
	err := Upgrade(context.Background(), opts)
	if err == nil {
		t.Fatal("an upgrade whose controller never answered reported success")
	}
	for _, want := range []string{"has not answered", "still running", "do not stop it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
}

func TestAContainerWithNoHealthCheckIsNotWaitedOn(t *testing.T) {
	opts, out, _ := waitFixture(t, 1000, "running", `{"Test":["NONE"]}`)
	if err := Upgrade(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "declares no health check") || !strings.Contains(out.String(), "Upgrade complete") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestHealthCommandRunsWhatTheContainerDeclares(t *testing.T) {
	for _, tc := range []struct {
		inspect string
		want    []string
	}{
		{composeHealthCheck, []string{"/usr/local/bin/zoomies", "healthcheck", "--url", "http://127.0.0.1:8080"}},
		{`{"Test":["CMD-SHELL","curl -f http://localhost/"]}`, []string{"/bin/sh", "-c", "curl -f http://localhost/"}},
		{`{"Test":["NONE"]}`, nil},
		{`null`, nil},
		{``, nil},
	} {
		got, ok := healthCommand(tc.inspect)
		if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
			t.Errorf("healthCommand(%q) = %q, %v; want %q", tc.inspect, got, ok, tc.want)
		}
	}
}

func TestLogLineMessageEchoesTheMessage(t *testing.T) {
	for in, want := range map[string]string{
		`zoomies  | {"level":"INFO","msg":"applied database migration","migration":"0054"}`: "applied database migration",
		`{"msg":"copied the database"}`:             "copied the database",
		"first\nlistening on http://0.0.0.0:8080\n": "listening on http://0.0.0.0:8080",
		"":                       "",
		strings.Repeat("x", 400): strings.Repeat("x", 157) + "...",
	} {
		if got := logLineMessage(in); got != want {
			t.Errorf("logLineMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

// A native controller is asked on its own listener, which is a stored setting:
// every interface is reached over loopback, and TLS means https.
func TestANativeControllerIsAskedOnItsListener(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Bind = "0.0.0.0:9443"
	cfg.Server.TLS.Mode = config.TLSSelfSigned
	if target, _ := nativeHealthTarget(cfg); target != "https://127.0.0.1:9443/healthz" {
		t.Fatalf("target = %q", target)
	}

	var answering atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || !answering.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg.Server.Bind = strings.TrimPrefix(srv.URL, "http://")
	cfg.Server.TLS.Mode = config.TLSOff
	target, client := nativeHealthTarget(cfg)
	if host, _, _ := net.SplitHostPort(cfg.Server.Bind); !strings.Contains(target, host) {
		t.Fatalf("target = %q, not the listener %s", target, cfg.Server.Bind)
	}
	if err := probeHealth(t.Context(), client, target); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("probe of a controller not yet answering = %v", err)
	}
	answering.Store(true)
	if err := probeHealth(t.Context(), client, target); err != nil {
		t.Fatalf("probe of an answering controller = %v", err)
	}
}
