package installer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// An answer file is an unattended install, and the account it makes is the
// platform one: the audit log has to say the system made it and that an
// answer file said to, or the first row of every such instance's history is
// an account nobody can account for.
func TestTheAnswerFileAdministratorIsAudited(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: ":memory:"})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	i := &Installer{out: &bytes.Buffer{}, ui: newUI(&bytes.Buffer{}), log: slog.New(slog.DiscardHandler)}
	p := Plan{AdminUser: "ada", adminPassword: "correct-horse-battery"}
	if err := i.stepAdmin(ctx, st, config.Default(), p); err != nil {
		t.Fatalf("stepAdmin: %v", err)
	}

	rows, _, err := st.ListAudit(ctx, store.AuditFilter{Actions: []string{"auth.bootstrap"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d auth.bootstrap rows; want exactly one", len(rows))
	}
	if rows[0].ActorKind != auth.KindSystem || !strings.Contains(rows[0].After, auth.BootstrapAnswerFile) {
		t.Fatalf("the row is by %s and says %s; want the system and the answer file", rows[0].ActorKind, rows[0].After)
	}
}
