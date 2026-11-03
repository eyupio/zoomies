package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/store"
)

// historyRow is one repair to seed. A person's rows are capped at ten a day by
// EnqueueEliRepair, so a test gives one person no more than eight and puts
// every row beyond that on an automatic repair, which has no owner to count.
type historyRow struct {
	owner   string // a user ID, or "" for a repair Eli started on its own
	repo    string
	trigger string
	state   string
}

// seedRepairs writes the rows through EnqueueEliRepair, the way the product
// does, and then moves each to its state. The return value is in the order
// given, so a test can name a row by position.
func (h *harness) seedRepairs(t *testing.T, inst *store.Installation, rows ...historyRow) []string {
	t.Helper()
	ids := make([]string, 0, len(rows))
	for i, row := range rows {
		r := &store.EliRepair{
			DedupKey:       fmt.Sprintf("test:%s:%d", t.Name(), i),
			InstallationID: inst.ID,
			Repo:           row.repo,
			PullNumber:     i + 1,
			UserID:         row.owner,
			Trigger:        row.trigger,
		}
		if added, err := h.st.EnqueueEliRepair(h.ctx, r); err != nil || !added {
			t.Fatalf("seeding repair %d: added=%v, %v", i, added, err)
		}
		r.State = row.state
		if err := h.st.SaveEliRepair(h.ctx, r); err != nil {
			t.Fatalf("moving repair %d to %q: %v", i, row.state, err)
		}
		ids = append(ids, r.ID)
	}
	return ids
}

type repairHistoryAnswer struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	Total int `json:"total"`
}

func (a repairHistoryAnswer) ids() []string {
	out := make([]string, 0, len(a.Items))
	for _, it := range a.Items {
		out = append(out, it.ID)
	}
	return out
}

func (h *harness) repairHistory(t *testing.T, cookie, query string) repairHistoryAnswer {
	t.Helper()
	resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs" + query, cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "reading the repair history"+query)
	var out repairHistoryAnswer
	resp.into(t, &out)
	return out
}

// fieldsOf is the fields a 422 names, so a test can say which input was refused
// and not only that something was.
func fieldsOf(t *testing.T, resp *response) []string {
	t.Helper()
	var env errorEnvelope
	resp.into(t, &env)
	var out []string
	for _, f := range env.Errors {
		out = append(out, f.Field)
	}
	if env.Error.Field != "" && !slices.Contains(out, env.Error.Field) {
		out = append(out, env.Error.Field)
	}
	return out
}

func TestEliRepairHistoryIsPagedFilteredAndCountedForTheCaller(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("history-admin", store.RoleAdmin)
	viewerUser, viewer := h.user("history-viewer", store.RoleViewer)
	other, _ := h.user("history-other", store.RoleViewer)

	// Six rows: two are the viewer's, two someone else's, and two are Eli's own.
	ids := h.seedRepairs(t, inst,
		historyRow{viewerUser.ID, "acme/site", "mention", "failed"},
		historyRow{viewerUser.ID, "acme/site", "mention", "succeeded"},
		historyRow{other.ID, "acme/site", "mention", "checks_failed"},
		historyRow{other.ID, "acme/widgets", "mention", "succeeded"},
		historyRow{"", "acme/widgets", "automatic", "failed"},
		historyRow{"", "acme/widgets", "automatic", "working"},
	)

	t.Run("a page says how many rows there are in all", func(t *testing.T) {
		got := h.repairHistory(t, admin, "?limit=2&offset=2")
		if len(got.Items) != 2 || got.Total != 6 {
			t.Fatalf("got %d items and a total of %d, want 2 and 6", len(got.Items), got.Total)
		}
	})

	t.Run("pages taken in turn cover every row once", func(t *testing.T) {
		var seen []string
		for _, offset := range []int{0, 2, 4} {
			seen = append(seen, h.repairHistory(t, admin, fmt.Sprintf("?limit=2&offset=%d", offset)).ids()...)
		}
		slices.Sort(seen)
		want := slices.Clone(ids)
		slices.Sort(want)
		if !slices.Equal(seen, want) {
			t.Fatalf("three pages of two did not cover the six rows once each\n got  %v\n want %v", seen, want)
		}
	})

	t.Run("with no paging asked for the first page still carries a total", func(t *testing.T) {
		got := h.repairHistory(t, admin, "")
		if len(got.Items) != 6 || got.Total != 6 {
			t.Fatalf("got %d items and a total of %d, want 6 and 6", len(got.Items), got.Total)
		}
	})

	for _, tc := range []struct {
		name, query string
		total       int
	}{
		{"repairs that need attention", "?group=attention", 3},
		{"repairs still running", "?group=active", 1},
		{"repairs that passed", "?group=passed", 2},
		{"repairs Eli started on its own", "?trigger=automatic", 2},
		{"repairs a person asked for", "?trigger=requested", 4},
		{"one repository, in any case", "?repo=WIDGETS", 3},
		{"a repository with spaces around it", "?repo=%20site%20", 3},
		{"a group and a trigger together", "?group=attention&trigger=automatic", 1},
	} {
		t.Run("an administrator narrowing to "+tc.name, func(t *testing.T) {
			got := h.repairHistory(t, admin, tc.query)
			if got.Total != tc.total || len(got.Items) != tc.total {
				t.Fatalf("%s: got %d items and a total of %d, want %d", tc.query, len(got.Items), got.Total, tc.total)
			}
		})
	}

	t.Run("a viewer is counted over their own rows only", func(t *testing.T) {
		if got := h.repairHistory(t, viewer, ""); got.Total != 2 || len(got.Items) != 2 {
			t.Fatalf("got %d items and a total of %d, want 2 and 2", len(got.Items), got.Total)
		}
		if got := h.repairHistory(t, viewer, "?group=attention"); got.Total != 1 {
			t.Errorf("the viewer's rows needing attention: total %d, want 1", got.Total)
		}
		// Automatic repairs belong to nobody, so no filter may hand them to a viewer.
		if got := h.repairHistory(t, viewer, "?trigger=automatic"); got.Total != 0 {
			t.Errorf("the viewer saw %d of Eli's own repairs", got.Total)
		}
	})

	t.Run("a filter that is not one is refused by name", func(t *testing.T) {
		for _, tc := range []struct{ query, field string }{
			{"?group=nonsense", "group"},
			{"?trigger=nonsense", "trigger"},
			{"?repo=" + strings.Repeat("a", 201), "repo"},
			// The contract's maxLength counts what was sent, not what is left once the
			// spaces are trimmed, so padding cannot slip a long value past it.
			{"?repo=" + strings.Repeat("%20", 201), "repo"},
			{"?repo=" + strings.Repeat("a", 190) + strings.Repeat("%20", 11), "repo"},
		} {
			resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs" + tc.query, cookie: admin})
			resp.mustStatus(t, http.StatusUnprocessableEntity, tc.field+" that is not valid")
			if got := fieldsOf(t, resp); !slices.Equal(got, []string{tc.field}) {
				t.Errorf("%s named %v, want just %s", tc.query, got, tc.field)
			}
		}
		// The longest repository the contract allows is still a filter, spaces or not.
		for _, query := range []string{
			"?repo=" + strings.Repeat("a", 200),
			"?repo=" + strings.Repeat("a", 190) + strings.Repeat("%20", 10),
			"?repo=" + strings.Repeat("%20", 200),
		} {
			h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs" + query, cookie: admin}).
				mustStatus(t, http.StatusOK, "a repository of exactly 200 characters")
		}
	})

	// This only proves the request is not refused: six rows cannot show a clamp.
	// TestEliRepairHistoryPageSizeIsClampedToTheMaximum seeds past the maximum.
	t.Run("an oversized limit and a negative offset are not refused", func(t *testing.T) {
		got := h.repairHistory(t, admin, "?limit=100000&offset=-4")
		if got.Total != 6 || len(got.Items) != 6 {
			t.Fatalf("got %d items and a total of %d, want 6 and 6", len(got.Items), got.Total)
		}
	})
}

// A clamp can only be seen with more rows than the maximum page holds. The
// request parser and the store each clamp, so this pins what a client gets
// rather than which of the two did it.
func TestEliRepairHistoryPageSizeIsClampedToTheMaximum(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("clamp-admin", store.RoleAdmin)

	// Automatic repairs have no owner, so the per-person daily cap does not apply;
	// the per-repository allowance does, hence several repositories.
	var rows []historyRow
	for i := range 11 {
		for range 50 {
			rows = append(rows, historyRow{"", fmt.Sprintf("acme/clamp-%d", i), "automatic", "succeeded"})
		}
	}
	h.seedRepairs(t, inst, rows...)

	for _, tc := range []struct {
		name, query string
		items       int
	}{
		{"no limit asked for", "", store.DefaultEliRepairPage},
		{"a limit past the maximum", "?limit=100000", store.MaxEliRepairPage},
		{"a limit within it", "?limit=7", 7},
	} {
		got := h.repairHistory(t, admin, tc.query)
		if len(got.Items) != tc.items || got.Total != len(rows) {
			t.Errorf("%s: got %d items and a total of %d, want %d and %d", tc.name, len(got.Items), got.Total, tc.items, len(rows))
		}
	}
}

func TestEliRepairSelectionIsTheCallersClearableMatches(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("selection-admin", store.RoleAdmin)
	viewerUser, viewer := h.user("selection-viewer", store.RoleViewer)
	other, _ := h.user("selection-other", store.RoleViewer)

	ids := h.seedRepairs(t, inst,
		historyRow{viewerUser.ID, "acme/site", "mention", "failed"},
		historyRow{viewerUser.ID, "acme/site", "mention", "succeeded"},
		historyRow{viewerUser.ID, "acme/site", "mention", "working"},
		historyRow{other.ID, "acme/site", "mention", "failed"},
	)

	type selection struct {
		IDs       []string `json:"ids"`
		Total     int      `json:"total"`
		Truncated bool     `json:"truncated"`
	}
	read := func(cookie, query string) selection {
		t.Helper()
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/selection" + query, cookie: cookie})
		resp.mustStatus(t, http.StatusOK, "reading a selection"+query)
		var out selection
		resp.into(t, &out)
		return out
	}

	got := read(viewer, "")
	if want := []string{ids[0], ids[1]}; len(got.IDs) != 2 || !slices.Contains(got.IDs, want[0]) || !slices.Contains(got.IDs, want[1]) {
		t.Errorf("the viewer's selection is %v, want their two finished rows %v and nothing running or anyone else's", got.IDs, want)
	}
	if got.Total != 2 || got.Truncated {
		t.Errorf("total %d, truncated %v; want 2 and false: a running row is not something a select-all may take", got.Total, got.Truncated)
	}

	// The filters narrow a selection as they narrow the list it is taken from.
	if got := read(viewer, "?group=attention"); !slices.Equal(got.IDs, []string{ids[0]}) || got.Total != 1 {
		t.Errorf("the viewer's rows needing attention: %v, total %d", got.IDs, got.Total)
	}
	// An administrator reaches everyone's rows, but still not a running one.
	if got := read(admin, ""); got.Total != 3 || slices.Contains(got.IDs, ids[2]) {
		t.Errorf("the administrator's selection is %v, total %d; want the three finished rows", got.IDs, got.Total)
	}

	for _, tc := range []struct{ query, field string }{
		{"?group=nonsense", "group"},
		{"?trigger=nonsense", "trigger"},
		{"?repo=" + strings.Repeat("a", 201), "repo"},
		{"?repo=" + strings.Repeat("%20", 201), "repo"},
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/selection" + tc.query, cookie: viewer})
		resp.mustStatus(t, http.StatusUnprocessableEntity, "a selection with a bad "+tc.field)
		if got := fieldsOf(t, resp); !slices.Equal(got, []string{tc.field}) {
			t.Errorf("%s named %v, want just %s", tc.query, got, tc.field)
		}
	}
}

// A select-all is a frozen list of at most 500 ids, and the page has to be told
// when the list is shorter than the matches or it would say "all" and clear a
// fraction.
func TestEliRepairSelectionSaysWhenItWasCapped(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("capped-admin", store.RoleAdmin)

	// Automatic repairs have no owner, so the per-person daily cap does not apply;
	// the per-repository allowance does, hence several repositories.
	var rows []historyRow
	for i := range 11 {
		for range 50 {
			rows = append(rows, historyRow{"", fmt.Sprintf("acme/repo-%d", i), "automatic", "succeeded"})
		}
	}
	h.seedRepairs(t, inst, rows...)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/selection", cookie: admin})
	resp.mustStatus(t, http.StatusOK, "selecting more rows than a selection holds")
	var got struct {
		IDs       []string `json:"ids"`
		Total     int      `json:"total"`
		Truncated bool     `json:"truncated"`
	}
	resp.into(t, &got)
	if len(got.IDs) != store.MaxEliRepairSelection || got.Total != 550 || !got.Truncated {
		t.Fatalf("got %d ids, a total of %d, truncated %v; want %d, 550 and true",
			len(got.IDs), got.Total, got.Truncated, store.MaxEliRepairSelection)
	}
}

func TestEliRepairsAreClearedInBulkAndReportedOneByOne(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, admin := h.user("bulk-admin", store.RoleAdmin)
	viewerUser, viewer := h.user("bulk-viewer", store.RoleViewer)
	other, _ := h.user("bulk-other", store.RoleViewer)

	ids := h.seedRepairs(t, inst,
		historyRow{viewerUser.ID, "acme/site", "mention", "failed"},
		historyRow{viewerUser.ID, "acme/site", "mention", "working"},
		historyRow{other.ID, "acme/site", "mention", "failed"},
	)
	own, activeOwn, someoneElses := ids[0], ids[1], ids[2]

	bulk := func(cookie string, body any) *response {
		t.Helper()
		return h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/bulk", cookie: cookie, body: body})
	}

	resp := bulk(viewer, map[string]any{"action": "clear", "ids": []string{own, own, activeOwn, someoneElses, "missing"}})
	resp.mustStatus(t, http.StatusOK, "clearing a mixed batch")
	var got struct {
		Results []store.EliRepairClear `json:"results"`
	}
	resp.into(t, &got)
	want := []store.EliRepairClear{
		{ID: own, OK: true},
		{ID: activeOwn, Error: "still running"},
		{ID: someoneElses, Error: "not found"},
		{ID: "missing", Error: "not found"},
	}
	if !slices.Equal(got.Results, want) {
		t.Fatalf("results, in the order asked and with the repeated id answered once\n got  %+v\n want %+v", got.Results, want)
	}

	if list := h.repairHistory(t, viewer, ""); slices.Contains(list.ids(), own) || !slices.Contains(list.ids(), activeOwn) {
		t.Errorf("after the clear the viewer's history is %v: the cleared row should be gone and the running one still there", list.ids())
	}
	if list := h.repairHistory(t, admin, ""); !slices.Contains(list.ids(), someoneElses) || list.Total != 2 {
		t.Errorf("the administrator's history is %v (total %d): someone else's row must be untouched", list.ids(), list.Total)
	}

	audit, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_clear"}}, store.Page{Limit: 5})
	if err != nil || len(audit) != 1 {
		t.Fatalf("audit rows = %v, %v; want exactly one for the request", audit, err)
	}
	if a := audit[0]; a.ActorID != viewerUser.ID || a.TargetKind != "eli_repair" ||
		!strings.Contains(a.After, `"count":1`) || !strings.Contains(a.After, own) ||
		strings.Contains(a.After, activeOwn) || strings.Contains(a.After, someoneElses) {
		t.Errorf("the audit row must name who cleared how many and which, and only those that were cleared: %+v", a)
	}

	// A request that cleared nothing changed nothing, so it leaves no row to read.
	bulk(viewer, map[string]any{"action": "clear", "ids": []string{activeOwn, "missing"}}).
		mustStatus(t, http.StatusOK, "a batch where nothing can be cleared")
	if audit, _, _ := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_clear"}}, store.Page{Limit: 5}); len(audit) != 1 {
		t.Errorf("a clear that cleared nothing wrote an audit row: %d rows", len(audit))
	}

	// Clearing a row twice is harmless: it is already out of the history.
	again := bulk(viewer, map[string]any{"action": "clear", "ids": []string{own}})
	again.mustStatus(t, http.StatusOK, "clearing a row that is already cleared")
	var repeat struct {
		Results []store.EliRepairClear `json:"results"`
	}
	again.into(t, &repeat)
	if !slices.Equal(repeat.Results, []store.EliRepairClear{{ID: own, OK: true}}) {
		t.Errorf("clearing twice answered %+v", repeat.Results)
	}
	// It is harmless, and it must not look like work: the person changed nothing,
	// so the audit log has no second row saying they cleared this repair.
	if audit, _, _ := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_clear"}}, store.Page{Limit: 5}); len(audit) != 1 {
		t.Errorf("clearing a cleared repair wrote an audit row: %d rows, want the one from the first clear", len(audit))
	}

	tooMany := make([]string, 501)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("erf_%d", i)
	}
	for _, tc := range []struct {
		name  string
		body  any
		field string
	}{
		{"an action that is not clear", map[string]any{"action": "delete"}, "action"},
		{"an action that is not clear, with ids", map[string]any{"action": "delete", "ids": []string{own}}, "action"},
		{"no action", map[string]any{"ids": []string{own}}, "action"},
		{"no ids", map[string]any{"action": "clear", "ids": []string{}}, "ids"},
		{"501 ids", map[string]any{"action": "clear", "ids": tooMany}, "ids"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := bulk(viewer, tc.body)
			resp.mustStatus(t, http.StatusUnprocessableEntity, tc.name)
			if got := fieldsOf(t, resp); !slices.Contains(got, tc.field) {
				t.Errorf("the refusal names %v, want it to name %s", got, tc.field)
			}
		})
	}
	// The largest batch the contract allows is carried out, not refused.
	maxed := make([]string, 500)
	for i := range maxed {
		maxed[i] = fmt.Sprintf("erf_%d", i)
	}
	bulk(viewer, map[string]any{"action": "clear", "ids": maxed}).mustStatus(t, http.StatusOK, "exactly 500 ids")
}

// clearRepairs posts a bulk clear as whoever holds the cookie or the token and
// returns the per-id answers.
func (h *harness) clearRepairs(t *testing.T, cookie, token string, ids ...string) []store.EliRepairClear {
	t.Helper()
	resp := h.do(request{method: http.MethodPost, path: "/api/v1/assistant/repairs/bulk", cookie: cookie, token: token,
		body: map[string]any{"action": "clear", "ids": ids}})
	resp.mustStatus(t, http.StatusOK, "clearing repairs")
	var out struct {
		Results []store.EliRepairClear `json:"results"`
	}
	resp.into(t, &out)
	return out.Results
}

// An administrator is the one caller who can tidy the whole history, and the
// audit log is where anyone later asks who did it: the row has to name the
// administrator, not the person whose repair it was.
func TestAnAdministratorsBulkClearReachesAnotherPersonsRowAndTheAuditNamesTheAdministrator(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	admin, adminCookie := h.user("reach-admin", store.RoleAdmin)
	owner, ownerCookie := h.user("reach-owner", store.RoleViewer)

	ids := h.seedRepairs(t, inst, historyRow{owner.ID, "acme/site", "mention", "failed"})

	got := h.clearRepairs(t, adminCookie, "", ids...)
	if want := []store.EliRepairClear{{ID: ids[0], OK: true}}; !slices.Equal(got, want) {
		t.Fatalf("an administrator clearing someone else's repair: got %+v, want %+v", got, want)
	}
	if list := h.repairHistory(t, ownerCookie, ""); len(list.Items) != 0 {
		t.Errorf("the owner still sees %v after an administrator cleared it", list.ids())
	}

	audit, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_clear"}}, store.Page{Limit: 5})
	if err != nil || len(audit) != 1 {
		t.Fatalf("audit rows = %v, %v; want exactly one", audit, err)
	}
	if a := audit[0]; a.ActorID != admin.ID || a.ActorID == owner.ID || !strings.Contains(a.After, ids[0]) {
		t.Errorf("the audit row must name the administrator who cleared it, not the owner: %+v", a)
	}
}

// The audit log is read later to answer who hid what. An administrator who
// "clears" a repair the owner already cleared hid nothing, and a row that says
// they did would put their name on someone else's action.
func TestTheAuditNamesOnlyTheRepairsARequestActuallyCleared(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	admin, adminCookie := h.user("changed-admin", store.RoleAdmin)
	owner, ownerCookie := h.user("changed-owner", store.RoleViewer)

	ids := h.seedRepairs(t, inst,
		historyRow{owner.ID, "acme/site", "mention", "failed"},
		historyRow{owner.ID, "acme/site", "mention", "succeeded"},
	)
	clearedByOwner, stillShown := ids[0], ids[1]
	h.clearRepairs(t, ownerCookie, "", clearedByOwner)

	audits := func() []*store.AuditEvent {
		t.Helper()
		rows, _, err := h.st.ListAudit(h.ctx, store.AuditFilter{Actions: []string{"assistant.repair_clear"}}, store.Page{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if len(audits()) != 1 {
		t.Fatalf("the owner's own clear should have left one audit row, got %d", len(audits()))
	}

	// Only the already-cleared row: ok for the administrator, and nothing to record.
	got := h.clearRepairs(t, adminCookie, "", clearedByOwner)
	if !slices.Equal(got, []store.EliRepairClear{{ID: clearedByOwner, OK: true}}) {
		t.Fatalf("an administrator clearing an already cleared repair got %+v, want ok", got)
	}
	if len(audits()) != 1 {
		t.Fatalf("an administrator clearing a repair that was already cleared wrote an audit row naming them: %d rows", len(audits()))
	}

	// Both, in one request: the answer is ok twice, the audit row names one repair.
	got = h.clearRepairs(t, adminCookie, "", clearedByOwner, stillShown)
	if !slices.Equal(got, []store.EliRepairClear{{ID: clearedByOwner, OK: true}, {ID: stillShown, OK: true}}) {
		t.Fatalf("a request naming a cleared and an uncleared repair got %+v, want ok for both", got)
	}
	rows := audits()
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want the owner's and the administrator's", len(rows))
	}
	for _, a := range rows {
		if a.ActorID != admin.ID {
			continue
		}
		if !strings.Contains(a.After, `"count":1`) || !strings.Contains(a.After, stillShown) || strings.Contains(a.After, clearedByOwner) {
			t.Errorf("the administrator's audit row must count and name only the repair they cleared: %+v", a)
		}
		return
	}
	t.Errorf("no audit row names the administrator, who cleared %s: %+v", stillShown, rows)
}

// Whether a repair is running is only worth saying to someone who may see it.
// The answer for someone else's row has to be the same "not found" whatever
// state the row is in, or the state would be a way to probe other people's work.
func TestAnotherPersonsRunningRepairIsNotFoundToANonAdministratorAndStillRunningToAnAdministrator(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	_, adminCookie := h.user("probe-admin", store.RoleAdmin)
	_, viewerCookie := h.user("probe-viewer", store.RoleViewer)
	other, _ := h.user("probe-other", store.RoleViewer)

	ids := h.seedRepairs(t, inst, historyRow{other.ID, "acme/site", "mention", "working"})

	if got := h.clearRepairs(t, viewerCookie, "", ids...); !slices.Equal(got, []store.EliRepairClear{{ID: ids[0], Error: "not found"}}) {
		t.Errorf("a viewer asking about someone else's running repair got %+v, want not found", got)
	}
	// The contrast is the point: the same row, asked of by someone who may see it.
	if got := h.clearRepairs(t, adminCookie, "", ids...); !slices.Equal(got, []store.EliRepairClear{{ID: ids[0], Error: "still running"}}) {
		t.Errorf("an administrator asking about the same row got %+v, want still running", got)
	}
}

// A token narrowed to assistant:own is "only the owner-scoped personal
// endpoints", whatever role it was minted at. Any scope on a resource implies
// reading it, so such a token can read every person's repairs; clearing is a
// write and must not ride on that.
//
// The token belongs to the administrator because a token never acts above its
// owner's role: one minted at admin for a viewer is a viewer, and would pass
// this test without the gate it is here to pin.
func TestATokenNarrowedToAssistantOwnClearsOnlyItsOwnPersonsRepairs(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	admin, adminCookie := h.user("narrow-admin", store.RoleAdmin)
	other, _ := h.user("narrow-other", store.RoleViewer)

	ids := h.seedRepairs(t, inst,
		historyRow{admin.ID, "acme/site", "mention", "failed"},
		historyRow{other.ID, "acme/site", "mention", "failed"},
	)
	own, someoneElses := ids[0], ids[1]

	mint := func(name string, scopes ...string) string {
		t.Helper()
		_, plaintext, err := h.ctrl.Auth().CreateAPIToken(h.ctx, auth.NewToken{Name: name, Role: store.RoleAdmin, UserID: admin.ID, Scopes: scopes})
		if err != nil {
			t.Fatalf("minting %s: %v", name, err)
		}
		return plaintext
	}

	narrow := mint("narrowed-to-own", "assistant:own")
	got := h.clearRepairs(t, "", narrow, someoneElses, own)
	want := []store.EliRepairClear{{ID: someoneElses, Error: "not found"}, {ID: own, OK: true}}
	if !slices.Equal(got, want) {
		t.Fatalf("an administrator's token narrowed to assistant:own\n got  %+v\n want %+v", got, want)
	}
	if list := h.repairHistory(t, adminCookie, ""); !slices.Equal(list.ids(), []string{someoneElses}) {
		t.Errorf("the history is %v; the other person's repair should be untouched and the token's own person's gone", list.ids())
	}

	// The control: the same role and owner with the scope that does reach
	// everyone's rows, so the refusal above is the scope and not the route.
	wide := mint("wide-assistant", "assistant:*")
	if got := h.clearRepairs(t, "", wide, someoneElses); !slices.Equal(got, []store.EliRepairClear{{ID: someoneElses, OK: true}}) {
		t.Errorf("a token scoped to assistant:* clearing someone else's repair got %+v, want ok", got)
	}
}

// A repair has a page of its own, addressed by its id, and an id is not a
// secret: it turns up in links, logs and pasted screenshots. So a repair that
// is not the caller's is answered word for word as one that does not exist,
// and the only person who can tell the difference is an administrator.
func TestGetEliRepairAnswersOwnersAdministratorsAndNobodyElse(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	owner, ownerCookie := h.user("repair-owner", store.RoleViewer)
	_, otherCookie := h.user("repair-other", store.RoleViewer)
	_, adminCookie := h.user("repair-admin", store.RoleAdmin)
	ids := h.seedRepairs(t, inst, historyRow{owner: owner.ID, repo: inst.Target + "/repo", trigger: "mention", state: "failed"})
	path := "/api/v1/assistant/repairs/" + ids[0]
	row, err := h.st.GetEliRepair(h.ctx, ids[0], owner.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	row.Details.Steps = []store.EliRepairStep{{ResponseRecorded: true, Response: `{"read":["private-source.go"]}`, ValidationError: "do not combine actions"}}
	if err := h.st.SaveEliRepair(h.ctx, row); err != nil {
		t.Fatal(err)
	}

	own := h.do(request{method: http.MethodGet, path: path, cookie: ownerCookie})
	own.mustStatus(t, http.StatusOK, "the owner reads their repair")
	var got struct {
		ID string `json:"id"`
	}
	own.into(t, &got)
	if got.ID != ids[0] {
		t.Errorf("answered repair %q, want %q", got.ID, ids[0])
	}
	assertShape(t, loadSpec(t), "EliRepair", own.body)
	if own.header.Get("Cache-Control") != "no-store" {
		t.Fatal("source-bearing repair details can be cached")
	}
	if !strings.Contains(string(own.body), "private-source.go") || !strings.Contains(string(own.body), "do not combine actions") {
		t.Fatal("the detail read lost the retained response or validation error")
	}
	history := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs", cookie: ownerCookie})
	history.mustStatus(t, http.StatusOK, "reading history without source-bearing responses")
	if strings.Contains(string(history.body), "private-source.go") {
		t.Fatal("a history poll included model response source")
	}

	h.do(request{method: http.MethodGet, path: path, cookie: adminCookie}).mustStatus(t, http.StatusOK, "an administrator reads anyone's repair")

	someoneElses := h.do(request{method: http.MethodGet, path: path, cookie: otherCookie})
	someoneElses.mustStatus(t, http.StatusNotFound, "another person's repair")
	missing := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/eli_missing", cookie: otherCookie})
	missing.mustStatus(t, http.StatusNotFound, "a repair that does not exist")
	if a, b := someoneElses.errorMessage(t), missing.errorMessage(t); a == "" || a != b || someoneElses.errorCode(t) != missing.errorCode(t) {
		t.Errorf("the two refusals differ, so one tells a person that an id exists: %q (%s) and %q (%s)", a, someoneElses.errorCode(t), b, missing.errorCode(t))
	}
}

// A repair cleared from the history is gone from the list, not from the page
// that follows it: a link someone kept still opens, so they can see what it was.
func TestGetEliRepairStillOpensARepairClearedFromTheHistory(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	owner, cookie := h.user("repair-owner", store.RoleViewer)
	ids := h.seedRepairs(t, inst, historyRow{owner: owner.ID, repo: inst.Target + "/repo", trigger: "mention", state: "failed"})
	if res, err := h.st.ClearEliRepairs(h.ctx, ids, owner.ID, true); err != nil || len(res) != 1 {
		t.Fatalf("clearing the repair: %v %v", res, err)
	}
	h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/" + ids[0], cookie: cookie}).
		mustStatus(t, http.StatusOK, "a cleared repair opened by its link")
}

// The error output of a job is its log, so it is asked for by a query that says
// which log. The mistakes are named by field, so a caller knows what to fix.
func TestErrorOutputNeedsAJobIDAndARepo(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("repair-viewer", store.RoleViewer)
	for name, tc := range map[string]struct {
		query  string
		fields []string
	}{
		"nothing at all":       {"", []string{"repo", "job_id"}},
		"no job":               {"?repo=acme/repo", []string{"job_id"}},
		"no repository":        {"?job_id=5", []string{"repo"}},
		"a job that is zero":   {"?repo=acme/repo&job_id=0", []string{"job_id"}},
		"a job below zero":     {"?repo=acme/repo&job_id=-3", []string{"job_id"}},
		"a job not a number":   {"?repo=acme/repo&job_id=abc", []string{"job_id"}},
		"a bare owner":         {"?repo=acme&job_id=5", []string{"repo"}},
		"an empty name":        {"?repo=acme/&job_id=5", []string{"repo"}},
		"too many parts":       {"?repo=acme/repo/extra&job_id=5", []string{"repo"}},
		"a very long repo":     {"?repo=" + strings.Repeat("a", 300) + "/repo&job_id=5", []string{"repo"}},
		"both wrong together":  {"?repo=acme&job_id=0", []string{"repo", "job_id"}},
		"whitespace is not it": {"?repo=%20&job_id=5", []string{"repo"}},
	} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/error-output" + tc.query, cookie: cookie})
		resp.mustStatus(t, http.StatusUnprocessableEntity, name)
		if got := fieldsOf(t, resp); !slices.Equal(got, tc.fields) {
			t.Errorf("%s: the refusal names %v, want %v", name, got, tc.fields)
		}
	}
}

// Whatever a job printed is shown to people who could fix the pull request. A
// caller whose GitHub account is not linked, or cannot write to the repository,
// is told what to do, and the log is not read for them.
func TestErrorOutputIsRefusedWithoutWriteAccess(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	repo := inst.Target + "/repo"
	linked, linkedCookie := h.user("repair-linked", store.RoleViewer)
	_, strangerCookie := h.user("repair-stranger", store.RoleViewer)
	if err := h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: linked.ID, GitHubUserID: 42, GitHubLogin: "octo"}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.ConfirmEliIdentity(h.ctx, linked.ID, 42, true); err != nil {
		t.Fatal(err)
	}
	h.gh.SetRepairActor("octo", 42, false)

	for name, cookie := range map[string]string{"not linked": strangerCookie, "linked but read-only": linkedCookie} {
		resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/error-output?repo=" + repo + "&job_id=5", cookie: cookie})
		resp.mustStatus(t, http.StatusConflict, name)
		if msg := resp.errorMessage(t); !strings.Contains(msg, "write access") || !strings.Contains(msg, "GitHub") {
			t.Errorf("%s: the refusal does not say what is needed: %q", name, msg)
		}
	}
}

// With write access the log is read through the installation. GitHub having no
// log for the job is an answer, not a failure: the page gets an empty list, and
// a sentence to show, in the shape the contract promises.
func TestErrorOutputAnswersAnEmptyListWhenGitHubHasNoLog(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	repo := inst.Target + "/repo"
	person, cookie := h.user("repair-writer", store.RoleViewer)
	if err := h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: person.ID, GitHubUserID: 42, GitHubLogin: "octo"}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.ConfirmEliIdentity(h.ctx, person.ID, 42, true); err != nil {
		t.Fatal(err)
	}
	h.gh.SetRepairActor("octo", 42, true)

	resp := h.do(request{method: http.MethodGet, path: "/api/v1/assistant/repairs/error-output?repo=" + repo + "&job_id=5", cookie: cookie})
	resp.mustStatus(t, http.StatusOK, "reading a job the fake GitHub has no log for")
	assertShape(t, loadSpec(t), "EliErrorOutput", resp.body)
	var got struct {
		Lines *[]string `json:"lines"`
		Note  string    `json:"note"`
	}
	resp.into(t, &got)
	if got.Lines == nil || len(*got.Lines) != 0 || got.Note == "" {
		t.Errorf("answered %s, want an empty lines array and a note", resp.body)
	}
}
