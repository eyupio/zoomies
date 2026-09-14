package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/test/e2e/proxmox"
)

// The range this command reads must be the range the harness reads.
//
// parseRange is deliberately duplicated -- this command has to keep working
// when the harness's build tag does not apply -- and a duplicated parser is
// only safe while both sides are held to the same cases. A range read one way
// here and another way there would have this command declare a cluster clean
// because it was looking at the wrong hundred identifiers.
func TestARangeIsReadTheSameWayTheHarnessReadsIt(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		want     proxmox.Range
		wantFail bool
	}{
		{raw: "9000-9099", want: proxmox.Range{Min: 9000, Max: 9099}},
		{raw: "  9000 - 9099  ", want: proxmox.Range{Min: 9000, Max: 9099}},
		{raw: "9000-9000", want: proxmox.Range{Min: 9000, Max: 9000}},
		{raw: "", wantFail: true},
		{raw: "9000", wantFail: true},
		{raw: "9000-", wantFail: true},
		{raw: "-9099", wantFail: true},
		{raw: "nine-thousand", wantFail: true},
		{raw: "9099-9000", wantFail: true},
		{raw: "0-9099", wantFail: true},
		{raw: "-1-9099", wantFail: true},
	} {
		got, err := parseRange(tc.raw)
		switch {
		case tc.wantFail && err == nil:
			t.Errorf("parseRange(%q) = %v, want a refusal", tc.raw, got)
		case !tc.wantFail && err != nil:
			t.Errorf("parseRange(%q): %v", tc.raw, err)
		case !tc.wantFail && got != tc.want:
			t.Errorf("parseRange(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// A refusal names every missing setting at once, and dials nothing.
//
// Somebody running this is usually running it in a hurry, after a harness died
// and before they know what is on their cluster. Being told about one missing
// flag per attempt is three round trips they do not have time for, and a
// refusal that had already opened a connection would be a refusal that could
// hang instead.
func TestRunSaysEverythingThatIsMissingBeforeItDialsAnything(t *testing.T) {
	var out bytes.Buffer
	err := run(options{ledgers: t.TempDir()}, &out)
	if err == nil {
		t.Fatal("run with nothing configured returned no error")
	}
	for _, flag := range []string{"-url", "-token", "-node", "-storage", "-range"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("the refusal does not name %s: %v", flag, err)
		}
	}
	if !strings.Contains(err.Error(), "ZOOMIES_PROXMOX_") {
		t.Errorf("the refusal does not say the settings also come from the environment: %v", err)
	}
	if out.Len() > 0 {
		t.Errorf("a refusal wrote a report: %q", out.String())
	}
}

// A range that cannot be read is refused before the cluster is dialled, for the
// same reason: the connection is the slow part, and the range is the setting
// that decides what the command is even looking at.
func TestARangeThatCannotBeReadIsRefusedBeforeTheCluster(t *testing.T) {
	var out bytes.Buffer
	err := run(options{
		endpoint: "https://pve.invalid:8006", token: "u@pve!t=s",
		node: "pve1", storage: "local-lvm", vmids: "nine thousand",
		ledgers: t.TempDir(),
	}, &out)
	if err == nil {
		t.Fatal("an unreadable range was accepted")
	}
	if !strings.Contains(err.Error(), "9000-9099") {
		t.Errorf("the refusal does not show the shape it wants: %v", err)
	}
}

// The report leads with the counts and then shows only what needs a person.
//
// An accounted resource printed alongside an unexplained one is how a hundred
// clean lines hide the one that matters, and this report is read at the end of
// a run that took hours.
func TestTheReportShowsTheCountsAndOnlyTheFindingsThatNeedSomebody(t *testing.T) {
	var out bytes.Buffer
	report(proxmox.Reconciliation{
		TakenAt:     time.Date(2026, 9, 14, 3, 4, 5, 0, time.UTC),
		Range:       proxmox.Range{Min: 9000, Max: 9099},
		Storage:     "local-lvm",
		VMsAtStart:  1,
		VMsAtEnd:    2,
		Created:     20,
		Confirmed:   19,
		DisksLeft:   1,
		Unexplained: 1,
		Findings: []proxmox.Finding{
			{Verdict: proxmox.Accounted, VMID: 9001, Detail: "created and confirmed deleted"},
			{
				Verdict: proxmox.LeftBehind, VMID: 9042, Node: "pve1",
				Name: "zoomies-qualify-9042", RunID: "run_abc", Detail: "its ledger names it and it is still here",
			},
		},
	}, &out)

	got := out.String()
	// The counts are matched without their padding: the alignment is a
	// formatting choice, and pinning it here would make widening a label a
	// test failure rather than a formatting change.
	for _, want := range []string{
		"range 9000-9099 on local-lvm",
		"2026-09-14T03:04:05Z",
		"VMs in the range at the end",
		"unexplained owned resources",
		"LEFT_BEHIND: VM 9042 on pve1 (zoomies-qualify-9042), run run_abc",
		"its ledger names it and it is still here",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "9001") {
		t.Errorf("the report printed an accounted resource:\n%s", got)
	}
	if strings.Contains(got, "every owned resource in the range is accounted for") {
		t.Errorf("a report with an unexplained resource said the cluster was clean:\n%s", got)
	}
}

// And says so plainly when there is nothing to report, because "no output" and
// "nothing found" have to be told apart by somebody deciding whether a
// qualification passed.
func TestACleanReportSaysSoRatherThanSayingNothing(t *testing.T) {
	var out bytes.Buffer
	report(proxmox.Reconciliation{
		Range: proxmox.Range{Min: 9000, Max: 9099}, Storage: "local-lvm",
		Created: 20, Confirmed: 20,
		Findings: []proxmox.Finding{{Verdict: proxmox.Accounted, VMID: 9001, Detail: "gone"}},
	}, &out)
	if !strings.Contains(out.String(), "every owned resource in the range is accounted for") {
		t.Errorf("a clean reconciliation did not say so:\n%s", out.String())
	}
}
