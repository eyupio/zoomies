package analysis

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/eyupio/zoomies/internal/catalog"
)

// A code the catalog does not have sends a reader to a page that does not exist.
func TestEveryCodeAFindingNamesIsInTheCatalog(t *testing.T) {
	c, err := catalog.Current()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, e := range c.Entries {
		known[e.ID] = true
	}
	codes := CatalogCodes()
	if len(codes) == 0 {
		t.Fatal("CatalogCodes is empty, so nothing is checked")
	}
	for _, code := range codes {
		if !known[code] {
			t.Errorf("an analysis can name %q, which the catalog does not have", code)
		}
	}
}

func TestWordsSaysADurationTheWayAPersonWould(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{725 * time.Second, "12m 5s"},
		{11400 * time.Second, "3h 10m"},
		{2400 * time.Second, "40m"},
		{0, "0s"},
		{-5 * time.Second, "0s"},
	} {
		if got := Words(c.d); got != c.want {
			t.Errorf("Words(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// The routes, the tools and the UI read the same JSON, and the OpenAPI schema
// and the generated TypeScript name every field in snake_case. A field left
// untagged would marshal as PoolID and match neither.
func TestEveryEvidenceFieldHasASnakeCaseJSONName(t *testing.T) {
	for _, v := range []any{QueueWaitFinding{}, CompareFinding{}, HostPressureFinding{}, StartupFailuresFinding{}, RecurringFailuresFinding{}, Interval{}} {
		checkJSONNames(t, reflect.TypeOf(v), map[reflect.Type]bool{})
	}
}

func checkJSONNames(t *testing.T, ty reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	for ty.Kind() == reflect.Slice || ty.Kind() == reflect.Pointer {
		ty = ty.Elem()
	}
	if ty.Kind() != reflect.Struct || ty.PkgPath() != reflect.TypeOf(Interval{}).PkgPath() || seen[ty] {
		return
	}
	seen[ty] = true
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if want := snakeCase(f.Name); name != want {
			t.Errorf("%s.%s has json name %q, want %q", ty.Name(), f.Name, name, want)
		}
		checkJSONNames(t, f.Type, seen)
	}
}

// snakeCase is a Go field name as the API spells it: JobID is job_id,
// ConfidenceReason is confidence_reason, P50MS is p50_ms.
func snakeCase(name string) string {
	r := []rune(name)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

func TestSnakeCaseSpellsAFieldNameAsTheAPIDoes(t *testing.T) {
	for name, want := range map[string]string{"JobID": "job_id", "ConfidenceReason": "confidence_reason",
		"P50MS": "p50_ms", "Since": "since", "HTTPServer": "http_server"} {
		if got := snakeCase(name); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", name, got, want)
		}
	}
}

// assertNoCommand holds a finding to the catalog's own rule: a next step
// names a setting or a code to read, and a command to paste would be run by
// someone who never read why.
func assertNoCommand(t *testing.T, steps []NextStep) {
	t.Helper()
	for _, s := range steps {
		for _, bad := range []string{"zoomies ", "sudo ", "docker ", "systemctl ", "`"} {
			if strings.Contains(s.Text, bad) {
				t.Errorf("next step %q reads as a command (%q)", s.Text, bad)
			}
		}
	}
}
