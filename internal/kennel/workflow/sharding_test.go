package workflow

import (
	"errors"
	"strings"
	"testing"
)

const shardWorkflow = `on: push
jobs:
  test:
    name: End to end
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - name: Browser tests
        run: npx playwright test --project=chromium
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: echo lint
`

func TestFindPlaywrightTestFindsTheOneShardableStep(t *testing.T) {
	job, step, err := FindPlaywrightTest([]byte(shardWorkflow))
	if err != nil || job != 0 || step != 1 {
		t.Fatalf("job=%d step=%d err=%v", job, step, err)
	}
	jobName, stepName, err := ShardNames([]byte(shardWorkflow), job, step)
	if err != nil || jobName != "End to end" || stepName != "Browser tests" {
		t.Fatalf("names %q %q %v", jobName, stepName, err)
	}
}

func TestShardChangeKeepsTheJobNameAndAddsOnlyTheShardFlag(t *testing.T) {
	before, after, line, err := ShardChange([]byte(shardWorkflow), 0, 1, 3)
	if err != nil || line != 3 {
		t.Fatalf("line=%d err=%v", line, err)
	}
	if strings.Count(shardWorkflow, before) != 1 || !strings.Contains(shardWorkflow, before+"  lint:") {
		t.Fatal("the edit was not one contiguous block that stops before the next job")
	}
	for _, want := range []string{
		"  test-shards:\n",
		"    name: \"End to end (shard ${{ matrix.shard }}/3)\"\n",
		"      fail-fast: false\n",
		"        shard: [1, 2, 3]\n",
		"        run: npx playwright test --project=chromium --shard=${{ matrix.shard }}/3\n",
		"  test:\n    name: End to end\n    needs: test-shards\n    if: ${{ always() }}\n    runs-on: ubuntu-latest\n",
		`run: test "${{ needs.test-shards.result }}" = "success"`,
	} {
		if !strings.Contains(after, want) {
			t.Errorf("after does not contain %q:\n%s", want, after)
		}
	}
	if strings.Count(after, "--shard=") != 1 {
		t.Error("the shard flag was added more than once")
	}
	// The rewrite must itself be a workflow Zoomies reads, and a second preview
	// of it must be refused, because it now shards.
	rewritten := strings.Replace(shardWorkflow, before, after, 1)
	if _, err := Inspect([]byte(rewritten)); err != nil {
		t.Fatalf("the rewritten workflow does not parse: %v", err)
	}
	var refusal *ShardRefusal
	if _, _, err := FindPlaywrightTest([]byte(rewritten)); !errors.As(err, &refusal) {
		t.Fatalf("a rewritten workflow can be sharded again: %v", err)
	}
}

func TestShardChangeCoversAJobWithNoNameAndAPreservesCRLF(t *testing.T) {
	workflow := "on: push\r\njobs:\r\n  e2e:\r\n    runs-on: ubuntu-24.04\r\n    steps:\r\n      - name: Run\r\n        run: |\r\n          npx playwright test\r\n"
	job, step, err := FindPlaywrightTest([]byte(workflow))
	if err != nil {
		t.Fatal(err)
	}
	_, after, _, err := ShardChange([]byte(workflow), job, step, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(after, "\r\n", ""), "\n") {
		t.Fatalf("mixed line endings: %q", after)
	}
	for _, want := range []string{"  e2e-shards:\r\n", "    name: \"e2e (shard ${{ matrix.shard }}/2)\"\r\n", "          npx playwright test --shard=${{ matrix.shard }}/2\r\n", "  e2e:\r\n    name: e2e\r\n"} {
		if !strings.Contains(after, want) {
			t.Errorf("after does not contain %q:\n%q", want, after)
		}
	}
}

func TestFindPlaywrightTestRefusesWhatShardingCouldBreak(t *testing.T) {
	job := func(extra, run string) string {
		return "on: push\njobs:\n  test:\n    name: Test\n    runs-on: ubuntu-latest\n" + extra + "    steps:\n      - name: Browser tests\n        run: " + run + "\n"
	}
	for name, tc := range map[string]struct{ workflow, reason string }{
		"already sharded":      {job("", "npx playwright test --shard=1/2"), "already shards"},
		"filtered":             {job("", "npx playwright test --grep @smoke"), "already shards, filters"},
		"a path":               {job("", "npx playwright test tests/a.spec.ts"), "does not recognise"},
		"piped":                {job("", "npx playwright test || true"), "does not recognise"},
		"existing matrix":      {job("    strategy:\n      matrix:\n        os: [a]\n", "npx playwright test"), "strategy"},
		"outputs":              {job("    outputs:\n      x: ${{ steps.a.outputs.x }}\n", "npx playwright test"), "outputs"},
		"services":             {job("    services:\n      db:\n        image: postgres\n", "npx playwright test"), "services"},
		"condition":            {job("    if: github.ref == 'refs/heads/main'\n", "npx playwright test"), "condition"},
		"continue on error":    {job("    continue-on-error: true\n", "npx playwright test"), "pass while failing"},
		"not ubuntu":           {strings.Replace(job("", "npx playwright test"), "ubuntu-latest", "macos-latest", 1), "Ubuntu"},
		"expression runner":    {strings.Replace(job("", "npx playwright test"), "ubuntu-latest", "ubuntu-${{ matrix.v }}", 1), "Ubuntu"},
		"colliding shard name": {job("", "npx playwright test") + "  test-shards:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n", "already exists"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := FindPlaywrightTest([]byte(tc.workflow))
			var refusal *ShardRefusal
			if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, tc.reason) {
				t.Fatalf("err=%v, want a refusal containing %q", err, tc.reason)
			}
		})
	}
}

func TestFindPlaywrightTestRefusesArtifactsCoverageAndUnnamedSteps(t *testing.T) {
	base := "on: push\njobs:\n  test:\n    name: Test\n    runs-on: ubuntu-latest\n    steps:\n      - name: Browser tests\n        run: npx playwright test\n"
	for name, workflow := range map[string]string{
		"artifact":  base + "      - uses: actions/upload-artifact@bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n        with:\n          name: report\n",
		"coverage":  base + "      - name: Upload coverage\n        run: echo hi\n",
		"unnamed":   strings.Replace(base, "      - name: Browser tests\n", "      - id: tests\n", 1),
		"test cond": strings.Replace(base, "        run: npx playwright test\n", "        if: always()\n        run: npx playwright test\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := FindPlaywrightTest([]byte(workflow))
			var refusal *ShardRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("err=%v, want a refusal", err)
			}
		})
	}
}

func TestFindPlaywrightTestSaysWhenThereIsNothingOrTwoThings(t *testing.T) {
	if _, _, err := FindPlaywrightTest([]byte("on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go test ./...\n")); !errors.Is(err, ErrShardNotFound) {
		t.Fatalf("no playwright step: %v", err)
	}
	two := shardWorkflow + "  more:\n    runs-on: ubuntu-latest\n    steps:\n      - name: Again\n        run: npx playwright test\n"
	if _, _, err := FindPlaywrightTest([]byte(two)); !errors.Is(err, ErrShardAmbiguous) {
		t.Fatalf("two playwright steps: %v", err)
	}
}

func TestShardChangeRefusesAnOutOfRangeCountAndAJobTooLargeToReview(t *testing.T) {
	for _, n := range []int{0, 1, 9} {
		if _, _, _, err := ShardChange([]byte(shardWorkflow), 0, 1, n); err == nil {
			t.Fatalf("count %d accepted", n)
		}
	}
	var b strings.Builder
	b.WriteString("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n")
	for i := 0; i < 40; i++ {
		b.WriteString("      - name: Step\n        run: echo hello\n")
	}
	b.WriteString("      - name: Browser tests\n        run: npx playwright test\n")
	_, _, _, err := ShardChange([]byte(b.String()), 0, 40, 2)
	var refusal *ShardRefusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "too large") {
		t.Fatalf("a large job: %v", err)
	}
}
