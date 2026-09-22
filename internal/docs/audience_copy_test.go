package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The UI speaks to two audiences now, and one of them cannot read the
// controller's log.
//
// On an instance one team operates while another uses the fleet, "the
// controller log will say why" is an instruction the reader cannot follow: the
// log belongs to the process, and the person reading it runs pools on that
// process. supportHint() in web/src/lib/errors.ts says the thing that is true
// for both -- quote the request ID to whoever operates this instance -- and
// this test is what stops a tenth sentence growing back beside it.
//
// The allowlist is short and each entry is a place where the phrase is
// correct rather than tolerated.
var controllerLogAllowed = map[string]string{
	// The helper's own explanation of what it replaced.
	"lib/errors.ts": "the doc comment on supportHint says what the phrase used to be",
	// First-run setup: the token is printed to the log and whoever can read
	// the log is, by definition, the person operating the process. There is no
	// second audience yet -- no account exists.
	"routes/Bootstrap.svelte": "the setup token is read from the log by the person who started the process",
}

func TestTheUIDoesNotSendTheFleetToAControllerLogItCannotRead(t *testing.T) {
	root := "../../web/src"
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(path, ".svelte") || strings.HasSuffix(path, ".ts")) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, ok := controllerLogAllowed[rel]; ok {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "controller log") {
				offenders = append(offenders, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(offenders) > 0 {
		t.Errorf("these send the reader to a log the fleet may not be able to read; use supportHint() from $lib/errors, or add a line to controllerLogAllowed saying why the phrase is right here:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// Every allowlist entry names a file that exists, so a file that moves takes
// its exemption with it rather than leaving one behind for the next phrase
// that lands there.
func TestTheControllerLogAllowlistNamesFilesThatExist(t *testing.T) {
	for rel, why := range controllerLogAllowed {
		if why == "" {
			t.Errorf("%s is allowlisted with no reason given", rel)
		}
		if _, err := os.Stat(filepath.Join("../../web/src", rel)); err != nil {
			t.Errorf("%s is allowlisted but is not there any more: %v", rel, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
