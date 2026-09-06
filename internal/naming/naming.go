// Package naming owns the one grammar Zoomies uses for the names an operator
// reads: pools, hosts and the runners GitHub shows in its own UI.
//
// The problem it solves is that "linux-x64" tells you almost nothing. When a
// job is slow, when a bill is too big, or when a runner picked up work it had
// no business picking up, the questions are always the same: how much machine
// was that, which operating system, which architecture. A name that answers
// them turns three page loads into a glance, and it does so in the one place
// that is copied into workflow files, pasted into issues and read in GitHub's
// own runner list, where Zoomies has no UI of its own.
//
// The grammar is:
//
//	zoomies-<cpu>vcpu[-<memory>gb]-<os>-<version>[-<arch>][-<suffix>]
//
// so a pool is "zoomies-4vcpu-ubuntu-2404", a host running it is
// "zoomies-16vcpu-ubuntu-2404-tuck" and a runner on that host is
// "zoomies-4vcpu-ubuntu-2404-k3f9qz". Every part after the prefix is optional
// and omitted when it is not known, because a half-described host is still
// better named than one called "ip-10-0-4-17".
//
// Two conventions keep names short enough to read:
//
//   - The version loses its dots: Ubuntu 24.04 is "2404", Debian 12 is "12".
//     Parse puts them back, so the round trip is lossless for the versions
//     these operating systems actually ship.
//   - amd64 is left off, because it is the overwhelming default and a name
//     that says it for every runner stops carrying information. arm64 is
//     always spelled out.
package naming

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Prefix brands every name this package builds. It is not configurable: the
// point of the prefix is that an operator looking at a list of runners in
// GitHub's UI, or containers on a host, can tell at a glance which ones are
// Zoomies' to reap.
const Prefix = "zoomies"

// MaxNameLength is the longest name this package will emit. GitHub rejects a
// runner name over 64 characters, and a name that cannot be registered is a
// runner that never starts, so the limit is enforced here rather than
// discovered at registration time.
const MaxNameLength = 64

// Spec is a name in structured form: what the thing is made of, in the order
// the grammar renders it.
//
// Every field is optional. A zero Spec renders as the bare prefix, which is
// what a host that has told us nothing about itself deserves to be called.
type Spec struct {
	// CPUs is the vCPU allocation: a pool's per-runner share, or a host's
	// total. Fractions round up, because half a core still needs a whole one
	// to run on and "0vcpu" would be a lie.
	CPUs float64
	// MemoryGB is the memory allocation. It is omitted from the name when
	// zero, which is the common case: most fleets size on CPU alone, and a
	// name that carries every dimension stops being readable.
	MemoryGB int
	// OS is a normalised operating system: ubuntu, debian, alpine, fedora,
	// rocky, macos or windows. Use NormalizeOS to get one.
	OS string
	// Version is the OS version as the operator writes it -- "24.04", "12",
	// "3.22". The compact form used in names is derived, not stored.
	Version string
	// Arch is a normalised architecture: amd64 or arm64. Use NormalizeArch.
	Arch string
	// Suffix distinguishes two things with the same shape: a machine name for
	// a host, a random token for a runner. Pools have none, because a pool is
	// its shape.
	Suffix string
}

// String renders the Spec in the canonical grammar.
func (s Spec) String() string {
	parts := make([]string, 0, 7)
	parts = append(parts, Prefix)
	if cpus := s.vCPU(); cpus > 0 {
		parts = append(parts, strconv.Itoa(cpus)+"vcpu")
	}
	if s.MemoryGB > 0 {
		parts = append(parts, strconv.Itoa(s.MemoryGB)+"gb")
	}
	// An operating system the grammar does not know is left out entirely
	// rather than passed through: a name has to parse back into the Spec that
	// built it, and an unrecognised word would come back as a suffix.
	if os := NormalizeOS(s.OS); os != "" {
		parts = append(parts, os)
		if v := CompactVersion(s.Version); v != "" {
			parts = append(parts, v)
		}
	}
	// amd64 is the default and says nothing; every other architecture is
	// named, so a name without one is a promise that it is the usual thing.
	if arch := NormalizeArch(s.Arch); arch != "" && arch != ArchAMD64 {
		parts = append(parts, arch)
	}
	if suffix := Slug(s.Suffix); suffix != "" {
		parts = append(parts, suffix)
	}
	return truncate(strings.Join(parts, "-"))
}

// vCPU is the whole number of vCPUs the name reports. A fractional allocation
// rounds up: 1.5 vCPU is advertised as 2, because the runner can burst onto a
// second core and a workflow author reading the label is sizing a job, not
// auditing a cgroup.
func (s Spec) vCPU() int {
	if s.CPUs <= 0 {
		return 0
	}
	return int(math.Ceil(s.CPUs))
}

// Describe renders the Spec as the sentence the UI and the CLI print beside a
// name, e.g. "4 vCPU, Ubuntu 24.04, arm64". It is empty when the Spec says
// nothing worth a sentence.
func (s Spec) Describe() string {
	var parts []string
	if n := s.vCPU(); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" vCPU")
	}
	if s.MemoryGB > 0 {
		parts = append(parts, strconv.Itoa(s.MemoryGB)+" GB")
	}
	if p := s.Platform(); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

// Platform renders just the operating system and architecture, e.g.
// "Ubuntu 24.04, arm64". It is what the Pools and Hosts pages show in the
// column beside the name.
func (s Spec) Platform() string {
	var parts []string
	if os := PrettyOS(s.OS); os != "" {
		if v := strings.TrimSpace(s.Version); v != "" {
			os += " " + v
		}
		parts = append(parts, os)
	}
	if a := NormalizeArch(s.Arch); a != "" {
		parts = append(parts, a)
	}
	return strings.Join(parts, ", ")
}

// Empty reports whether the Spec carries nothing but the prefix.
func (s Spec) Empty() bool { return s.String() == Prefix }

// WithSuffix returns a copy carrying a different discriminator, which is how a
// pool's shape becomes a runner's name.
func (s Spec) WithSuffix(suffix string) Spec {
	s.Suffix = suffix
	return s
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// Parse reads a name built by this package back into a Spec.
//
// It reports false for anything that is not in the grammar -- including names
// an operator invented, which stay legal everywhere Zoomies accepts a name.
// Parsing is best-effort by design: a caller uses it to enrich a display, so a
// name that only half fits still yields the parts that do.
func Parse(name string) (Spec, bool) {
	fields := strings.Split(strings.ToLower(strings.TrimSpace(name)), "-")
	if len(fields) == 0 || fields[0] != Prefix {
		return Spec{}, false
	}
	fields = fields[1:]

	var s Spec
	i := 0
	if i < len(fields) {
		if n, ok := suffixedNumber(fields[i], "vcpu"); ok {
			s.CPUs, i = float64(n), i+1
		}
	}
	if i < len(fields) {
		if n, ok := suffixedNumber(fields[i], "gb"); ok {
			s.MemoryGB, i = n, i+1
		}
	}
	if i < len(fields) && knownOS[fields[i]] {
		s.OS, i = fields[i], i+1
		if i < len(fields) && isVersion(fields[i]) {
			s.Version, i = ExpandVersion(s.OS, fields[i]), i+1
		}
	}
	// An unnamed architecture is amd64: that is the whole reason it is left
	// out, so parsing has to put it back or a round trip would lose it.
	s.Arch = ArchAMD64
	if i < len(fields) && knownArch[fields[i]] {
		s.Arch, i = NormalizeArch(fields[i]), i+1
	}
	if i < len(fields) {
		s.Suffix = strings.Join(fields[i:], "-")
	}
	return s, true
}

// suffixedNumber parses "4vcpu" or "16gb" into its number.
func suffixedNumber(field, unit string) (int, bool) {
	digits, ok := strings.CutSuffix(field, unit)
	if !ok || digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func isVersion(field string) bool {
	if field == "" {
		return false
	}
	for _, r := range field {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Normalisation
// ---------------------------------------------------------------------------

// The architectures Zoomies runs on. They are Go's names, because that is what
// an agent reports about itself and what a container image manifest lists, and
// inventing a third vocabulary would only need translating twice.
const (
	ArchAMD64 = "amd64"
	ArchARM64 = "arm64"
)

// The operating systems the naming grammar knows. An OS outside this list is
// not rejected anywhere -- it simply does not get a canonical name, which is
// the honest outcome for a platform Zoomies has not been taught about.
const (
	OSUbuntu  = "ubuntu"
	OSDebian  = "debian"
	OSAlpine  = "alpine"
	OSFedora  = "fedora"
	OSRocky   = "rocky"
	OSMacOS   = "macos"
	OSWindows = "windows"
)

var knownOS = map[string]bool{
	OSUbuntu: true, OSDebian: true, OSAlpine: true,
	OSFedora: true, OSRocky: true, OSMacOS: true, OSWindows: true,
}

var knownArch = map[string]bool{
	ArchAMD64: true, ArchARM64: true, "x64": true, "x86_64": true,
	"aarch64": true, "arm": true,
}

// osAliases maps what a host actually calls itself -- Go's GOOS, the ID= field
// of /etc/os-release, the names operators type -- onto the grammar's OS.
var osAliases = map[string]string{
	"darwin": OSMacOS, "macos": OSMacOS, "mac": OSMacOS, "osx": OSMacOS,
	"ubuntu": OSUbuntu, "debian": OSDebian, "raspbian": OSDebian,
	"alpine": OSAlpine, "fedora": OSFedora,
	"rocky": OSRocky, "rockylinux": OSRocky, "almalinux": OSRocky,
	"rhel": OSRocky, "centos": OSRocky,
	"windows": OSWindows,
}

// prettyOS is how each OS is spelled in prose, where "ubuntu" looks careless.
var prettyOS = map[string]string{
	OSUbuntu: "Ubuntu", OSDebian: "Debian", OSAlpine: "Alpine",
	OSFedora: "Fedora", OSRocky: "Rocky Linux",
	OSMacOS: "macOS", OSWindows: "Windows",
}

// NormalizeOS maps an operating system identifier onto the grammar's spelling,
// returning "" for one the grammar does not know.
//
// It deliberately does not turn "linux" into a distribution: a host that only
// says "linux" has not said which one, and guessing Ubuntu would produce a
// name that claims a fact nobody established.
func NormalizeOS(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	if alias, ok := osAliases[v]; ok {
		return alias
	}
	if knownOS[v] {
		return v
	}
	return ""
}

// PrettyOS renders an operating system for prose. It returns "" for one the
// grammar does not know, so a caller can fall back to whatever it was told.
func PrettyOS(s string) string { return prettyOS[NormalizeOS(s)] }

// NormalizeArch maps an architecture onto amd64 or arm64, returning "" for
// anything else. The label spellings GitHub uses (x64, arm) are accepted
// because they are what a workflow's runs-on contains.
func NormalizeArch(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ArchAMD64, "x64", "x86_64", "amd":
		return ArchAMD64
	case ArchARM64, "aarch64", "arm":
		return ArchARM64
	}
	return ""
}

// Kernel maps an OS onto the kernel family GitHub's runner labels use, which
// is the "linux" in runs-on: [self-hosted, linux, x64].
func Kernel(os string) string {
	switch NormalizeOS(os) {
	case OSMacOS:
		return "macos"
	case OSWindows:
		return "windows"
	case "":
		return ""
	}
	return "linux"
}

// ArchLabel maps an architecture onto the label actions/runner advertises for
// it, which is "x64" rather than "amd64".
func ArchLabel(arch string) string {
	switch NormalizeArch(arch) {
	case ArchAMD64:
		return "x64"
	case ArchARM64:
		return "arm64"
	}
	return ""
}

// CompactVersion strips the punctuation out of a version so it survives as one
// field of a dash-separated name: "24.04" becomes "2404", "3.22" becomes
// "322", "12" stays "12".
//
// Only the first two components are kept. A point release is a patch level,
// and a pool named for one would have to be renamed every few weeks.
func CompactVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '_' })
	var b strings.Builder
	for i, p := range parts {
		if i == 2 {
			break
		}
		for _, r := range p {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ExpandVersion is CompactVersion's inverse, which needs to know the operating
// system because only its own conventions say where the dot went.
//
// Ubuntu is YY.MM, always four digits. Everything else numbers its releases
// with a single major component, so the compact form is already the version.
func ExpandVersion(os, compact string) string {
	compact = strings.TrimSpace(compact)
	if compact == "" {
		return ""
	}
	if NormalizeOS(os) == OSUbuntu && len(compact) == 4 {
		return compact[:2] + "." + compact[2:]
	}
	return compact
}

// Slug reduces arbitrary text to the character set the grammar allows:
// lowercase letters, digits and single dashes.
//
// Everything Zoomies names ends up as a container name, a hostname and a
// GitHub runner name at once, and the intersection of what those three accept
// is narrower than any of them alone.
func Slug(s string) string {
	var b strings.Builder
	lastDash := true // leading dashes are dropped
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// truncate keeps a name inside MaxNameLength without leaving it ending in a
// dash, which several of the systems these names are handed to reject.
func truncate(s string) string {
	if len(s) <= MaxNameLength {
		return s
	}
	return strings.TrimRight(s[:MaxNameLength], "-")
}

// Validate reports what is wrong with a name a human typed, in a sentence the
// API can hand straight back. A name outside the grammar is fine; a name that
// no container runtime or GitHub registration would accept is not.
func Validate(name string) error {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return fmt.Errorf("a name is required")
	case len(n) > MaxNameLength:
		return fmt.Errorf("%q is %d characters; GitHub rejects a runner name over %d, so keep the name it is built from shorter",
			n, len(n), MaxNameLength)
	case Slug(n) != strings.ToLower(n):
		return fmt.Errorf("%q may only contain lowercase letters, digits and single dashes, because the name becomes a container name, a hostname and a GitHub runner name at once",
			n)
	}
	return nil
}
