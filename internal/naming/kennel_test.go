package naming

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The wizard names pools from its own copy of the kennel, in TypeScript,
// because the name has to appear in a form field before anything is created.
// Two lists that drift apart give a fleet pools called one thing and runners
// called another, which reads as two products rather than one -- so the lists
// are compared here rather than trusted to stay equal.
func TestKennelMatchesTheWizard(t *testing.T) {
	path := filepath.Join("..", "..", "web", "src", "lib", "pools", "names.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the wizard's word list: %v", err)
	}
	block := regexp.MustCompile(`(?s)KENNEL: readonly string\[\] = \[(.*?)\];`).FindSubmatch(src)
	if block == nil {
		t.Fatalf("%s no longer declares KENNEL the way this test reads it", path)
	}
	var wizard []string
	for _, m := range regexp.MustCompile(`'([a-z]+)'`).FindAllStringSubmatch(string(block[1]), -1) {
		wizard = append(wizard, m[1])
	}
	if !slices.Equal(wizard, Kennel) {
		t.Errorf("the wizard's kennel and this package's have drifted apart:\n  wizard: %v\n  Go:     %v", wizard, Kennel)
	}
}

func TestKennelWordsFitInAName(t *testing.T) {
	seen := map[string]bool{}
	for _, word := range Kennel {
		// A word that needed sanitising would arrive in a name as something
		// other than what this list says, and a duplicate would quietly halve
		// an already small pool of words.
		if Slug(word) != word {
			t.Errorf("kennel word %q is not already a name segment; Slug makes it %q", word, Slug(word))
		}
		if seen[word] {
			t.Errorf("kennel word %q appears twice", word)
		}
		seen[word] = true
		// The budget: the brand, the longest shape a fleet can really have, the
		// word, and the token, all inside GitHub's limit.
		spec := Spec{CPUs: 192, MemoryGB: 768, OS: OSUbuntu, Version: "24.04", Arch: ArchARM64}
		name := RunnerName(spec.String(), word+"-a3f9qz2m")
		if !strings.Contains(name, word) || len(name) > MaxNameLength {
			t.Errorf("kennel word %q does not fit: %q is %d characters", word, name, len(name))
		}
	}
}

func TestKennelWordComesFromTheKennel(t *testing.T) {
	for range 100 {
		if word := KennelWord(); !slices.Contains(Kennel, word) {
			t.Fatalf("KennelWord returned %q, which is not in the kennel", word)
		}
	}
}
