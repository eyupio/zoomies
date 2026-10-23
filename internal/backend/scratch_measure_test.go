package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A folder kept in memory is a tmpfs of a fixed size, and the one fact about it
// a job's failure cannot show is how full it got: Go deletes its work
// directory as the link fails, and the runner's own output never carries a
// step's "no space left on device". So each sample measures the folders, and
// it measures them with the one tool every image already has for the purpose,
// df, run inside the container through the engine's API. Nothing here needs a
// privilege the agent does not have: the agent is an unprivileged account
// with a socket, on a host whose /proc hides every process but its own.
func TestEachInMemoryFolderIsReadFromTheRowItIsMountedOn(t *testing.T) {
	// What GNU df prints for the three folders a docker-in-docker pair could
	// have asked for. The image store is not in this container: df answers for
	// whatever holds that path, which is the root disk, and the row for it is
	// not a folder that was asked for.
	const out = `Filesystem     1024-blocks      Used Available Capacity Mounted on
tmpfs              1048576    204800    843776      20% /tmp
tmpfs              4194304         0   4194304       0% /home/runner/_work
/dev/vda         264212084  29187520  11656092      72% /
`
	got := parseDF(out, []string{"/home/runner/_work", "/tmp", "/var/lib/docker"})
	want := []ScratchUse{
		{Path: "/home/runner/_work", UsedBytes: 0, SizeBytes: 4096 << 20},
		{Path: "/tmp", UsedBytes: 200 << 20, SizeBytes: 1024 << 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("measured %+v, want %+v: a folder is only measured where it is itself a mount, never by the disk under it", got, want)
	}
}

func TestADfAnswerThatCannotBeTrustedMeasuresNothing(t *testing.T) {
	paths := []string{"/tmp"}
	for name, out := range map[string]string{
		"nothing at all":           "",
		"only the header":          "Filesystem 1024-blocks Used Available Capacity Mounted on\n",
		"an error and no table":    "df: unrecognized option '-P'\nTry 'df --help' for more information.\n",
		"more used than there is":  "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 2048 0 100% /tmp\n",
		"a size of nothing":        "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 0 0 0 0% /tmp\n",
		"numbers that are not":     "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs lots some none 50% /tmp\n",
		"the wrong mount":          "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 100 924 10% /dev/shm\n",
		"a row cut short":          "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 100\n",
		"a negative figure":        "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 -5 924 0% /tmp\n",
		"a figure that overflowed": "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 9223372036854775807 1 1 0% /tmp\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := parseDF(out, paths); got != nil {
				t.Fatalf("measured %+v from %q, want nothing: a made-up figure reads as a folder with room", got, out)
			}
		})
	}
}

// df warns on its error stream and carries on with the rest, and a carriage
// return from a CRLF-speaking tool must not leave a mount point that never
// matches the folder it is for.
func TestDfWarningsAndLineEndingsDoNotHideTheFolders(t *testing.T) {
	const out = "df: /var/lib/docker: No such file or directory\r\n" +
		"Filesystem     1024-blocks Used Available Capacity Mounted on\r\n" +
		"tmpfs              2097152 1000   2096152       1% /tmp\r\n"
	got := parseDF(out, []string{"/tmp", "/var/lib/docker"})
	want := []ScratchUse{{Path: "/tmp", UsedBytes: 1000 << 10, SizeBytes: 2048 << 20}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("measured %+v, want %+v", got, want)
	}
}

// An engine that hands the output back without framing it leaves the table in
// the stream the client keeps unframed text in. It is the same table, and the
// folders in it are as real.
func TestATableTheEngineDidNotFrameIsStillRead(t *testing.T) {
	x := &stubExec{res: ExecResult{Stderr: "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 1 1023 1% /tmp\n"}}
	got := measureScratch(context.Background(), x, "c1", []string{"/tmp"})
	if len(got.use) != 1 || got.unmeasured != "" {
		t.Fatalf("reading = %+v, want the folder measured", got)
	}
}

// stubExec answers a measurement without an engine, and remembers what it was
// asked, so the command that runs inside a customer's container is pinned
// down: it is a read-only df with explicit arguments, never a shell line.
type stubExec struct {
	res ExecResult
	err error

	id  string
	cmd []string
	env []string
}

func (s *stubExec) ContainerExec(_ context.Context, id string, cmd, env []string) (ExecResult, error) {
	s.id, s.cmd, s.env = id, cmd, env
	return s.res, s.err
}

func TestTheFoldersAreMeasuredByAReadOnlyDfWithNoShellInvolved(t *testing.T) {
	x := &stubExec{res: ExecResult{Stdout: "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 1 1023 1% /tmp\n"}}
	got := measureScratch(context.Background(), x, "c1", []string{"/home/runner/_work", "/tmp"})
	if x.id != "c1" || strings.Join(x.cmd, " ") != "df -P -k /home/runner/_work /tmp" {
		t.Fatalf("ran %v in %q, want df with POSIX output in kilobytes and each folder as its own argument", x.cmd, x.id)
	}
	if strings.Join(x.env, " ") != "LC_ALL=C" {
		t.Fatalf("ran with %v; the output is parsed, so it must not be translated", x.env)
	}
	if len(got.use) != 1 || got.unmeasured != "" {
		t.Fatalf("reading = %+v, want the one folder df answered for and no reason", got)
	}
}

// The reason a folder could not be measured is what an operator can act on, so
// each way it fails is told apart: an engine that forbids exec is fixed at the
// engine, an image without df is fixed in the image, and neither is fixed by
// retrying. A container that is not there is nothing to report at all.
func TestWhyAFolderCouldNotBeMeasuredIsNamed(t *testing.T) {
	const table = "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 1 1023 1% /tmp\n"
	for _, tc := range []struct {
		name string
		x    stubExec
		want string
		n    int
	}{
		{"measured", stubExec{res: ExecResult{Stdout: table}}, "", 1},
		{"some rows and a nonzero exit", stubExec{res: ExecResult{Stdout: table, ExitCode: 1}}, "", 1},
		{"an engine or proxy that forbids exec", stubExec{err: &APIError{Status: http.StatusForbidden}}, ScratchExecRefused, 0},
		{"a route the engine does not have", stubExec{err: &APIError{Status: http.StatusMethodNotAllowed}}, ScratchExecRefused, 0},
		{"an unauthorised request", stubExec{err: &APIError{Status: http.StatusUnauthorized}}, ScratchExecRefused, 0},
		{"a container that is gone", stubExec{err: &APIError{Status: http.StatusNotFound}}, "", 0},
		{"a container that is not running", stubExec{err: &APIError{Status: http.StatusConflict}}, "", 0},
		{"an engine that did not answer in time", stubExec{err: busyErr(errors.New("slow"))}, ScratchTimedOut, 0},
		{"a deadline", stubExec{err: context.DeadlineExceeded}, ScratchTimedOut, 0},
		{"an engine that failed", stubExec{err: &APIError{Status: http.StatusInternalServerError}}, ScratchUnreadable, 0},
		{"an image with no df", stubExec{res: ExecResult{ExitCode: 127, Stderr: "executable file not found in $PATH"}}, ScratchNoDF, 0},
		{"a df that cannot be run", stubExec{res: ExecResult{ExitCode: 126}}, ScratchNoDF, 0},
		{"a df that printed nothing we know", stubExec{res: ExecResult{Stdout: "hello\n"}}, ScratchUnreadable, 0},
		{"a df that rejects the options", stubExec{res: ExecResult{ExitCode: 1, Stderr: "df: unrecognized option '-P'"}}, ScratchUnreadable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := measureScratch(context.Background(), &tc.x, "c1", []string{"/tmp"})
			if got.unmeasured != tc.want || len(got.use) != tc.n {
				t.Fatalf("reading = %+v, want reason %q and %d folders", got, tc.want, tc.n)
			}
		})
	}
}

// Only the folders the container was created with in memory are asked about,
// and a path that could be read as an option never reaches the command.
func TestOnlyTheContainersOwnInMemoryFoldersAreAskedAbout(t *testing.T) {
	insp := &ContainerInspect{HostConfig: &HostConfig{Tmpfs: map[string]string{
		"/tmp":                "rw,size=1024m",
		"/home/runner/_work":  "rw,size=4096m",
		"-a":                  "rw",
		"relative/path":       "rw",
		"/home/runner/.cache": "rw,size=64m",
	}}}
	got := scratchPaths(insp, nil)
	want := []string{"/home/runner/.cache", "/home/runner/_work", "/tmp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v: sorted so a sample reads the same way twice, absolute so none is an option", got, want)
	}
	// A docker-in-docker sidecar keeps the work folder it shares with its
	// runner on a volume whose driver mounts a tmpfs; a plain volume is disk.
	sidecar := &ContainerInspect{HostConfig: &HostConfig{Mounts: []Mount{
		{Type: "volume", Target: "/home/runner/_work", VolumeOptions: &VolumeOptions{DriverConfig: &VolumeDriver{Name: "local", Options: map[string]string{"type": "tmpfs"}}}},
		{Type: "volume", Target: "/var/lib/data"},
	}, Tmpfs: map[string]string{"/var/lib/docker": "rw"}}}
	if got := scratchPaths(sidecar, nil); !reflect.DeepEqual(got, []string{"/home/runner/_work", "/var/lib/docker"}) {
		t.Fatalf("sidecar paths = %v, want its tmpfs volume and its tmpfs", got)
	}
	for _, none := range []*ContainerInspect{nil, {}, {HostConfig: &HostConfig{}}} {
		if got := scratchPaths(none, nil); got != nil {
			t.Fatalf("paths = %v for %+v, want none", got, none)
		}
	}
	if got := scratchPaths(insp, errors.New("gone")); got != nil {
		t.Fatalf("paths = %v for a container that could not be inspected, want none", got)
	}
}

// inspectWith is a runner container as the engine reports it, with the folders
// it was created with in memory.
func inspectWith(labels map[string]string, tmpfs map[string]string) *ContainerInspect {
	return &ContainerInspect{
		ID:         "c1",
		Config:     &ContainerConfig{Labels: labels},
		HostConfig: &HostConfig{Tmpfs: tmpfs},
	}
}

// execEngine is the three calls that run one command, answering with a canned
// table, and records the commands it was asked to run.
type execEngine struct {
	mu   sync.Mutex
	cmds map[string][]string
}

func (e *execEngine) routes(container, execID, stdout string, exit int, into map[string]http.HandlerFunc) {
	if e.cmds == nil {
		e.cmds = map[string][]string{}
	}
	into["POST "+v+"/containers/"+container+"/exec"] = func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Cmd []string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		e.mu.Lock()
		e.cmds[container] = req.Cmd
		e.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]string{"Id": execID})
	}
	into["POST "+v+"/exec/"+execID+"/start"] = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(frame(1, stdout))
	}
	into["GET "+v+"/exec/"+execID+"/json"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"Running": false, "ExitCode": exit})
	}
}

func (e *execEngine) cmd(container string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cmds[container]
}

func TestDockerStatsMeasuresEachInMemoryFolderOfARunner(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"memory_stats": map[string]any{"usage": 1 << 30, "limit": 4 << 30}})
		},
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelName: "runner-1"},
				map[string]string{"/tmp": "rw,size=1024m", "/home/runner/_work": "rw,size=4096m"}))
		},
	}
	var e execEngine
	e.routes("c1", "e1", "Filesystem 1024-blocks Used Available Capacity Mounted on\n"+
		"tmpfs 4194304 100 4194204 1% /home/runner/_work\n"+
		"tmpfs 1048576 1000000 48576 96% /tmp\n", 0, routes)
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	got, err := b.Stats(context.Background(), "c1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	want := []ScratchUse{
		{Path: "/home/runner/_work", UsedBytes: 100 << 10, SizeBytes: 4096 << 20},
		{Path: "/tmp", UsedBytes: 1000000 << 10, SizeBytes: 1024 << 20},
	}
	if !reflect.DeepEqual(got.Scratch, want) || got.ScratchUnmeasured != "" {
		t.Fatalf("scratch = %+v (%q), want %+v", got.Scratch, got.ScratchUnmeasured, want)
	}
	if got.MemoryBytes == 0 {
		t.Fatalf("stats = %+v; the folders are measured beside the sample, not instead of it", got)
	}
	if cmd := strings.Join(e.cmd("c1"), " "); cmd != "df -P -k /home/runner/_work /tmp" {
		t.Fatalf("ran %q", cmd)
	}
}

// A docker-in-docker runner keeps its image store in the sidecar, a container
// of its own, and that folder is measured where it is mounted.
func TestDockerStatsMeasuresTheImageStoreInTheSidecar(t *testing.T) {
	runnerLabels := map[string]string{LabelName: "runner-1", LabelDockerMode: string(store.DockerDinD)}
	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) },
		"GET " + v + "/containers/d1/stats": func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) },
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(runnerLabels, map[string]string{"/tmp": "rw,size=1024m"}))
		},
		"GET " + v + "/containers/d1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelDinDFor: "runner-1"}, map[string]string{"/var/lib/docker": "rw,size=8192m"}))
		},
		"GET " + v + "/containers/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, []ContainerSummary{{ID: "d1", Labels: map[string]string{LabelDinDFor: "runner-1"}}})
		},
	}
	var e execEngine
	e.routes("c1", "e1", "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1048576 10 1048566 1% /tmp\n", 0, routes)
	e.routes("d1", "e2", "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 8388608 8000000 388608 96% /var/lib/docker\n", 0, routes)
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	got, err := b.Stats(context.Background(), "c1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	want := []ScratchUse{
		{Path: "/tmp", UsedBytes: 10 << 10, SizeBytes: 1024 << 20},
		{Path: "/var/lib/docker", UsedBytes: 8000000 << 10, SizeBytes: 8192 << 20},
	}
	if !reflect.DeepEqual(got.Scratch, want) {
		t.Fatalf("scratch = %+v, want the runner's folder and then the sidecar's: %+v", got.Scratch, want)
	}
}

// Measuring is an extra, and an extra must never cost the sample: the agent
// sizes runners and lends them CPU and memory from these figures, and a
// sample lost to a refused exec would read as a fleet of idle runners.
func TestAFolderThatCannotBeMeasuredNeverCostsTheSample(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"memory_stats": map[string]any{"usage": 1 << 30, "limit": 4 << 30}})
		},
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelName: "runner-1"}, map[string]string{"/tmp": "rw,size=1024m"}))
		},
		"POST " + v + "/containers/c1/exec": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "exec is not allowed through this socket"})
		},
	}
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	got, err := b.Stats(context.Background(), "c1")
	if err != nil {
		t.Fatalf("stats failed over a refused exec: %v", err)
	}
	if got.MemoryBytes != 1<<30 || len(got.Scratch) != 0 || got.ScratchUnmeasured != ScratchExecRefused {
		t.Fatalf("stats = %+v; want the sample whole, no folders, and the reason they are missing", got)
	}
}

// An engine that never answers the exec is waited for only so long. The
// sample's own budget is shared with the stats call that runs beside it.
func TestAnExecThatNeverAnswersIsGivenUpOn(t *testing.T) {
	old := scratchTimeout
	scratchTimeout = 100 * time.Millisecond
	t.Cleanup(func() { scratchTimeout = old })

	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"memory_stats": map[string]any{"usage": 1 << 30}})
		},
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelName: "runner-1"}, map[string]string{"/tmp": "rw,size=1024m"}))
		},
		"POST " + v + "/containers/c1/exec": func(w http.ResponseWriter, r *http.Request) {
			// A server only notices its client has gone once it has read the
			// request, so a handler that waits for that has to read first.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		},
	}
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	began := time.Now()
	got, err := b.Stats(context.Background(), "c1")
	if err != nil || got.ScratchUnmeasured != ScratchTimedOut || got.MemoryBytes != 1<<30 {
		t.Fatalf("stats = %+v, err = %v; want the sample whole and the reason a timeout", got, err)
	}
	if time.Since(began) > 3*time.Second {
		t.Fatalf("the sample took %v; a hung exec held it", time.Since(began))
	}
}

// The likelier way for an exec to hang is not before it starts but after: the
// command is running, a frame has arrived and the output never ends. That is
// the same timeout, named the same, and it ends when the budget does.
func TestAnExecWhoseOutputStallsIsGivenUpOnAsATimeout(t *testing.T) {
	old := scratchTimeout
	scratchTimeout = 150 * time.Millisecond
	t.Cleanup(func() { scratchTimeout = old })

	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]any{"memory_stats": map[string]any{"usage": 1 << 30}})
		},
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelName: "runner-1"}, map[string]string{"/tmp": "rw,size=1024m"}))
		},
		"POST " + v + "/containers/c1/exec": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]string{"Id": "e1"})
		},
		"POST " + v + "/exec/e1/start": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write(frame(1, "Filesystem 1024-blocks Used Available Capacity Mounted on\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		},
	}
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	got, err := b.Stats(context.Background(), "c1")
	if err != nil || got.ScratchUnmeasured != ScratchTimedOut || len(got.Scratch) != 0 {
		t.Fatalf("stats = %+v, err = %v; want the sample whole and the folders unmeasured for a timeout", got, err)
	}
}

// The command runs beside the stats call and not after it: that call is a
// second of the daemon's time at the least, and the agent gives the whole
// sample five. Here the stats call is held until the exec has arrived, which a
// measurement that waited for the stats would never let happen.
func TestFoldersAreMeasuredWhileTheDaemonSamplesTheRunner(t *testing.T) {
	execAsked := make(chan struct{})
	var once sync.Once
	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-execAsked:
			case <-time.After(5 * time.Second):
				http.Error(w, "the folders were never asked about while the stats call was out", http.StatusGatewayTimeout)
				return
			}
			writeJSON(w, 200, map[string]any{})
		},
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelName: "runner-1"}, map[string]string{"/tmp": "rw,size=1024m"}))
		},
	}
	var e execEngine
	e.routes("c1", "e1", "Filesystem 1024-blocks Used Available Capacity Mounted on\ntmpfs 1024 1 1023 1% /tmp\n", 0, routes)
	create := routes["POST "+v+"/containers/c1/exec"]
	routes["POST "+v+"/containers/c1/exec"] = func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(execAsked) })
		create(w, r)
	}
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	got, err := b.Stats(context.Background(), "c1")
	if err != nil || len(got.Scratch) != 1 {
		t.Fatalf("stats = %+v, err = %v; want the folder measured", got, err)
	}
}

// A runner with nothing in memory has nothing to measure, and the engine is
// not asked to run anything in it.
func TestARunnerWithNoInMemoryFolderIsNotAskedAnything(t *testing.T) {
	var execs int
	var mu sync.Mutex
	routes := map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/stats": func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) },
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, inspectWith(map[string]string{LabelName: "runner-1"}, nil))
		},
		"POST " + v + "/containers/c1/exec": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			execs++
			mu.Unlock()
			writeJSON(w, http.StatusCreated, map[string]string{"Id": "e1"})
		},
	}
	b := dockerBackendFor(t, newFakeEngine(t, routes), DockerOptions{})

	got, err := b.Stats(context.Background(), "c1")
	if err != nil || got.Scratch != nil || got.ScratchUnmeasured != "" {
		t.Fatalf("stats = %+v, err = %v; want nothing said about folders that do not exist", got, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if execs != 0 {
		t.Fatalf("the engine was asked to run %d commands in a container with nothing to measure", execs)
	}
}
