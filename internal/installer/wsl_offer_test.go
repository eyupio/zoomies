package installer

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// wslFake is a WSL2 distro with systemd and an installed agent unit, and no
// Windows interop, so only the in-distro steps are in play.
type wslFake struct {
	files map[string]string
	ran   []string
}

func newWSLFake() *wslFake {
	return &wslFake{files: map[string]string{
		"/etc/systemd/system/zoomies-agent.service": "[Service]\nUser=zoomies\n",
	}}
}

func (f *wslFake) GOOS() string { return "linux" }
func (f *wslFake) Getenv(k string) string {
	if k == "WSL_DISTRO_NAME" {
		return "Ubuntu"
	}
	return ""
}
func (f *wslFake) Remove(p string) error              { delete(f.files, p); return nil }
func (f *wslFake) Geteuid() int                       { return 0 }
func (f *wslFake) MkdirAll(string, os.FileMode) error { return nil }
func (f *wslFake) Exists(p string) bool               { _, ok := f.files[p]; return ok }
func (f *wslFake) ReadFile(p string) ([]byte, error) {
	if s, ok := f.files[p]; ok {
		return []byte(s), nil
	}
	return nil, fs.ErrNotExist
}
func (f *wslFake) WriteFile(p string, d []byte, _ os.FileMode) error {
	f.files[p] = string(d)
	return nil
}
func (f *wslFake) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.ran = append(f.ran, name+" "+strings.Join(args, " "))
	if name == "loginctl" {
		f.files["/var/lib/systemd/linger/"+args[1]] = ""
		return nil, nil
	}
	return nil, errors.New("not available")
}

func runOffer(t *testing.T, f *wslFake, o wslOffer, input string) string {
	t.Helper()
	var out bytes.Buffer
	o.Out, o.In = &out, strings.NewReader(input)
	o.Binary = "/usr/local/bin/zoomies"
	o.isWSL = func() bool { return true }
	o.sys = f
	offerWSLSelfManagement(context.Background(), o)
	return out.String()
}

func TestAnInteractiveInstallOffersWSLSelfManagementAndAppliesIt(t *testing.T) {
	f := newWSLFake()
	out := runOffer(t, f, wslOffer{Interactive: true}, "y\n")
	for _, want := range []string{"WSL2 distro", "boot.command", "linger", "Set this up now?", "Nothing is restarted"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(f.files["/etc/wsl.conf"], "agent --wsl-ensure") {
		t.Fatalf("wsl.conf was not written: %q", f.files["/etc/wsl.conf"])
	}
}

func TestDecliningTheWSLOfferChangesNothing(t *testing.T) {
	f := newWSLFake()
	out := runOffer(t, f, wslOffer{Interactive: true}, "n\n")
	if _, wrote := f.files["/etc/wsl.conf"]; wrote || len(f.ran) != 0 {
		t.Fatalf("declined, but the host was changed: %v", f.ran)
	}
	if !strings.Contains(out, "sudo zoomies agent --wsl-ensure") {
		t.Errorf("a decline should say how to do it later:\n%s", out)
	}
}

func TestWSLOfferWithNobodyToAskIsNotAppliedWithoutYes(t *testing.T) {
	f := newWSLFake()
	out := runOffer(t, f, wslOffer{}, "")
	if _, wrote := f.files["/etc/wsl.conf"]; wrote {
		t.Fatal("an unattended run wrote wsl.conf without --yes")
	}
	if !strings.Contains(out, "--yes") {
		t.Errorf("should say --yes does it:\n%s", out)
	}
	runOffer(t, f, wslOffer{AssumeYes: true}, "")
	if _, wrote := f.files["/etc/wsl.conf"]; !wrote {
		t.Fatal("--yes did not apply it")
	}
}

func TestWSLOfferUnderCheckOnlyDescribes(t *testing.T) {
	f := newWSLFake()
	out := runOffer(t, f, wslOffer{Check: true, Interactive: true, AssumeYes: true}, "y\n")
	if _, wrote := f.files["/etc/wsl.conf"]; wrote || len(f.ran) != 0 {
		t.Fatal("--check changed the host")
	}
	if !strings.Contains(out, "No changes made") {
		t.Errorf("output: %s", out)
	}
}

func TestWSLOfferIsSilentOffWSLAndQuietOnceDone(t *testing.T) {
	var out bytes.Buffer
	offerWSLSelfManagement(context.Background(), wslOffer{Out: &out, isWSL: func() bool { return false }})
	if out.Len() != 0 {
		t.Fatalf("spoke on a plain Linux host: %q", out.String())
	}
	f := newWSLFake()
	runOffer(t, f, wslOffer{AssumeYes: true}, "")
	again := runOffer(t, f, wslOffer{Interactive: true}, "y\n")
	if strings.Contains(again, "Set this up now?") {
		t.Fatalf("asked again once everything was in place:\n%s", again)
	}
}
