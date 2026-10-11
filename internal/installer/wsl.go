package installer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/eyupio/zoomies/internal/backend"
)

const wslConfPath = "/etc/wsl.conf"

// runningUnderWSL is whether this Linux is a WSL distro. Its init is rarely
// systemd unless the operator turned that on, so a join or an upgrade that finds
// no service manager there has a specific, fixable reason to give instead of the
// generic one.
func runningUnderWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && osReleaseIsWSL(string(b))
}

// osReleaseIsWSL reads the kernel release string, which WSL2 builds name
// "...microsoft-standard-WSL2" and WSL1 names "...-Microsoft".
func osReleaseIsWSL(release string) bool {
	r := strings.ToLower(release)
	return strings.Contains(r, "microsoft") || strings.Contains(r, "wsl")
}

// wslServiceNotes says why an agent started by hand in a WSL distro stops with
// its terminal, and the two changes that keep it running. Turning systemd on is
// optional: without it the agent still works, it just lives and dies with the
// shell that started it.
func wslServiceNotes() []string {
	return []string{
		"this is a WSL distro without systemd, so the agent stops when its terminal closes.",
		"to keep it running, optionally turn systemd on: add this to /etc/wsl.conf",
		"  [boot]",
		"  systemd=true",
		"then run `wsl --shutdown` from Windows, reopen the distro and run the join again with a new token.",
		"WSL can still stop an idle distro; docs/hosts-and-pools.md (A host in WSL2) says how to keep it up.",
	}
}

// wslSystemdSetting reports whether conf already asks for systemd, so that an
// upgrade does not offer to write a line that is there and say, correctly, that
// what is missing is a restart of the distro.
func wslSystemdSetting(conf string) bool {
	section := ""
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			section = strings.ToLower(strings.Trim(line, "[] "))
		case section == "boot":
			k, v, ok := strings.Cut(line, "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "systemd") {
				return strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
	}
	return false
}

// withWSLSystemd returns conf with systemd=true under [boot], changing nothing
// else in it. A wsl.conf is often hand-written (automount, network, interop), so
// the line goes into the section that is there rather than a second [boot].
func withWSLSystemd(conf string) string {
	lines := strings.Split(conf, "\n")
	boot, end := -1, len(lines)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "[") {
			continue
		}
		if boot >= 0 {
			end = i
			break
		}
		if strings.EqualFold(strings.Trim(t, "[] "), "boot") {
			boot = i
		}
	}
	if boot < 0 {
		out := strings.TrimRight(conf, "\n")
		if out != "" {
			out += "\n\n"
		}
		return out + "[boot]\nsystemd=true\n"
	}
	for i := boot + 1; i < end; i++ {
		k, _, ok := strings.Cut(strings.TrimSpace(lines[i]), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "systemd") {
			lines[i] = "systemd=true"
			return strings.Join(lines, "\n")
		}
	}
	lines = append(lines[:boot+1], append([]string{"systemd=true"}, lines[boot+1:]...)...)
	return strings.Join(lines, "\n")
}

// enableWSLSystemd writes systemd=true into the wsl.conf at path, keeping the
// file it replaces beside it the first time. It returns false when the file
// already asked for systemd.
func enableWSLSystemd(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if wslSystemdSetting(string(data)) {
		return false, nil
	}
	if err == nil {
		if _, statErr := os.Stat(path + ".zoomies-bak"); errors.Is(statErr, fs.ErrNotExist) {
			if err := os.WriteFile(path+".zoomies-bak", data, 0o644); err != nil {
				return false, err
			}
		}
	}
	return true, os.WriteFile(path, []byte(withWSLSystemd(string(data))), 0o644)
}

// offerWSLSystemd explains an upgrade that found no service on a WSL distro and
// offers the fix: the agent there is most likely running by hand, so there is no
// unit to restart. It changes nothing under --check. The distro has to be
// restarted from Windows for the setting to take effect, which no process inside
// it can do, so the returned error always ends with that instruction.
func (p *upgradePlan) offerWSLSystemd() error {
	out := p.opts.Out
	conf := p.opts.wslConf
	if conf == "" {
		conf = wslConfPath
	}
	data, _ := os.ReadFile(conf)
	fmt.Fprintln(out, "This is a WSL distro and systemd is not running in it, so Zoomies has no service here to restart.")
	fmt.Fprintln(out, "An agent started by hand keeps running the old build until you stop it and start it again.")
	if wslSystemdSetting(string(data)) {
		return fmt.Errorf("installer: %s already turns systemd on, but this distro has not been restarted since; run `wsl --shutdown` from Windows, reopen the distro and run the upgrade again", conf)
	}
	if p.opts.Check {
		return fmt.Errorf("installer: no service was found; turn systemd on in %s ([boot] systemd=true), run `wsl --shutdown` from Windows and upgrade again", conf)
	}
	approved := p.opts.AssumeYes
	if !approved && p.opts.Interactive {
		approved = askApproval(p.opts.In, out, "Turn systemd on in "+conf+" now? [Y/n] ")
	}
	if !approved {
		return fmt.Errorf("installer: no service was found; to fix it, add `[boot]` and `systemd=true` to %s, run `wsl --shutdown` from Windows and upgrade again (or re-run this with --yes to have it written for you)", conf)
	}
	if _, err := enableWSLSystemd(conf); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("installer: cannot write %s: %w; run the upgrade with sudo, or add `[boot]` and `systemd=true` to it yourself", conf, err)
		}
		return fmt.Errorf("installer: writing %s: %w", conf, err)
	}
	fmt.Fprintf(out, "Turned systemd on in %s (the previous file is kept as %s.zoomies-bak).\n", conf, conf)
	return fmt.Errorf("installer: systemd takes effect when the distro restarts: run `wsl --shutdown` from Windows, reopen the distro, then join this host with a new token (`zoomies agent join`) so the agent becomes a service, or upgrade again if it already is one")
}

// wslWithoutSystemd is whether an upgrade that found no service is on a WSL
// distro that cannot have one.
func (p *upgradePlan) wslWithoutSystemd() bool {
	isWSL, hasSystemd := p.opts.isWSL, p.opts.hasSystemd
	if isWSL == nil {
		isWSL = runningUnderWSL
	}
	if hasSystemd == nil {
		hasSystemd = backend.HasSystemd
	}
	return isWSL() && !hasSystemd()
}
