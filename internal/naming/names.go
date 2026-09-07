package naming

import (
	"strings"
)

// PoolName is the canonical name for a pool of the given shape.
//
// It carries no suffix: a pool *is* its shape, and two pools with the same
// shape are the same pool. That is also what makes the name usable in a
// workflow's runs-on, which is the name's main job.
func PoolName(spec Spec) string { return spec.WithSuffix("").String() }

// HostName is the canonical name for a machine: its shape, plus enough of the
// machine's own name to tell two identical machines apart.
//
// The machine name goes last because it is the part an operator scans for --
// the eye stops at the end of the line -- and because it is the part most
// likely to be truncated on a host whose hostname is a cloud provider's idea
// of a name.
func HostName(spec Spec, machine string) string {
	return spec.WithSuffix(shortMachine(machine)).String()
}

// Labels are the labels a pool of this shape advertises.
//
// The canonical name comes first: it is the one a workflow author writes, and
// the only one that says how much machine they are asking for. The kernel and
// architecture labels follow because actions/runner advertises them anyway,
// and because a pool that declares them is a pool the scheduler will refuse to
// hand an x64 job to when it is arm64 -- which is the whole point of naming
// the architecture in the first place.
func Labels(spec Spec) []string {
	out := make([]string, 0, 3)
	if name := PoolName(spec); name != Prefix {
		out = append(out, name)
	}
	if k := Kernel(spec.OS); k != "" {
		out = append(out, k)
	}
	if a := ArchLabel(spec.Arch); a != "" {
		out = append(out, a)
	}
	return out
}

// shortMachine reduces a hostname to the part that identifies the machine.
//
// A fully qualified name repeats the same domain on every host in the fleet,
// which is precisely the information a name should not spend characters on.
func shortMachine(machine string) string {
	if host, _, ok := strings.Cut(strings.TrimSpace(machine), "."); ok {
		machine = host
	}
	return Slug(machine)
}
