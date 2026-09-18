package installer

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/eyupio/zoomies/internal/store"
)

// cgroupControllersPath exists only under a cgroup v2 unified hierarchy: a
// host still on cgroup v1, or split into the legacy hybrid layout, has no
// file by this name at the root. It is the same test util-linux's own
// `mount` output and systemd rely on, done directly rather than by shelling
// out to either.
const cgroupControllersPath = "/sys/fs/cgroup/cgroup.controllers"

// cgroupV2Unified reports whether this host mounts a cgroup v2 unified
// hierarchy, which is what a systemd user slice needs before it has
// anything to delegate. A var rather than a func, so a test can stand in
// for a cgroup v1 host without needing one.
var cgroupV2Unified = func() bool {
	_, err := os.Stat(cgroupControllersPath)
	return err == nil
}

// rootlessSocketUID pulls the uid out of a rootless socket endpoint, e.g.
// unix:///run/user/1000/podman/podman.sock. Delegating the cgroup
// controllers means editing that user's own systemd slice, and by the time
// setup gets here it is usually running as root itself -- install.sh
// re-execs it under sudo because it writes a unit and /etc/zoomies -- so the
// uid has to come from the socket the daemon actually answered on rather
// than from the installer's own.
var rootlessSocketUID = regexp.MustCompile(`/run/user/(\d+)/`)

func rootlessUID(endpoint string) (int, bool) {
	m := rootlessSocketUID.FindStringSubmatch(endpoint)
	if m == nil {
		return 0, false
	}
	uid, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return uid, true
}

// cgroupDelegateDropIn hands the user slice every controller a pool's
// resource limits can ask for, not just cpu: a pool that sets pids_limit or
// a memory limit is refused in exactly the same way otherwise.
const cgroupDelegateDropIn = "[Service]\nDelegate=cpu cpuset io memory pids\n"

// cgroupDelegateUnit is the drop-in's own file name, distinct from the
// override.conf `systemctl edit` would write, so an operator's own manual
// edit and this one do not clobber each other.
const cgroupDelegateUnit = "zoomies-delegate.conf"

// cgroupDropInPath is where the drop-in for one uid's user slice lives.
func cgroupDropInPath(uid int) string {
	return fmt.Sprintf("/etc/systemd/system/user@%d.service.d/%s", uid, cgroupDelegateUnit)
}

// writeCgroupDropIn is writeFileAtomic by default. It is a var so a test can
// check delegateCgroupControllers's command sequencing without root or a
// write into the real /etc/systemd/system this runs against outside a test.
var writeCgroupDropIn = writeFileAtomic

// delegateCgroupControllers gives a rootless daemon's user slice the
// controllers it needs to enforce a CPU quota, a memory limit and a pids
// limit: this is host.limits_unenforceable's fix, done once at setup rather
// than left for an operator to run by hand after the finding shows up.
//
// It needs root to write the drop-in -- user@<uid>.service is a system unit
// regardless of how unprivileged the daemon under it is -- and it restarts
// the daemon inside the target user's own session, because `systemctl
// --user` against anyone else's slice does nothing. run is taken as an
// argument, the same seam prepareDirs uses, so a test can watch what would
// have been run without a real systemd to run it against.
func delegateCgroupControllers(ctx context.Context, run commandRunner, uid int, userName string, kind store.BackendKind) error {
	dropInPath := cgroupDropInPath(uid)
	if err := writeCgroupDropIn(dropInPath, []byte(cgroupDelegateDropIn), 0o644); err != nil {
		return fmt.Errorf("installer: writing %s: %w", dropInPath, err)
	}
	if _, err := run(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	runtimeDir := "XDG_RUNTIME_DIR=/run/user/" + strconv.Itoa(uid)
	if _, err := run(ctx, "runuser", "-u", userName, "--", "env", runtimeDir, "systemctl", "--user", "restart", string(kind)); err != nil {
		return fmt.Errorf("installer: wrote %s, but restarting %s in %s's session failed; log %s out and back in "+
			"(or reboot) to pick up the delegation: %w", dropInPath, kind, userName, userName, err)
	}
	return nil
}

// cgroupDelegator is delegateCgroupControllers by default. It is a var, the
// same seam PrepareOptions.newManager uses, so a test can check
// ensureCgroupDelegation's branching without a real systemd or write access
// to /etc.
var cgroupDelegator = delegateCgroupControllers

// ensureCgroupDelegation is the setup-time half of host.limits_unenforceable:
// where the controller can only report that a rootless daemon refuses a
// runner's CPU quota, setup can fix the common case itself, on the same
// host, before a pool ever asks for one.
//
// A daemon that has not said whether it can apply a limit (Known is false),
// or that already can, needs nothing done -- writing the drop-in a second
// time would be a needless restart of a daemon that already works.
func ensureCgroupDelegation(ctx context.Context, u *ui, det Detection, kind store.BackendKind, rootless bool, endpoint string, limits store.LimitSupport, run commandRunner) {
	if kind == store.BackendProcess || !rootless {
		return
	}
	if !limits.Known || (limits.CPU && limits.Memory && limits.Pids) {
		return
	}
	if det.OS != "linux" || !det.HasSystemd {
		return // the fix is a systemd drop-in; there is nothing to automate elsewhere
	}
	uid, ok := rootlessUID(endpoint)
	if !ok {
		return
	}
	if !det.Root {
		u.note(fmt.Sprintf("%s here cannot apply a CPU quota, a memory limit or a pids limit; delegating the cgroup "+
			"controllers needs root, so re-run setup with sudo to have it fixed automatically", kind))
		return
	}
	if !cgroupV2Unified() {
		u.warn(fmt.Sprintf("%s here cannot apply a CPU quota, a memory limit or a pids limit, and this host is still on cgroup v1", kind))
		u.note("cgroup v2 needs `systemd.unified_cgroup_hierarchy=1` on the kernel command line and a reboot; nothing here can do that for you")
		return
	}
	userName := currentUserName(uid)
	if err := cgroupDelegator(ctx, run, uid, userName, kind); err != nil {
		u.warn("could not delegate the cgroup controllers to " + userName + "'s user slice: " + err.Error())
		return
	}
	u.ok(fmt.Sprintf("delegated cpu, cpuset, io, memory and pids to %s's user slice, and restarted %s", userName, kind))
}
