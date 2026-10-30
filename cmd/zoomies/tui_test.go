package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeTUIAPI records what the dashboard asked for and answers from a table.
// The model never knows whether it is talking to a controller or to this, which
// is the point: the screens and the confirmations can be tested without one.
type fakeTUIAPI struct {
	calls   []tuiCall
	replies map[string]string
}

type tuiCall struct {
	path string
	q    url.Values
	body any
}

func (f *fakeTUIAPI) get(_ context.Context, path string, q url.Values, out any) ([]byte, error) {
	f.calls = append(f.calls, tuiCall{path: path, q: q})
	raw := []byte(f.replies[path])
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, err
		}
	}
	return raw, nil
}

func (f *fakeTUIAPI) post(_ context.Context, path string, q url.Values, body, out any) ([]byte, error) {
	f.calls = append(f.calls, tuiCall{path: path, q: q, body: body})
	return f.reply(path, out)
}

func (f *fakeTUIAPI) patch(_ context.Context, path string, q url.Values, body, out any) ([]byte, error) {
	f.calls = append(f.calls, tuiCall{path: path, q: q, body: body})
	return f.reply(path, out)
}

func (f *fakeTUIAPI) del(_ context.Context, path string, q url.Values, out any) ([]byte, error) {
	f.calls = append(f.calls, tuiCall{path: path, q: q})
	return f.reply(path, out)
}

func (f *fakeTUIAPI) reply(path string, out any) ([]byte, error) {
	raw := []byte(f.replies[path])
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, err
		}
	}
	return raw, nil
}

func newTestTUIModel(api tuiAPI) tuiModel {
	m := newTUIModel(context.Background(), api, "https://zoomies.example.test", "1h", nil)
	m.colour = false
	m.width, m.height = 120, 30
	m.loading = false
	m.loaded = true
	m.updated = time.Unix(1_700_000_000, 0)
	m.now = time.Unix(1_700_000_030, 0)
	return m
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(t *testing.T, m tuiModel, key string) (tuiModel, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(keyMsg(key))
	next, ok := updated.(tuiModel)
	if !ok {
		t.Fatalf("Update returned %T, want tuiModel", updated)
	}
	return next, cmd
}

func TestTUIKeepsTheCursorInsideItsTabAndSwitchesWithTab(t *testing.T) {
	m := newTestTUIModel(&fakeTUIAPI{})
	m.data.runners = []runnerItem{{ID: "run_1", Name: "one"}, {ID: "run_2", Name: "two"}}
	m.active = tabRunners

	m, _ = press(t, m, "j")
	if got := m.cursor[tabRunners]; got != 1 {
		t.Fatalf("cursor after one j = %d, want 1", got)
	}
	m, _ = press(t, m, "j")
	if got := m.cursor[tabRunners]; got != 1 {
		t.Fatalf("cursor at the end = %d, want it left at 1", got)
	}
	m, _ = press(t, m, "tab")
	if m.active != tabHosts {
		t.Fatalf("tab moved to %v, want Hosts", m.active)
	}
	m, _ = press(t, m, "2")
	if m.active != tabProblems {
		t.Fatalf("number key moved to %v, want Problems", m.active)
	}
}

func TestTUIListShrinkingUnderTheCursorMovesItBack(t *testing.T) {
	m := newTestTUIModel(&fakeTUIAPI{})
	m.active = tabJobs
	m.cursor[tabJobs] = 4

	updated, _ := m.Update(tuiDataMsg{data: tuiData{jobs: []jobItem{{ID: "job_1"}}}, at: m.now})
	m = updated.(tuiModel)

	if got := m.cursor[tabJobs]; got != 0 {
		t.Fatalf("cursor = %d after the list shrank to one row, want 0", got)
	}
}

func TestTUIAsksBeforeApplyingARemedy(t *testing.T) {
	api := &fakeTUIAPI{replies: map[string]string{
		"/problems/apply": `{"applied":true,"remedy":{"label":"Raise the host's capacity"}}`,
	}}
	m := newTestTUIModel(api)
	m.active = tabProblems
	m.data.problems.Items = []problemItem{{
		Code:     "host.slots_below_capacity",
		Severity: "warning",
		Title:    "A host has fewer slots than it could",
		TargetID: "host_1",
		Remedy: &problemRemedy{
			ID:     "rem_1",
			Label:  "Raise the host's capacity",
			Effect: "Two more slots, at 1 CPU and 2 GiB each.",
		},
	}}

	m, cmd := press(t, m, "enter")
	if cmd != nil {
		t.Fatal("asking to apply a remedy did something before it was confirmed")
	}
	if m.confirm == nil {
		t.Fatal("no confirmation was asked")
	}
	if len(api.calls) != 0 {
		t.Fatalf("the API was touched before confirmation: %+v", api.calls)
	}

	m, cmd = press(t, m, "y")
	if m.confirm != nil {
		t.Fatal("the confirmation survived the answer")
	}
	if cmd == nil {
		t.Fatal("confirming produced no command")
	}
	msg, ok := cmd().(tuiActionResultMsg)
	if !ok {
		t.Fatalf("action returned %T, want tuiActionResultMsg", msg)
	}
	if msg.err != nil {
		t.Fatalf("action failed: %v", msg.err)
	}
	if len(api.calls) != 1 || api.calls[0].path != "/problems/apply" {
		t.Fatalf("calls = %+v, want one POST /problems/apply", api.calls)
	}
	body, ok := api.calls[0].body.(map[string]any)
	if !ok {
		t.Fatalf("body = %T, want a map", api.calls[0].body)
	}
	for key, want := range map[string]string{
		"code":      "host.slots_below_capacity",
		"target_id": "host_1",
		"remedy_id": "rem_1",
	} {
		if got := body[key]; got != want {
			t.Errorf("body[%q] = %v, want %q", key, got, want)
		}
	}
}

func TestTUIDismissesAProblemKindForTheAccount(t *testing.T) {
	api := &fakeTUIAPI{}
	m := newTestTUIModel(api)
	m.active = tabProblems
	m.data.problems.Items = []problemItem{{
		Code:     "pool.daemon_share_suggested",
		Severity: "warning",
		Title:    "The Docker sidecar takes too little of a slot",
	}}

	m, _ = press(t, m, "d")
	if m.confirm == nil {
		t.Fatal("no confirmation was asked")
	}
	m, cmd := press(t, m, "y")
	if cmd == nil {
		t.Fatal("confirming produced no command")
	}
	if msg := cmd().(tuiActionResultMsg); msg.err != nil {
		t.Fatalf("dismiss failed: %v", msg.err)
	}
	if len(api.calls) != 1 || api.calls[0].path != "/auth/problem-dismissals" {
		t.Fatalf("calls = %+v, want one PATCH /auth/problem-dismissals", api.calls)
	}
	body := api.calls[0].body.(map[string]any)
	set := body["set"].([]map[string]any)
	if len(set) != 1 || set[0]["key"] != "type:pool.daemon_share_suggested" || set[0]["severity"] != "warning" {
		t.Fatalf("dismissal body = %+v, want the kind key and its severity", body)
	}
}

func TestTUIDrainingARunnerCarriesTheConfirmationTheCLIWouldAskFor(t *testing.T) {
	api := &fakeTUIAPI{}
	m := newTestTUIModel(api)
	m.active = tabRunners
	m.data.runners = []runnerItem{{ID: "run_1", Name: "zoomies-1"}}

	m, _ = press(t, m, "enter")
	m, cmd := press(t, m, "y")
	if cmd == nil {
		t.Fatal("confirming produced no command")
	}
	if msg := cmd().(tuiActionResultMsg); msg.err != nil {
		t.Fatalf("drain failed: %v", msg.err)
	}
	if len(api.calls) != 1 {
		t.Fatalf("calls = %+v, want one", api.calls)
	}
	call := api.calls[0]
	if call.path != "/runners/run_1/drain" {
		t.Fatalf("path = %q, want /runners/run_1/drain", call.path)
	}
	if got := call.q.Get("confirm"); got != "true" {
		t.Fatalf("confirm = %q, want true after the operator accepted it", got)
	}
}

func TestTUIViewSaysWhenNothingNeedsAttention(t *testing.T) {
	m := newTestTUIModel(&fakeTUIAPI{})
	m.data.problems = problemsResponse{OK: true}
	m.data.stats.Window = "1h"

	view := m.render()
	if !strings.Contains(view, "Nothing needs your attention.") {
		t.Fatalf("the quiet case is not unmistakable:\n%s", view)
	}
	if !strings.Contains(view, "Overview") || !strings.Contains(view, "Jobs") {
		t.Fatalf("the tab bar is missing:\n%s", view)
	}
}

func TestTUIWithoutATerminalPointsAtStatus(t *testing.T) {
	e := &env{out: &bytes.Buffer{}, err: &bytes.Buffer{}, in: strings.NewReader("")}
	err := runTUI(context.Background(), e, nil)
	if err == nil {
		t.Fatal("runTUI accepted a non-terminal")
	}
	if !strings.Contains(err.Error(), "zoomies status") {
		t.Fatalf("the error does not name the command to use instead: %v", err)
	}
}

func TestTUIOnlyReloadsForEventsThatCanChangeATab(t *testing.T) {
	for kind, want := range map[string]bool{
		"job.updated":      true,
		"runner.updated":   true,
		"problems.updated": true,
		"scaling":          true,
		"audit":            false,
		"webhook.delivery": false,
		"kennel.summary":   false,
		"provider.updated": false,
	} {
		if got := tuiKindMatters(kind); got != want {
			t.Errorf("tuiKindMatters(%q) = %v, want %v", kind, got, want)
		}
	}
}
