package backend

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The controller cannot read an agent's settings, because a standalone agent's
// are on its own host, so the agent says what is true of its mirror: that it is
// on, and, in one of a fixed set of words, what is wrong with it. The words are
// the agent's and the sentences are the controller's, because what an agent
// sends is not trusted to be rendered as it arrives.

// stepClock is a clock a test moves by hand.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func newStepClock() *stepClock { return &stepClock{t: time.Unix(1_800_000_000, 0)} }
func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *stepClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// failingRegistry is an upstream that has a token service and fails everything
// else, so a request for any image is a failure of the registry.
func failingRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"t","expires_in":300}`))
			return
		}
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func reportHost(t *testing.T, shared string, gateway func(context.Context, string) (string, error), hub *httptest.Server, tweak func(*registryMirrorOptions)) *registryMirror {
	t.Helper()
	notInAContainer(t)
	if hub == nil {
		hub = emptyRegistry(t)
	}
	opts := registryMirrorOptions{
		Enabled: true, SharedDir: shared, Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Gateway: gateway, Upstream: hub.URL, TokenURL: hub.URL + "/token", Service: "test",
	}
	if tweak != nil {
		tweak(&opts)
	}
	m := newRegistryMirror(opts)
	t.Cleanup(m.close)
	return m
}

func TestAMirrorThatIsOffHasNothingToReport(t *testing.T) {
	m := reportHost(t, t.TempDir(), loopback, nil, func(o *registryMirrorOptions) { o.Enabled = false })
	if got := m.report(); got != nil {
		t.Fatalf("report = %+v, want none so the heartbeat is what it was before the mirror existed", got)
	}
}

// A host that has turned the mirror on and has had no job yet still has to say
// so, or an operator who has just enabled it sees nothing to tell them it took.
func TestAMirrorThatIsOnReportsBeforeAnySidecarHasUsedIt(t *testing.T) {
	m := reportHost(t, t.TempDir(), loopback, nil, nil)
	got := m.report()
	if got == nil {
		t.Fatal("a mirror that is on reported nothing until it was first used")
	}
	if got.Requests != 0 || got.Problem != "" || got.HostImageCustom {
		t.Fatalf("report = %+v, want an idle mirror with nothing wrong", got)
	}
}

func TestTheReportNamesWhyTheMirrorCannotBeOffered(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(t *testing.T) (shared string, gateway func(context.Context, string) (string, error), inContainer bool)
		ask  bool
		want string
	}{
		{"the agent is in a container", func(t *testing.T) (string, func(context.Context, string) (string, error), bool) {
			return t.TempDir(), loopback, true
		}, false, MirrorProblemContainer},
		{"there is no shared folder", func(t *testing.T) (string, func(context.Context, string) (string, error), bool) {
			return "", loopback, false
		}, false, MirrorProblemSharedFolder},
		{"the host has no address on the network", func(t *testing.T) (string, func(context.Context, string) (string, error), bool) {
			return t.TempDir(), func(context.Context, string) (string, error) { return "", errors.New("no gateway") }, false
		}, true, MirrorProblemGateway},
		{"the address cannot be listened on", func(t *testing.T) (string, func(context.Context, string) (string, error), bool) {
			return t.TempDir(), func(context.Context, string) (string, error) { return "203.0.113.9", nil }, false
		}, true, MirrorProblemListener},
		{"the folder cannot be written", func(t *testing.T) (string, func(context.Context, string) (string, error), bool) {
			shared := t.TempDir()
			if err := os.WriteFile(filepath.Join(shared, "registry-mirror"), []byte("not a folder"), 0o600); err != nil {
				t.Fatal(err)
			}
			return shared, loopback, false
		}, true, MirrorProblemFolder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shared, gateway, inContainer := tc.set(t)
			m := reportHost(t, shared, gateway, nil, nil)
			if inContainer {
				runningInContainer = func() bool { return true }
			}
			// The two that are true of the host before any job asks are reported
			// before one does; the rest are found by trying.
			if tc.ask {
				if url := m.urlFor(context.Background(), ""); url != "" {
					t.Fatalf("offered %q", url)
				}
			}
			got := m.report()
			if got == nil || got.Problem != tc.want {
				t.Fatalf("report = %+v, want the problem %q", got, tc.want)
			}
		})
	}
}

// A problem that has stopped being true has to stop being said, or an operator
// who fixed the folder is told about it until the agent restarts.
func TestAProblemEndsWhenTheMirrorIsOfferedAgain(t *testing.T) {
	var mu sync.Mutex
	broken := true
	m := reportHost(t, t.TempDir(), func(context.Context, string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if broken {
			return "", errors.New("no gateway")
		}
		return "127.0.0.1", nil
	}, nil, nil)
	if m.urlFor(context.Background(), "") != "" {
		t.Fatal("offered with no gateway")
	}
	if got := m.report(); got == nil || got.Problem != MirrorProblemGateway {
		t.Fatalf("report = %+v, want the gateway problem", got)
	}
	mu.Lock()
	broken = false
	mu.Unlock()
	if m.urlFor(context.Background(), "") == "" {
		t.Fatal("not offered once the gateway was found")
	}
	if got := m.report(); got == nil || got.Problem != "" {
		t.Fatalf("report = %+v, want no problem once the mirror is offered", got)
	}
}

// A network that works does not hide one that does not: the report is of the
// host, and one sidecar network with no address is a problem however many do.
func TestOneNetworkThatFailsIsStillReportedWhileAnotherWorks(t *testing.T) {
	m := reportHost(t, t.TempDir(), func(_ context.Context, network string) (string, error) {
		if network == "broken" {
			return "", errors.New("no gateway")
		}
		return "127.0.0.1", nil
	}, nil, nil)
	m.urlFor(context.Background(), "broken")
	m.urlFor(context.Background(), "fine")
	if got := m.report(); got == nil || got.Problem != MirrorProblemGateway {
		t.Fatalf("report = %+v, want the failing network still reported", got)
	}
}

func TestAListenerThatStoppedIsReportedUntilItIsStartedAgain(t *testing.T) {
	m := reportHost(t, t.TempDir(), loopback, nil, nil)
	ctx := context.Background()
	m.urlFor(ctx, "")
	m.mu.Lock()
	var ln net.Listener
	for _, r := range m.running {
		ln = r.ln
	}
	m.mu.Unlock()
	_ = ln.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := m.report(); got != nil && got.Problem == MirrorProblemListener {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a listener that stopped was not reported: %+v", m.report())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.urlFor(ctx, "") == "" {
		t.Fatal("the listener was not started again")
	}
	if got := m.report(); got == nil || got.Problem != "" {
		t.Fatalf("report = %+v, want no problem once the listener is back", got)
	}
}

// A single failure is a blip and an unbroken five minutes is a fault, so the
// agent only says the registry is failing the mirror once it has been for that
// long. A mirror the registry is failing is slower than none.
func TestARegistryThatKeepsFailingTheMirrorIsReportedOnceItHasForFiveMinutes(t *testing.T) {
	clock := newStepClock()
	m := reportHost(t, t.TempDir(), loopback, failingRegistry(t), func(o *registryMirrorOptions) { o.Now = clock.Now })
	url := m.urlFor(context.Background(), "")
	askAboutAnImage(t, url)

	clock.advance(4*time.Minute + 59*time.Second)
	if got := m.report(); got == nil || got.Problem != "" {
		t.Fatalf("report = %+v, want nothing yet: it has not been five minutes", got)
	}
	clock.advance(time.Second)
	got := m.report()
	if got == nil || got.Problem != MirrorProblemUpstream || got.UpstreamFailingSeconds != 300 {
		t.Fatalf("report = %+v, want the upstream problem at five minutes", got)
	}
}

// The host's own sidecar image is the agent's to know and the controller's to
// weigh against each pool's, so the agent says whether it is the default.
func TestTheReportSaysWhetherTheHostsOwnSidecarImageIsCustom(t *testing.T) {
	for _, custom := range []bool{false, true} {
		m := reportHost(t, t.TempDir(), loopback, nil, func(o *registryMirrorOptions) { o.HostImageCustom = custom })
		if got := m.report(); got == nil || got.HostImageCustom != custom {
			t.Errorf("custom = %v: report = %+v", custom, got)
		}
	}
}

func TestADockerBackendReportsItsOwnSidecarImageAsCustomOnlyWhenItIs(t *testing.T) {
	for name, tc := range map[string]struct {
		image string
		want  bool
	}{
		"the default":          {"", false},
		"the default, written": {DefaultDinDImage, false},
		"an image of its own":  {"registry.example.com/ci/dind:27", true},
	} {
		t.Run(name, func(t *testing.T) {
			b, _, err := createADinDRunner(t, DockerOptions{RegistryMirror: true, SharedDir: t.TempDir(), DinDImage: tc.image}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := b.RegistryMirrorReport()
			if got == nil || got.HostImageCustom != tc.want {
				t.Fatalf("report = %+v, want host_image_custom %v", got, tc.want)
			}
		})
	}
}

// Podman carries the Docker backend's fields and cannot run a docker-in-docker
// sidecar, so a host that only has Podman has no mirror to be on, whatever its
// settings say; reporting one would tell the controller a mirror was serving
// pools it can never serve.
func TestAPodmanBackendNeverReportsAMirror(t *testing.T) {
	b, err := NewPodman(DockerOptions{Host: "tcp://127.0.0.1:1", RegistryMirror: true, SharedDir: t.TempDir(), Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	if got := b.RegistryMirrorReport(); got != nil {
		t.Fatalf("a Podman backend reported %+v", got)
	}
}

// Two sidecar networks can be the same address, and a network that failed and
// then finds a listener already running for its address has been served, so its
// problem ends with the first sidecar that is offered the mirror there.
func TestAProblemEndsWhenTheNetworkIsServedByAListenerThatIsAlreadyRunning(t *testing.T) {
	var mu sync.Mutex
	broken := true
	m := reportHost(t, t.TempDir(), func(_ context.Context, network string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if network == "later" && broken {
			return "", errors.New("no gateway")
		}
		return "127.0.0.1", nil
	}, nil, nil)
	ctx := context.Background()
	if m.urlFor(ctx, "first") == "" {
		t.Fatal("the first network was not offered the mirror")
	}
	if m.urlFor(ctx, "later") != "" {
		t.Fatal("offered with no gateway")
	}
	if got := m.report(); got == nil || got.Problem != MirrorProblemGateway {
		t.Fatalf("report = %+v, want the gateway problem", got)
	}
	mu.Lock()
	broken = false
	mu.Unlock()
	if m.urlFor(ctx, "later") == "" {
		t.Fatal("the network was not offered the listener that already runs")
	}
	if got := m.report(); got == nil || got.Problem != "" {
		t.Fatalf("report = %+v, want the problem to end once the network is served", got)
	}
}

func TestAFolderProblemEndsOnceTheFolderCanBeWritten(t *testing.T) {
	shared := t.TempDir()
	blocker := filepath.Join(shared, "registry-mirror")
	if err := os.WriteFile(blocker, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := reportHost(t, shared, loopback, nil, nil)
	if m.urlFor(context.Background(), "") != "" {
		t.Fatal("offered with no folder to keep anything in")
	}
	if got := m.report(); got == nil || got.Problem != MirrorProblemFolder {
		t.Fatalf("report = %+v, want the folder problem", got)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if m.urlFor(context.Background(), "") == "" {
		t.Fatal("not offered once the folder could be made")
	}
	if got := m.report(); got == nil || got.Problem != "" {
		t.Fatalf("report = %+v, want the problem to end once the folder is writable", got)
	}
}

// The controller works out which pools the mirror serves from what an agent
// reports, and the agent offers it to whatever sidecar image really is the
// default, so the two have to be the same rule. This holds one to the other for
// every combination of the host's image and the pool's, which is how they stay
// the same after somebody changes either.
func TestTheControllersRuleForWhichSidecarsGetTheMirrorIsTheBackends(t *testing.T) {
	const custom = "registry.example.com/ci/dind:27"
	hostImages := map[string]string{"empty": "", "the default": DefaultDinDImage, "its own": custom}
	poolImages := map[string]string{
		"empty": "", "padded": "   ", "the default": DefaultDinDImage,
		"the default, padded": "  " + DefaultDinDImage + "\n", "its own": custom,
	}
	for hostName, hostImage := range hostImages {
		for poolName, poolImage := range poolImages {
			t.Run("host "+hostName+", pool "+poolName, func(t *testing.T) {
				b, err := NewDocker(DockerOptions{
					Host: "tcp://127.0.0.1:1", RegistryMirror: true, SharedDir: t.TempDir(),
					DinDImage: hostImage, Logger: quietLogger(),
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(b.Close)
				report := b.RegistryMirrorReport()
				if report == nil {
					t.Fatal("no report from a Docker backend with the mirror on")
				}
				want := b.sidecarImage(Spec{DinDImage: poolImage}) == DefaultDinDImage
				if got := SidecarImageIsDefault(poolImage, report.HostImageCustom); got != want {
					t.Fatalf("SidecarImageIsDefault(%q, host custom %v) = %v, but the backend runs %q and offers the mirror %v",
						poolImage, report.HostImageCustom, got, b.sidecarImage(Spec{DinDImage: poolImage}), want)
				}
			})
		}
	}
}
