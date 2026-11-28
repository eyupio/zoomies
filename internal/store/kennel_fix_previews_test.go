package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func kennelFixPreviewFixture(t *testing.T, s *Store) *KennelFixPreview {
	t.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme/repo", TargetType: TargetRepo}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	return &KennelFixPreview{ID: NewID(PrefixKennelFixPreview), InstallationID: inst.ID, Repository: inst.Target, InitiatingUser: "usr_actor", Version: KennelFixPreviewVersion, Payload: json.RawMessage(`{"version":"kennel-fix-retained-v1","evidence":"sample"}`)}
}

// A fix is bound to its file and not to the branch head, so a draft may be based
// on a newer head than the one the preview was made at.
func TestKennelFixPublicationMayBeBasedOnANewerHeadThanThePreview(t *testing.T) {
	s := newTestStore(t)
	r := kennelFixPreviewFixture(t, s)
	ctx := context.Background()
	if err := s.PutKennelFixPreview(ctx, r, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	head, newer, source := strings.Repeat("a", 40), strings.Repeat("c", 40), strings.Repeat("b", 64)
	if _, err := s.ApproveKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID, head, source, r.SHA256, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	pub := &KennelFixPublication{
		PreviewID: r.ID, InstallationID: r.InstallationID, Repository: r.Repository, PublishedBy: "usr_actor",
		Branch: "zoomies/kennel-fix-" + r.ID, BaseBranch: "main", BaseSHA: newer, HeadSHA: head, SourceSHA256: source, PreviewSHA256: r.SHA256,
		PullNumber: 12, PullURL: "https://github.com/acme/repo/pull/12",
	}
	if err := s.PutKennelFixPublication(ctx, pub, optimisationAudit()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetKennelFixPreview(ctx, r.InstallationID, r.Repository, r.ID)
	if err != nil || got.Publication == nil || got.Publication.BaseSHA != newer || got.Publication.HeadSHA != head {
		t.Fatalf("publication %+v err=%v", got.Publication, err)
	}
	// What was approved is still what is published: a different head is refused.
	other := *pub
	other.HeadSHA = newer
	if err := s.PutKennelFixPublication(ctx, &other, optimisationAudit()); !errors.Is(err, ErrConflict) {
		t.Fatalf("a publication of a head nobody approved: %v", err)
	}
}
