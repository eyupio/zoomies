package workflow

import (
	"strings"
	"testing"
)

const timeoutWorkflow = `on: push
jobs:
  build:
    name: Build
    runs-on: ubuntu-latest # the shared label
    steps:
      - run: make
  test:
    runs-on: ubuntu-latest
    steps:
      - run: make test
  lint:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - run: make lint
`

func TestTimeoutChangeAddsTheKeyAfterRunsOnAndNamesTheJob(t *testing.T) {
	data := []byte(timeoutWorkflow)
	before, after, line, err := TimeoutChange(data, 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if line != 6 {
		t.Errorf("line = %d, want the line the new key lands on", line)
	}
	if !strings.HasPrefix(before, "  build:\n") || !strings.HasSuffix(before, "# the shared label\n") {
		t.Errorf("before = %q, want the job's key through its runs-on line", before)
	}
	if after != before+"    timeout-minutes: 30\n" {
		t.Errorf("after = %q", after)
	}
	if strings.Count(timeoutWorkflow, before) != 1 {
		t.Error("the replaced block is not unique")
	}
	if name, err := TimeoutJobName(data, 0); err != nil || name != "Build" {
		t.Errorf("name = %q, %v", name, err)
	}
	if name, err := TimeoutJobName(data, 1); err != nil || name != "test" {
		t.Errorf("a job with no name is known by its key, got %q, %v", name, err)
	}
}

// Two jobs on the same label have the same runs-on line, so the change is named
// by the job's own key and the second job gets its own unique block.
func TestTimeoutChangeIsUniqueForAJobWhoseRunsOnLineIsRepeated(t *testing.T) {
	data := []byte(timeoutWorkflow)
	before, _, _, err := TimeoutChange(data, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(before, "  test:\n") || strings.Count(timeoutWorkflow, before) != 1 {
		t.Errorf("before = %q", before)
	}
}

func TestTimeoutChangeKeepsCRLFAndAFileWithoutATrailingNewline(t *testing.T) {
	crlf := "on: push\r\njobs:\r\n  build:\r\n    runs-on: ubuntu-latest\r\n    steps:\r\n      - run: make\r\n"
	before, after, _, err := TimeoutChange([]byte(crlf), 0, 10)
	if err != nil || after != before+"    timeout-minutes: 10\r\n" {
		t.Fatalf("crlf: after=%q err=%v", after, err)
	}
	// The runs-on line is the last line, so the file has no newline after it.
	tail := "on: push\njobs:\n  build:\n    steps:\n      - run: make\n    runs-on: ubuntu-latest"
	before, after, _, err = TimeoutChange([]byte(tail), 0, 10)
	if err != nil || after != before+"\n    timeout-minutes: 10\n" {
		t.Fatalf("tail: before=%q after=%q err=%v", before, after, err)
	}
}

func TestTimeoutChangeRefusesWhatItCouldNotChangeSafely(t *testing.T) {
	tests := []struct {
		name string
		data string
		job  int
		mins int
	}{
		{"a job that already has a timeout", timeoutWorkflow, 2, 10},
		{"minutes below one", timeoutWorkflow, 0, 0},
		{"minutes above the bound", timeoutWorkflow, 0, MaxTimeoutMinutes + 1},
		{"a job that does not exist", timeoutWorkflow, 9, 10},
		{"a runs-on list over several lines", "on: push\njobs:\n  build:\n    runs-on:\n      - ubuntu-latest\n    steps:\n      - run: make\n", 0, 10},
		{"a job written as a flow mapping", "on: push\njobs:\n  build: {runs-on: ubuntu-latest, steps: [{run: make}]}\n", 0, 10},
		{"a reusable workflow job", "on: push\njobs:\n  call:\n    uses: ./.github/workflows/x.yml\n", 0, 10},
		{"a file the parser refuses", "on: push\njobs: [\n", 0, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := TimeoutChange([]byte(tc.data), tc.job, tc.mins); err == nil {
				t.Fatal("the change was made")
			}
		})
	}
}

func TestTimeoutJobNameRefusesAnExpressionAndAnOversizedName(t *testing.T) {
	expr := "on: push\njobs:\n  build:\n    name: ${{ matrix.os }}\n    runs-on: x\n    steps:\n      - run: make\n"
	// The key is still a safe name, so an expression in `name` falls back to it.
	if name, err := TimeoutJobName([]byte(expr), 0); err != nil || name != "build" {
		t.Errorf("name = %q, %v", name, err)
	}
	long := "on: push\njobs:\n  " + strings.Repeat("a", 200) + ":\n    runs-on: x\n    steps:\n      - run: make\n"
	if _, err := TimeoutJobName([]byte(long), 0); err == nil {
		t.Error("an oversized key was accepted as a name")
	}
}
