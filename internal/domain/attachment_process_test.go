package domain_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

func claimFromAttachment(att domain.HostAttachment) domain.ClaimRef {
	return domain.Fingerprint{
		IntegrationInstance: att.IntegrationInstance,
		Epoch:               att.Epoch,
		Container:           att.Container,
		Process:             att.Process,
	}.ClaimRef()
}

// TestEnrollmentPersistsProcessBirthOnTheAttachment is the enroll write:
// createEnrollment copies fp.Process onto the attachment, and that is
// enough to rebuild the exact claim without rereading the host.
func TestEnrollmentPersistsProcessBirthOnTheAttachment(t *testing.T) {
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	atts := h.a.Attachments(res.Session)
	if len(atts) != 1 {
		t.Fatalf("Attachments = %d, want 1", len(atts))
	}
	if atts[0].Process != fp.Process {
		t.Fatalf("stored Process = %+v, want %+v", atts[0].Process, fp.Process)
	}
	if !atts[0].Process.Present() {
		t.Fatal("enrolled Process is not present")
	}
	if got := claimFromAttachment(atts[0]); got != fp.ClaimRef() {
		t.Fatalf("rebuilt claim = %s, want %s", got, fp.ClaimRef())
	}
	session, _ := h.a.Session(res.Session)
	if session.Attachment != atts[0].ID {
		t.Fatalf("Session.Attachment = %s, want last-leaf %s", session.Attachment, atts[0].ID)
	}

	h.reopen()
	atts = h.a.Attachments(res.Session)
	if len(atts) != 1 || atts[0].Process != fp.Process {
		t.Fatalf("after reopen, Process = %+v, want %+v", atts, fp.Process)
	}
	if got := claimFromAttachment(atts[0]); got != fp.ClaimRef() {
		t.Fatalf("after reopen, rebuilt claim = %s, want %s", got, fp.ClaimRef())
	}
}

// TestBindPersistsProcessBirthOnTheAttachment is the launch write: Bind's
// fingerprint branch copies fp.Process onto the attachment it creates.
func TestBindPersistsProcessBirthOnTheAttachment(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	launched, err := h.a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo", Actor: "user:beau",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	if err := h.a.Bind(ctx, domain.BindRequest{
		Session:     launched.Session,
		Actor:       "host",
		Attestation: domain.Attestation{Source: domain.SourceLaunchPlan},
		Fingerprint: &fp,
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	atts := h.a.Attachments(launched.Session)
	if len(atts) != 1 {
		t.Fatalf("Attachments = %d, want 1", len(atts))
	}
	if atts[0].Process != fp.Process {
		t.Fatalf("stored Process = %+v, want %+v", atts[0].Process, fp.Process)
	}
	if got := claimFromAttachment(atts[0]); got != fp.ClaimRef() {
		t.Fatalf("rebuilt claim = %s, want %s", got, fp.ClaimRef())
	}
}

// TestTwoLeavesStoreDistinctProcessBirthsWithoutZippingCorrelations pins
// the collection vs last-leaf split: two Binds store two Process values
// on two attachments, Session.Attachment stays the last leaf, and the
// instance still has N process.birth correlations that are not the pairing
// key.
func TestTwoLeavesStoreDistinctProcessBirthsWithoutZippingCorrelations(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	launched, err := h.a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo", Actor: "user:beau",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	first := herdrFingerprint("w1:p1", "term_a", 4242)
	second := herdrFingerprint("w1:p2", "term_b", 5353)
	for _, fp := range []domain.Fingerprint{first, second} {
		if err := h.a.Bind(ctx, domain.BindRequest{
			Session:     launched.Session,
			Actor:       "host",
			Attestation: domain.Attestation{Source: domain.SourceLaunchPlan},
			Fingerprint: &fp,
		}); err != nil {
			t.Fatalf("Bind %s: %v", fp.Container, err)
		}
	}

	atts := h.a.Attachments(launched.Session)
	if len(atts) != 2 {
		t.Fatalf("Attachments = %d, want 2", len(atts))
	}
	if atts[0].ID >= atts[1].ID {
		t.Fatalf("Attachments not sorted by ID: %s, %s", atts[0].ID, atts[1].ID)
	}
	if atts[0].Process == atts[1].Process {
		t.Fatalf("both leaves stored the same Process %+v", atts[0].Process)
	}

	byContainer := map[string]domain.HostAttachment{}
	for _, att := range atts {
		byContainer[att.Container] = att
	}
	for _, fp := range []domain.Fingerprint{first, second} {
		att, ok := byContainer[fp.Container]
		if !ok {
			t.Fatalf("no attachment for container %s", fp.Container)
		}
		if att.Process != fp.Process {
			t.Fatalf("container %s Process = %+v, want %+v", fp.Container, att.Process, fp.Process)
		}
		if got := claimFromAttachment(att); got != fp.ClaimRef() {
			t.Fatalf("container %s rebuilt claim = %s, want %s", fp.Container, got, fp.ClaimRef())
		}
	}

	session, _ := h.a.Session(launched.Session)
	last := byContainer[second.Container]
	if session.Attachment != last.ID {
		t.Fatalf("Session.Attachment = %s, want last-leaf %s", session.Attachment, last.ID)
	}

	var births []domain.Correlation
	for _, c := range h.a.Correlations(domain.TargetInstance, string(launched.Instance)) {
		if c.ExternalKind == "process.birth" {
			births = append(births, c)
		}
	}
	if len(births) != 2 {
		t.Fatalf("process.birth correlations = %d, want 2", len(births))
	}
	// The pairing key is att.Process, looked up by container above — not
	// births[i] zipped to atts[i] by order or timestamp.
}
