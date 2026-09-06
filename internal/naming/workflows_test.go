package naming

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The image catalogue in this package says which operating systems a pool may
// ask for; the workflows say which ones are actually built and pushed. A row
// in one and not the other is an operating system that is half-added: either a
// pool can name an image that does not exist, or an image is published that
// nothing can use. Neither failure shows up until a runner refuses to start,
// so it is checked here instead.
func TestWorkflowMatricesCoverTheCatalogue(t *testing.T) {
	for _, wf := range []struct{ file, job string }{
		{"ci.yml", "runner-images"},
		{"release.yml", "runner-images"},
	} {
		t.Run(wf.file, func(t *testing.T) {
			got := runnerMatrix(t, wf.file, wf.job)
			want := map[string]matrixEntry{}
			for _, img := range Images() {
				want[img.Tag()] = matrixEntry{
					Base: img.Base, Family: img.Family, OS: img.OS, Version: img.Version,
				}
			}
			for tag, w := range want {
				g, ok := got[tag]
				if !ok {
					t.Errorf("%s builds no %s image, but the catalogue publishes one", wf.file, tag)
					continue
				}
				if g != w {
					t.Errorf("%s builds %s as %+v, but the catalogue says %+v", wf.file, tag, g, w)
				}
			}
			for tag := range got {
				if _, ok := want[tag]; !ok {
					t.Errorf("%s builds %s, which is not in the catalogue, so no pool can ask for it", wf.file, tag)
				}
			}
		})
	}
}

// matrixEntry is the part of a matrix row that has to agree with the catalogue.
type matrixEntry struct {
	Base    string `yaml:"base"`
	Family  string `yaml:"family"`
	OS      string `yaml:"os"`
	Version string `yaml:"version"`
}

func runnerMatrix(t *testing.T, file, job string) map[string]matrixEntry {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []struct {
						Tag         string `yaml:"tag"`
						matrixEntry `yaml:",inline"`
					} `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", file))
	if err != nil {
		t.Fatalf("reading the workflow: %v", err)
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	j, ok := wf.Jobs[job]
	if !ok {
		t.Fatalf("%s has no %s job", file, job)
	}
	out := map[string]matrixEntry{}
	for _, row := range j.Strategy.Matrix.Include {
		out[row.Tag] = row.matrixEntry
	}
	if len(out) == 0 {
		t.Fatalf("%s's %s job builds nothing", file, job)
	}
	return out
}

// The Makefile builds the same variants by hand, because a contributor
// debugging a Fedora runner wants `make image-runner RUNNER_VARIANT=fedora-42`
// and not a workflow run. It drifts from the catalogue just as easily.
func TestMakefileBuildsTheCatalogue(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	// The rows are column-aligned, so compare on fields rather than on the
	// exact spacing a contributor is free to adjust.
	rows := map[string]string{}
	var listed []string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 2 && fields[0] == "RUNNER_VARIANTS" && fields[1] == ":=":
			listed = fields[2:]
		case len(fields) >= 2 && strings.HasPrefix(fields[0], "variant.") && fields[1] == ":=":
			rows[strings.TrimPrefix(fields[0], "variant.")] = strings.Join(fields[2:], " ")
		}
	}
	if len(listed) == 0 {
		t.Fatal("the Makefile no longer lists RUNNER_VARIANTS")
	}
	for _, img := range Images() {
		want := strings.Join([]string{img.Base, img.Family, img.OS, img.Version}, " ")
		got, ok := rows[img.Tag()]
		if !ok {
			t.Errorf("the Makefile has no row building %s; expected:\n\tvariant.%s := %s", img.Tag(), img.Tag(), want)
			continue
		}
		if got != want {
			t.Errorf("the Makefile builds %s as %q, but the catalogue says %q", img.Tag(), got, want)
		}
		if !slices.Contains(listed, img.Tag()) {
			t.Errorf("%s has a row but is not in RUNNER_VARIANTS, so `make images-runner` skips it", img.Tag())
		}
	}
	for _, tag := range listed {
		if _, ok := FindImage(splitTag(tag)); !ok {
			t.Errorf("the Makefile builds %q, which is not in the catalogue", tag)
		}
	}
}

// splitTag turns an image tag back into the OS and version FindImage takes.
func splitTag(tag string) (string, string) {
	os, version, _ := strings.Cut(tag, "-")
	return os, ExpandVersion(os, version)
}
