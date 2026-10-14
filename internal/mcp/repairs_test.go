package mcp

import (
	"errors"
	"strings"
	"testing"
)

func repairAPI(body string) *kennelAPI { return &kennelAPI{body: body} }

// The two tools read and only read: the writes the feature has, the request,
// the accept, the clear and the policy, are not on the surface at all, and an
// argument no schema names is refused rather than dropped.
func TestEliRepairsToolsReadAndNothingElse(t *testing.T) {
	for _, name := range []string{"list_elis_repairs", "get_eli_repair"} {
		tl := kennelTool(t, name)
		if tl.action {
			t.Errorf("%s acts, but a history read changes nothing", name)
		}
		if tl.live {
			t.Errorf("%s is marked live, but a page of history is an answer, not a window", name)
		}
		if _, err := kennelCall(t, tl, repairAPI(`{}`), `{"clear":true}`); err == nil || !strings.Contains(err.Error(), "input schema") {
			t.Errorf("%s: clear=true: %v, want a schema refusal", name, err)
		}
	}
}

// A page of history opens with its counts, then rows. The route's own numbers
// and groups are what the model is told, so the answer and the page agree.
func TestListEliRepairsCountsThePageAndListsTheRows(t *testing.T) {
	body := `{"items":[` +
		`{"id":"rep_1","repo":"eyupio/zoomies","pull_number":101,"state":"failed","trigger":"automatic","updated_at":"2026-10-14T07:07:00Z","message":"The failed job has no usable error evidence."},` +
		`{"id":"rep_2","repo":"eyupio/zoomies","pull_number":102,"state":"checking","trigger":"mention","github_login":"octocat","updated_at":"2026-10-14T08:03:00Z","message":"Pushed commit abc"}],"total":57}`
	api := repairAPI(body)
	out, err := kennelCall(t, kennelTool(t, "list_elis_repairs"), api, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if api.got != "GET /assistant/repairs" {
		t.Errorf("made %q", api.got)
	}
	if api.query.Encode() != "limit=30" {
		t.Errorf("query = %s, want the route's page with only the defaults named", api.query.Encode())
	}
	got := anyBlock(out)
	for _, want := range []string{"2 repairs listed, of 57 that match", "1 failed", "1 checking"} {
		if !strings.Contains(got, want) {
			t.Errorf("answer missed %q in %s;", want, got)
		}
	}
	// The trusted line carries the state and what it means, not a name a
	// stranger wrote.
	for _, trusted := range []string{"rep_1: state=failed trigger=automatic", "rep_2: state=checking"} {
		if !strings.Contains(got, trusted) {
			t.Errorf("row line missed %q in %s;", trusted, got)
		}
	}
	if i := strings.Index(got, "eyupio/zoomies"); i >= 0 && i < strings.Index(got, "untrusted data") {
		t.Errorf("the repository reached the model outside the quoted block: %s", got)
	}
	// The quoted block carries the names and messages, one notice before it.
	if !strings.Contains(got, repairNotice) ||
		!strings.Contains(got, `rep_2: repo="eyupio/zoomies" pull=102 requested_by="octocat" message="Pushed commit abc"`) {
		t.Errorf("quoted block = %s", got)
	}
}

// A page that did not reach the whole history says how much still matches,
// and where to read on.
func TestListEliRepairsSaysHowToReadOn(t *testing.T) {
	api := repairAPI(`{"items":[{"id":"rep_1","repo":"acme/zoomies","pull_number":1,"state":"checking","trigger":"automatic","updated_at":"2026-10-14T07:00:00Z"}],"total":57}`)
	out, err := kennelCall(t, kennelTool(t, "list_elis_repairs"), api, `{"limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if api.query.Encode() != "limit=1" {
		t.Errorf("query = %s", api.query.Encode())
	}
	if got := anyBlock(out); !strings.Contains(got, "56 more match") || !strings.Contains(got, "before=1") {
		t.Errorf("a page of 1 with 57 matching did not say what shows more: %s", got)
	}
}

// An answer to a filter that matched nothing says which filters were in
// force, rather than implying the whole history is empty.
func TestListEliRepairsSaysWhenTheFiltersHidEverything(t *testing.T) {
	api := repairAPI(`{"items":[],"total":0}`)
	out, err := kennelCall(t, kennelTool(t, "list_elis_repairs"), api, `{"group":"attention","repo":"eyupio","trigger":"automatic","before":100}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "group=attention&limit=30&offset=100&repo=eyupio&trigger=automatic"; !stringsContainsOrder(api.query.Encode(), want) {
		t.Errorf("query = %s, want %s", api.query.Encode(), want)
	}
	if got := anyBlock(out); !strings.Contains(got, "attention") || !strings.Contains(got, "automatic") {
		t.Errorf("an empty, filtered answer said nothing about the filters: %s", got)
	}
}

// A filter the route refuses comes back as the route's own sentence, not
// paraphrased, so the model can read what to change.
func TestListEliRepairsCarriesARouteRefusalThrough(t *testing.T) {
	api := &kennelAPI{err: errors.New(`unknown group "meh": use attention, active or passed`)}
	if _, err := kennelCall(t, kennelTool(t, "list_elis_repairs"), api, `{"group":"meh"}`); err == nil || !strings.Contains(err.Error(), "attention") {
		t.Errorf("refusal = %v, want the controller's own sentence", err)
	}
	// An answer of the wrong shape is refused rather than shown, so a future
	// field cannot be read as this tool's.
	if _, err := kennelCall(t, kennelTool(t, "list_elis_repairs"), repairAPI(`{}`), `{}`); err != nil {
		t.Errorf("an answer with no total: %v, want the shape refusal", err)
	}
}

// get_eli_repair searches by repository and pull number and answers with the
// newest row it holds, tying the row's names to its id.
func TestGetEliRepairShowsOnePullRequestAndItsWords(t *testing.T) {
	api := repairAPI(`{"items":[{"id":"rep_9","repo":"eyupio/zoomies","pull_number":7,"state":"checking","trigger":"mention","github_login":"octo","updated_at":"2026-10-14T07:07:00Z","message":"Pushed commit abc"}],"total":3}`)
	out, err := kennelCall(t, kennelTool(t, "get_eli_repair"), api, `{"repo":"eyupio/zoomies","pull_number":7}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := api.query.Encode(); got != "limit=2&pull_number=7&repo=eyupio%2Fzoomies" {
		t.Errorf("query = %s", got)
	}
	answer := anyBlock(out)
	for _, want := range []string{"rep_9: state=checking", "pushed and waiting", `repo="eyupio/zoomies" pull=7 requested_by="octo" message="Pushed commit abc"`} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer missed %q in %s;", want, answer)
		}
	}
	if answer != "" && !strings.Contains(answer, repairNotice) {
		t.Errorf("the quoted block reached the model without its notice: %s", answer)
	}
}

// No repair of that pull request, and a page whose newest row belongs to
// another pull request, are the same answer: none, not a substitute.
func TestGetEliRepairSaysWhenThereIsNone(t *testing.T) {
	for _, body := range []string{
		`{"items":[],"total":0}`,
		`{"items":[{"id":"rep_8","repo":"eyupio/zoomies","pull_number":5,"state":"failed","trigger":"automatic","updated_at":"2026-10-14T07:03:00Z"}],"total":1}`,
	} {
		out, err := kennelCall(t, kennelTool(t, "get_eli_repair"), repairAPI(body), `{"repo":"eyupio/zoomies","pull_number":7}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := anyBlock(out); !strings.Contains(got, "No Eli repair of eyupio/zoomies pull request 7") {
			t.Errorf("answer = %s", got)
		}
	}
	// A call that names neither half of the search is refused before the route
	// is asked: the answer must be about a pull request it can name.
	api := repairAPI(`{}`)
	if _, err := kennelCall(t, kennelTool(t, "get_eli_repair"), api, `{"repo":"","pull_number":0}`); err == nil || api.calls != 0 {
		t.Errorf("a nameless search: %v, %d calls", err, api.calls)
	}
}

// The answer for a passed row tells the model that asking again now is the
// one thing that cannot change anything; a failed one goes back to a person,
// who can ask again from the page.
func TestGetEliRepairTellsTheModelWhatAskingAgainDoes(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  string
	}{
		{"succeeded", "Asking again now answers that the repair is already accepted"},
		{"checks_failed", "Another attempt is a person's"},
		{"failed", "from the repairs page"},
		{"superseded", "no longer describe the pull request's head"},
	} {
		body := `{"items":[{"id":"rep_1","repo":"eyupio/zoomies","pull_number":9,"state":"` + tc.state + `","trigger":"mention","github_login":"octo","updated_at":"2026-10-14T07:07:00Z"}],"total":1}`
		out, err := kennelCall(t, kennelTool(t, "get_eli_repair"), repairAPI(body), `{"repo":"eyupio/zoomies","pull_number":9}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := anyBlock(out); !strings.Contains(got, tc.want) {
			t.Errorf("%s: answer missed %q in %s;", tc.state, tc.want, got)
		}
	}
}

// A row of a shape the tool cannot read, or any other pull request's row,
// stops the whole answer: refusing is how the quoted block stays a boundary
// and the newest row stays the answer.
func TestARowShapeTheToolCannotReadIsRefused(t *testing.T) {
	for _, body := range []string{
		`{"items":[{"id":"rep_1","repo":"eyupio/zoomies","pull_number":true,"state":"failed","trigger":"automatic"}],"total":1}`,
		`{"items":[{"id":"rep_1","repo":"eyupio/zoomies","pull_number":0,"state":"failed","trigger":"automatic"}],"total":1}`,
		`{"items":[{"id":"rep_1","repo":"","pull_number":3,"state":"failed","trigger":"automatic"}],"total":1}`,
		`{"items":[{"id":"","repo":"eyupio/zoomies","pull_number":3,"state":"failed","trigger":"automatic"}],"total":1}`,
		`{"items":[{"id":"rep_1","repo":"eyupio/zoomies","pull_number":3,"state":"","trigger":"automatic"}],"total":1}`,
		`{"items":[{"id":"rep_1","repo":"eyupio/zoomies","pull_number":3,"state":"failed","trigger":"mention"}],"total":1}`,
	} {
		if _, err := kennelCall(t, kennelTool(t, "list_elis_repairs"), repairAPI(body), `{}`); err == nil {
			t.Errorf("a mangled row was rendered: %s", body)
		}
	}
}

// anyBlock is everything a tool answered, as one string: rows and the
// controller's own sentences together, for a test that looks for content.
func anyBlock(content []Content) string {
	var b strings.Builder
	for _, c := range content {
		b.WriteString(c.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// stringsContainsOrder says every piece named, in the order given, is in s.
func stringsContainsOrder(s, pieces string) bool {
	parts := strings.Split(pieces, "&")
	last := 0
	for _, p := range parts {
		i := strings.Index(s[last:], p)
		if i < 0 {
			return false
		}
		last += i + len(p)
	}
	return true
}
