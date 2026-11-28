package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/releasekey"
)

// The installers and the binary must trust the same release key: an installer
// holds a first download to it, the binary holds an upgrade to it, and a release
// signed by a key only one of them carries is refused by the other.
func TestTheInstallerCarriesTheReleaseKeyTheBinaryDoes(t *testing.T) {
	for script, line := range map[string]string{
		"install.sh":  `(?m)^RELEASE_KEY="([^"]*)"$`,
		"install.ps1": `(?m)^\$script:ReleaseKey = '([^']*)'\r?$`,
	} {
		body, err := os.ReadFile(filepath.Join("..", "..", script))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(line).FindStringSubmatch(string(body))
		if m == nil {
			t.Errorf("%s has no release key line", script)
			continue
		}
		got := strings.TrimSpace(m[1])
		if got != strings.TrimSpace(releasekey.Default) {
			t.Errorf("%s carries release key %q and the binary carries %q; set both to the same key, or neither", script, got, releasekey.Default)
		}
		if got != "" {
			if _, err := releasekey.ParsePublic(got); err != nil {
				t.Errorf("%s carries a release key that is not an Ed25519 public key: %v", script, err)
			}
		}
	}
}
