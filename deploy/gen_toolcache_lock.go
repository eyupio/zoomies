//go:build ignore

// Command gen_toolcache_lock resolves the language toolchains the runner-full
// image carries to exact releases and digests, and writes deploy/toolcache.lock.
//
// The image unpacks every one of these into the tool cache, and a job runs
// whatever it finds there -- so what goes in has to be what its publisher
// released, not whatever a mirror or a proxy between the build and the
// publisher handed over on the day. deploy/runner-toolcache.sh refuses an
// archive whose digest is not the one written here.
//
// Most publishers say what the digest is: nodejs.org's SHASUMS256.txt, go.dev's
// release feed, Adoptium's API, .NET's release metadata, Apache's .sha512 files,
// Gradle's .sha256 and rustup's own. actions/python-versions publishes none, so
// a Python archive's digest is the one this command computed when it first
// pinned it. That is trust on first use, and the lock is what makes the first
// use the only one: an archive that changes afterwards fails the build rather
// than shipping.
//
// Which lines of each toolchain the image carries is the table below. Each
// run moves every line to its newest release, which is what the scheduled
// workflow does; a line that is no longer published is an error, not a quiet
// drop, so retiring one is an edit here.
//
// Run it from the repository root:
//
//	go run deploy/gen_toolcache_lock.go
//
// An archive whose URL the current lock already carries keeps the digest it
// has, so a run that moves one Python patch downloads that one archive rather
// than all thirty.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/naming"
)

const lockPath = "deploy/toolcache.lock"

// The lines the image carries. Each is a release line its project still
// supports; the newest release on it is what gets pinned.
var (
	pythonMinors   = []string{"3.10", "3.11", "3.12", "3.13", "3.14"}
	nodeMajors     = []string{"22", "24"}
	javaMajors     = []string{"17", "21", "25"}
	dotnetChannels = []string{"8.0", "10.0"}
	// Go is every release go.dev lists as stable, which is the two it
	// supports; Maven is the newest 3.x, Gradle the current release, and Rust
	// the stable channel.
)

// arches maps the image's architectures (Docker's TARGETARCH) to the names
// each publisher uses for them.
var arches = []struct{ docker, python, node, java, dotnet, rust string }{
	{"amd64", "x64", "x64", "x64", "x64", "x86_64-unknown-linux-gnu"},
	{"arm64", "arm64", "arm64", "aarch64", "arm64", "aarch64-unknown-linux-gnu"},
}

// entry is one line of the lock.
type entry struct {
	tool, version, platform, arch, digest, url string
}

func (e entry) String() string {
	return strings.Join([]string{e.tool, e.version, e.platform, e.arch, e.digest, e.url}, " ")
}

var client = &http.Client{Timeout: 10 * time.Minute}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen_toolcache_lock:", err)
		os.Exit(1)
	}
}

func run() error {
	known := readLock()

	var platforms []string
	for _, img := range naming.Images() {
		if img.Full {
			platforms = append(platforms, img.Version)
		}
	}
	if len(platforms) == 0 {
		return errors.New("no catalogue row in internal/naming is Full, so there is nothing to build a tool cache for")
	}

	var out []entry
	steps := []struct {
		name string
		fn   func() ([]entry, error)
	}{
		{"Python", func() ([]entry, error) { return python(platforms, known) }},
		{"Node.js", node},
		{"Go", golang},
		{"Java", java},
		{".NET", dotnet},
		{"Maven", maven},
		{"Gradle", gradle},
		{"Rust", rust},
	}
	for _, s := range steps {
		es, err := s.fn()
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		fmt.Fprintf(os.Stderr, "%s: %d archives\n", s.name, len(es))
		out = append(out, es...)
	}
	return writeLock(out)
}

// --- Python -------------------------------------------------------------------

// python pins the newest stable release of each minor from the manifest
// setup-python itself reads, for every Ubuntu the catalogue marks Full. The
// free-threaded builds are left out: they are a separate interpreter a
// workflow has to ask for by name.
func python(platforms []string, known map[string]string) ([]entry, error) {
	var manifest []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Files   []struct {
			Filename        string `json:"filename"`
			Arch            string `json:"arch"`
			Platform        string `json:"platform"`
			PlatformVersion string `json:"platform_version"`
			URL             string `json:"download_url"`
		} `json:"files"`
	}
	if err := getJSON("https://raw.githubusercontent.com/actions/python-versions/main/versions-manifest.json", &manifest); err != nil {
		return nil, err
	}
	var out []entry
	for _, minor := range pythonMinors {
		found := false
		for _, rel := range manifest {
			if !rel.Stable || !strings.HasPrefix(rel.Version, minor+".") {
				continue
			}
			found = true
			for _, platform := range platforms {
				for _, a := range arches {
					url := ""
					for _, f := range rel.Files {
						if f.Platform == "linux" && f.PlatformVersion == platform && f.Arch == a.python {
							url = f.URL
						}
					}
					if url == "" {
						return nil, fmt.Errorf("Python %s has no build for Ubuntu %s %s", rel.Version, platform, a.docker)
					}
					digest, ok := known[url]
					if !ok {
						var err error
						if digest, err = hashURL(url); err != nil {
							return nil, err
						}
					}
					out = append(out, entry{"python", rel.Version, platform, a.docker, digest, url})
				}
			}
			break // the manifest lists newest first
		}
		if !found {
			return nil, fmt.Errorf("no stable Python %s in the manifest", minor)
		}
	}
	return out, nil
}

// --- Node.js ------------------------------------------------------------------

func node() ([]entry, error) {
	var index []struct {
		Version string `json:"version"`
	}
	if err := getJSON("https://nodejs.org/dist/index.json", &index); err != nil {
		return nil, err
	}
	var out []entry
	for _, major := range nodeMajors {
		version := ""
		for _, r := range index { // newest first
			if strings.HasPrefix(r.Version, "v"+major+".") {
				version = r.Version
				break
			}
		}
		if version == "" {
			return nil, fmt.Errorf("no Node.js %s release", major)
		}
		sums, err := getText("https://nodejs.org/dist/" + version + "/SHASUMS256.txt")
		if err != nil {
			return nil, err
		}
		for _, a := range arches {
			file := fmt.Sprintf("node-%s-linux-%s.tar.gz", version, a.node)
			sum := sumFor(sums, file)
			if sum == "" {
				return nil, fmt.Errorf("SHASUMS256.txt for %s does not list %s", version, file)
			}
			out = append(out, entry{"node", strings.TrimPrefix(version, "v"), "-", a.docker,
				"sha256:" + sum, "https://nodejs.org/dist/" + version + "/" + file})
		}
	}
	return out, nil
}

// --- Go -----------------------------------------------------------------------

func golang() ([]entry, error) {
	var releases []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Files   []struct {
			Filename, OS, Arch, SHA256, Kind string
		} `json:"files"`
	}
	if err := getJSON("https://go.dev/dl/?mode=json", &releases); err != nil {
		return nil, err
	}
	var out []entry
	for _, r := range releases {
		if !r.Stable {
			continue
		}
		for _, a := range arches {
			var file, sum string
			for _, f := range r.Files {
				if f.OS == "linux" && f.Arch == a.docker && f.Kind == "archive" {
					file, sum = f.Filename, f.SHA256
				}
			}
			if file == "" {
				return nil, fmt.Errorf("%s has no linux-%s archive", r.Version, a.docker)
			}
			out = append(out, entry{"go", strings.TrimPrefix(r.Version, "go"), "-", a.docker,
				"sha256:" + sum, "https://go.dev/dl/" + file})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("go.dev lists no stable release")
	}
	return out, nil
}

// --- Java ---------------------------------------------------------------------

// java pins Eclipse Temurin, the distribution GitHub's runners carry. The
// version recorded is the one setup-java names its tool-cache directory
// after: Adoptium's semver with the + turned into a -, because a + in a path
// breaks Kotlin.
func java() ([]entry, error) {
	var out []entry
	for _, major := range javaMajors {
		for _, a := range arches {
			var assets []struct {
				Binary struct {
					Package struct {
						Checksum string `json:"checksum"`
						Link     string `json:"link"`
					} `json:"package"`
				} `json:"binary"`
				Version struct {
					Semver string `json:"semver"`
				} `json:"version"`
			}
			url := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%s/hotspot?architecture=%s&image_type=jdk&os=linux&vendor=eclipse", major, a.java)
			if err := getJSON(url, &assets); err != nil {
				return nil, err
			}
			if len(assets) != 1 {
				return nil, fmt.Errorf("Temurin %s %s: want one release, got %d", major, a.docker, len(assets))
			}
			pkg := assets[0].Binary.Package
			if !isHex(pkg.Checksum, 64) || pkg.Link == "" {
				return nil, fmt.Errorf("Temurin %s %s: no checksum or link", major, a.docker)
			}
			out = append(out, entry{"java", strings.Replace(assets[0].Version.Semver, "+", "-", 1), "-", a.docker,
				"sha256:" + pkg.Checksum, pkg.Link})
		}
	}
	return out, nil
}

// --- .NET ---------------------------------------------------------------------

func dotnet() ([]entry, error) {
	var index struct {
		Releases []struct {
			Channel   string `json:"channel-version"`
			LatestSDK string `json:"latest-sdk"`
			JSON      string `json:"releases.json"`
		} `json:"releases-index"`
	}
	if err := getJSON("https://builds.dotnet.microsoft.com/dotnet/release-metadata/releases-index.json", &index); err != nil {
		return nil, err
	}
	var out []entry
	for _, channel := range dotnetChannels {
		var sdk, releasesURL string
		for _, r := range index.Releases {
			if r.Channel == channel {
				sdk, releasesURL = r.LatestSDK, r.JSON
			}
		}
		if sdk == "" {
			return nil, fmt.Errorf("no .NET %s channel", channel)
		}
		type file struct{ Name, URL, Hash string }
		var releases struct {
			Releases []struct {
				SDKs []struct {
					Version string `json:"version"`
					Files   []file `json:"files"`
				} `json:"sdks"`
			} `json:"releases"`
		}
		if err := getJSON(releasesURL, &releases); err != nil {
			return nil, err
		}
		var files []file
		for _, r := range releases.Releases {
			for _, s := range r.SDKs {
				if s.Version == sdk && files == nil {
					files = s.Files
				}
			}
		}
		if files == nil {
			return nil, fmt.Errorf(".NET %s: SDK %s is not in its releases.json", channel, sdk)
		}
		for _, a := range arches {
			name := "dotnet-sdk-linux-" + a.dotnet + ".tar.gz"
			var f *file
			for i := range files {
				if files[i].Name == name {
					f = &files[i]
				}
			}
			if f == nil || !isHex(f.Hash, 128) {
				return nil, fmt.Errorf(".NET SDK %s has no %s with a SHA-512", sdk, name)
			}
			out = append(out, entry{"dotnet", sdk, "-", a.docker, "sha512:" + strings.ToLower(f.Hash), f.URL})
		}
	}
	return out, nil
}

// --- Maven and Gradle ---------------------------------------------------------

func maven() ([]entry, error) {
	const base = "https://repo.maven.apache.org/maven2/org/apache/maven/apache-maven/"
	body, err := getText(base + "maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	var meta struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := xml.Unmarshal([]byte(body), &meta); err != nil {
		return nil, err
	}
	stable := regexp.MustCompile(`^3\.\d+\.\d+$`)
	var best string
	for _, v := range meta.Versions {
		if stable.MatchString(v) && (best == "" || versionLess(best, v)) {
			best = v
		}
	}
	if best == "" {
		return nil, errors.New("no Maven 3.x release")
	}
	url := fmt.Sprintf("%s%s/apache-maven-%s-bin.tar.gz", base, best, best)
	sum, err := getText(url + ".sha512")
	if err != nil {
		return nil, err
	}
	sum = strings.Fields(sum + " ")[0]
	if !isHex(sum, 128) {
		return nil, fmt.Errorf("Maven %s: %s.sha512 is not a SHA-512", best, url)
	}
	return []entry{{"maven", best, "-", "-", "sha512:" + sum, url}}, nil
}

func gradle() ([]entry, error) {
	var cur struct {
		Version     string `json:"version"`
		DownloadURL string `json:"downloadUrl"`
		ChecksumURL string `json:"checksumUrl"`
	}
	if err := getJSON("https://services.gradle.org/versions/current", &cur); err != nil {
		return nil, err
	}
	sum, err := getText(cur.ChecksumURL)
	if err != nil {
		return nil, err
	}
	sum = strings.TrimSpace(sum)
	if !isHex(sum, 64) {
		return nil, fmt.Errorf("Gradle %s: %s is not a SHA-256", cur.Version, cur.ChecksumURL)
	}
	return []entry{{"gradle", cur.Version, "-", "-", "sha256:" + sum, cur.DownloadURL}}, nil
}

// --- Rust ---------------------------------------------------------------------

// rust pins rustup-init by digest and the stable toolchain by version. The
// toolchain itself is rustup's to verify: it checks every component against
// the channel manifest it downloads, which is what rustup exists to do.
func rust() ([]entry, error) {
	rel, err := getText("https://static.rust-lang.org/rustup/release-stable.toml")
	if err != nil {
		return nil, err
	}
	rustup := tomlString(rel, "version")
	if rustup == "" {
		return nil, errors.New("release-stable.toml names no rustup version")
	}
	var out []entry
	for _, a := range arches {
		url := fmt.Sprintf("https://static.rust-lang.org/rustup/archive/%s/%s/rustup-init", rustup, a.rust)
		sum, err := getText(url + ".sha256")
		if err != nil {
			return nil, err
		}
		sum = strings.Fields(sum + " ")[0]
		if !isHex(sum, 64) {
			return nil, fmt.Errorf("rustup %s %s: no SHA-256", rustup, a.docker)
		}
		out = append(out, entry{"rustup", rustup, "-", a.docker, "sha256:" + sum, url})
	}
	channel, err := getText("https://static.rust-lang.org/dist/channel-rust-stable.toml")
	if err != nil {
		return nil, err
	}
	_, rest, ok := strings.Cut(channel, "\n[pkg.rust]\n")
	if !ok {
		return nil, errors.New("channel-rust-stable.toml has no [pkg.rust]")
	}
	// "1.90.0 (1159e78c4 2025-09-14)": the toolchain rustup installs is the
	// number, and the channel is where it gets the rest.
	version := strings.Fields(tomlString(rest, "version") + " ")[0]
	if version == "" {
		return nil, errors.New("channel-rust-stable.toml names no rust version")
	}
	out = append(out, entry{"rust", version, "-", "-", "-", "-"})
	return out, nil
}

// --- the lock file ------------------------------------------------------------

const header = `# The language toolchains the runner-full image unpacks into its tool cache.
#
# Generated by ` + "`go run deploy/gen_toolcache_lock.go`" + `; do not edit. The
# scheduled toolcache workflow reruns it and opens a pull request when a line
# moves. deploy/runner-toolcache.sh installs from these rows and nothing else,
# and refuses an archive whose digest is not the one written here.
#
# tool  version  platform  arch  digest  url
#
# platform is the Ubuntu release a Python build is for, and - for everything
# that runs on any of them; arch is Docker's TARGETARCH, and - for what is not
# architecture-specific. Python's digests are this generator's own, taken when
# the archive was first pinned: actions/python-versions publishes none.

`

func writeLock(es []entry) error {
	var b strings.Builder
	b.WriteString(header)
	for _, e := range es {
		b.WriteString(e.String())
		b.WriteByte('\n')
	}
	return os.WriteFile(lockPath, []byte(b.String()), 0o644)
}

// readLock returns the digests the current lock holds, by URL.
func readLock() map[string]string {
	known := map[string]string{}
	f, err := os.Open(lockPath)
	if err != nil {
		return known
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 6 && !strings.HasPrefix(fields[0], "#") && fields[4] != "-" {
			known[fields[5]] = fields[4]
		}
	}
	return known
}

// --- helpers ------------------------------------------------------------------

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "zoomies-gen-toolcache-lock")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func getText(url string) (string, error) {
	resp, err := get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func getJSON(url string, v any) error {
	resp, err := get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}

func hashURL(url string) (string, error) {
	fmt.Fprintf(os.Stderr, "  hashing %s\n", url)
	resp, err := get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// sumFor finds a file's digest in a SHASUMS-style listing.
func sumFor(sums, file string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == file && isHex(f[0], 64) {
			return f[0]
		}
	}
	return ""
}

// tomlString reads the first `key = "value"` line of a TOML document, in
// either of TOML's string quotes: rustup's release file uses single quotes and
// the channel manifest double.
func tomlString(doc, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	if m := re.FindStringSubmatch(doc); m != nil {
		return m[1] + m[2]
	}
	return ""
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// versionLess orders dotted numeric versions.
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}
