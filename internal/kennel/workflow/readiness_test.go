package workflow

import (
	"errors"
	"strings"
	"testing"
)

const readinessWorkflow = `on: push
jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - name: Wait for the API
        run: sleep 30
      - name: Test
        run: echo ok
`

func TestFindFixedSleepFindsTheOneBoundedWait(t *testing.T) {
	job, step, seconds, err := FindFixedSleep([]byte(readinessWorkflow))
	if err != nil || job != 0 || step != 0 || seconds != 30 {
		t.Fatalf("job=%d step=%d seconds=%d err=%v", job, step, seconds, err)
	}
	name, stepName, err := ReadinessNames([]byte(readinessWorkflow), job, step)
	if err != nil || name != "Build" || stepName != "Wait for the API" {
		t.Fatalf("names %q %q %v", name, stepName, err)
	}
	before, after, line, err := ReadinessChange([]byte(readinessWorkflow), job, step, "timeout 30 sh -c 'until true; do sleep 1; done'")
	if err != nil || line != 8 || !strings.Contains(before, "run: sleep 30") || !strings.Contains(after, "run: |\n          timeout 30 sh -c 'until true; do sleep 1; done'\n") || strings.Contains(after, "Test") {
		t.Fatalf("before=%q after=%q line=%d err=%v", before, after, line, err)
	}
	if strings.Count(readinessWorkflow, before) != 1 || !strings.Contains(readinessWorkflow, before+"      - name: Test") {
		t.Fatal("the edit was not one contiguous block")
	}
}

func TestFindFixedSleepAcceptsOneLiteralBlockAndPreservesCRLF(t *testing.T) {
	workflow := "on: push\r\njobs:\r\n  build:\r\n    name: Build\r\n    runs-on: ubuntu-24.04\r\n    steps:\r\n      - name: Wait\r\n        run: |\r\n          sleep 12\r\n      - name: Test\r\n        run: echo ok\r\n"
	job, step, seconds, err := FindFixedSleep([]byte(workflow))
	if err != nil || seconds != 12 {
		t.Fatalf("job=%d step=%d seconds=%d err=%v", job, step, seconds, err)
	}
	_, after, _, err := ReadinessChange([]byte(workflow), job, step, "timeout 12 sh -c 'until true; do sleep 1; done'")
	if err != nil || !strings.Contains(after, "run: |\r\n          timeout") || strings.Contains(strings.ReplaceAll(after, "\r\n", ""), "\n") {
		t.Fatalf("after=%q err=%v", after, err)
	}
}

func TestFindFixedSleepRefusesAmbiguityAndUnsupportedShells(t *testing.T) {
	for name, workflow := range map[string]string{
		"two waits": `on: push
jobs:
  a:
    name: A
    runs-on: ubuntu-latest
    steps:
      - name: Wait
        run: sleep 30
  b:
    name: B
    runs-on: ubuntu-24.04
    steps:
      - name: Wait
        run: sleep 10
`,
		"windows": `on: push
jobs:
  a:
    name: A
    runs-on: windows-latest
    steps:
      - name: Wait
        run: sleep 30
`,
		"other command": `on: push
jobs:
  a:
    name: A
    runs-on: ubuntu-latest
    steps:
      - name: Wait
        run: |
          sleep 30
          echo ready
`,
		"comment": `on: push
jobs:
  a:
    name: A
    runs-on: ubuntu-latest
    steps:
      - name: Wait
        run: |
          # wait for the API
          sleep 30
`,
		"suffix": `on: push
jobs:
  a:
    name: A
    runs-on: ubuntu-latest
    steps:
      - name: Wait
        run: sleep 30s
`,
		"too short": `on: push
jobs:
  a:
    name: A
    runs-on: ubuntu-latest
    steps:
      - name: Wait
        run: sleep 1
`,
		"expression": `on: push
jobs:
  a:
    name: A
    runs-on: ${{ matrix.os }}
    steps:
      - name: Wait
        run: sleep 30
`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := FindFixedSleep([]byte(workflow)); err == nil {
				t.Fatal("accepted")
			} else if name == "two waits" && !errors.Is(err, ErrFixedSleepAmbiguous) {
				t.Fatalf("ambiguous error %v", err)
			} else if name != "two waits" && !errors.Is(err, ErrFixedSleepNotFound) && !errors.Is(err, errUnsupported) {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestReadinessChangeRefusesANonLiteralScript(t *testing.T) {
	for _, script := range []string{"", "echo one\necho two", "echo\x00", strings.Repeat("x", 4097)} {
		if _, _, _, err := ReadinessChange([]byte(readinessWorkflow), 0, 0, script); err == nil {
			t.Fatalf("accepted %q", script)
		}
	}
	if _, _, _, err := ReadinessChange([]byte(readinessWorkflow), 1, 0, "true"); err == nil {
		t.Fatal("accepted the wrong position")
	}
}
