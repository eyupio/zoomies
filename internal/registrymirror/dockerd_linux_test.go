package registrymirror

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests above are about what the mirror does with a registry. This one is
// about what a real Docker daemon does with the mirror, which the protocol
// alone cannot show: that it accepts one on a plain-HTTP address, asks it for
// what it was found to ask for, and takes what it is given. It needs root and a
// dockerd, starts a throwaway daemon of its own, and is off unless asked for.
// The image is built here and served by the fake registry, so it needs no
// network and cannot meet Docker Hub's rate limit.
func TestARealDockerDaemonPullsAnImageThroughTheMirror(t *testing.T) {
	if os.Getenv("ZOOMIES_TEST_DOCKERD") != "1" {
		t.Skip("set ZOOMIES_TEST_DOCKERD=1 to run against a real dockerd")
	}
	if os.Geteuid() != 0 {
		t.Skip("a throwaway dockerd needs root")
	}
	dockerd, err := exec.LookPath("dockerd")
	if err != nil {
		t.Skip("no dockerd on this machine")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("no docker client on this machine")
	}

	f := newFakeRegistry(t)
	publishTestImage(t, f, "library/mirrortest", "latest")
	// The mirror listens on 127.0.0.1, which dockerd treats as safe over plain HTTP.
	m, srv := newTestMirror(t, f, nil)

	// A unix socket path is limited to about 100 bytes, which a test's own
	// directory can exceed, so the daemon's runtime files go somewhere short.
	run, err := os.MkdirTemp("/tmp", "zm")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.MkdirTemp("/tmp", "zmd")
	if err != nil {
		t.Fatal(err)
	}
	sock := "unix://" + filepath.Join(run, "d.sock")
	logFile := filepath.Join(run, "dockerd.log")
	logOut, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	daemon := exec.Command(dockerd,
		"--host="+sock,
		"--data-root="+data,
		"--exec-root="+filepath.Join(run, "exec"),
		"--pidfile="+filepath.Join(run, "d.pid"),
		"--storage-driver=vfs", "--iptables=false", "--ip6tables=false", "--bridge=none",
		"--registry-mirror="+srv.URL,
	)
	daemon.Env = withoutProxies(os.Environ())
	daemon.Stdout, daemon.Stderr = logOut, logOut
	if err := daemon.Start(); err != nil {
		t.Fatalf("starting dockerd: %v", err)
	}
	t.Cleanup(func() {
		_ = daemon.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = daemon.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = daemon.Process.Kill()
			<-done
		}
		unmountUnder(data)
		unmountUnder(run)
		_ = os.RemoveAll(data)
		_ = os.RemoveAll(run)
	})
	t.Cleanup(func() {
		if t.Failed() {
			log, _ := os.ReadFile(logFile)
			t.Logf("dockerd log:\n%s", tail(string(log), 3000))
		}
	})

	cli := func(args ...string) (string, error) {
		cmd := exec.Command(docker, append([]string{"--host=" + sock}, args...)...)
		cmd.Env = withoutProxies(os.Environ())
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := cli("info"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dockerd did not come up within 60 seconds")
		}
		time.Sleep(500 * time.Millisecond)
	}

	if out, err := cli("pull", "mirrortest:latest"); err != nil {
		t.Fatalf("the first pull failed: %v\n%s", err, out)
	}
	if f.count("manifest HEAD") == 0 || f.count("manifest GET") == 0 || f.count("blob") < 2 {
		t.Errorf("the pull did not go through the mirror to the registry: %d HEAD, %d GET, %d blobs", f.count("manifest HEAD"), f.count("manifest GET"), f.count("blob"))
	}
	if m.Stats().BytesStored == 0 {
		t.Error("the mirror kept nothing from a real pull")
	}
	blobsAfterFirst := f.count("blob")

	if out, err := cli("rmi", "mirrortest:latest"); err != nil {
		t.Fatalf("removing the image: %v\n%s", err, out)
	}
	if out, err := cli("pull", "mirrortest:latest"); err != nil {
		t.Fatalf("the second pull failed: %v\n%s", err, out)
	}
	if got := f.count("blob"); got != blobsAfterFirst {
		t.Errorf("the second pull asked the registry for %d more blobs, want none: they are on the mirror's disk", got-blobsAfterFirst)
	}
	if m.Stats().Hits < 2 {
		t.Errorf("%d hits after the second pull, want at least the config and the layer", m.Stats().Hits)
	}
}

// publishTestImage puts a one-layer image on a fake registry: a manifest, a
// config and a gzipped layer that hash the way a daemon checks them, for the
// machine's own architecture.
func publishTestImage(t *testing.T, f *fakeRegistry, repo, tag string) {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	content := []byte("hello from a mirrored layer\n")
	if err := tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	diffID := sha256.Sum256(raw.Bytes())
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	layerDigest := f.addBlob(gz.Bytes())

	config, err := json.Marshal(map[string]any{
		"architecture": runtime.GOARCH,
		"os":           "linux",
		"config":       map[string]any{},
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{"sha256:" + hex.EncodeToString(diffID[:])}},
	})
	if err != nil {
		t.Fatal(err)
	}
	configDigest := f.addBlob(config)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     dockerV2,
		"config":        map[string]any{"mediaType": "application/vnd.docker.container.image.v1+json", "size": len(config), "digest": configDigest},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
			"size":      gz.Len(),
			"digest":    layerDigest,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.addManifest(repo, tag, manifest, dockerV2)
}

// withoutProxies is the environment with whatever proxy the machine has taken
// out, so that a pull cannot reach anywhere but the mirror.
func withoutProxies(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToLower(k) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "NO_PROXY=127.0.0.1,localhost")
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// unmountUnder detaches whatever a daemon left mounted below dir, deepest
// first, so that the directory can be removed.
func unmountUnder(dir string) {
	b, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return
	}
	var targets []string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && strings.HasPrefix(fields[1], dir) {
			targets = append(targets, fields[1])
		}
	}
	for i := len(targets) - 1; i >= 0; i-- {
		_ = syscall.Unmount(targets[i], 0)
	}
}
