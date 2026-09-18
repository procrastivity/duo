package herdr

import (
	"context"
	"errors"
	"fmt"

	"github.com/procrastivity/duo/internal/host"
)

// CloseExactAttachment is the narrow pane-control seam used by portable
// launcher conformance fault induction. It refuses to act unless the claim
// completely identifies this integration instance, the pane's terminal
// incarnation, and a proven process birth, then revalidates all of that
// continuity immediately before closing the pane.
//
// Herdr 0.8.2's pane.close accepts only pane_id. Consequently, the full
// continuity check and the mutating request are separate server calls: this
// method cannot provide server-side compare-and-close atomicity, and the pane
// can change between validation and close. It makes no stronger guarantee.
func (h *Host) CloseExactAttachment(ctx context.Context, claim host.HostAttachmentClaim) error {
	if err := h.validateExactCloseClaim(claim); err != nil {
		return err
	}

	continuity, err := h.ValidateAttachment(ctx, claim)
	if err != nil {
		return err
	}
	if continuity.Class != host.ContinuitySameLive {
		return fmt.Errorf("herdr: exact attachment close requires %s continuity, got %s",
			host.ContinuitySameLive, continuity.Class)
	}
	if !evidenceMatchesCloseClaim(continuity.Evidence, claim) {
		return errors.New("herdr: exact attachment close continuity evidence does not match the claim")
	}

	// Do not reinterpret pane_not_found here. After successful validation it
	// means the separate close request did not prove the requested induction.
	return h.client.call(ctx, "pane.close", paneTargetParams{PaneID: claim.Attachment.PaneID}, nil)
}

func (h *Host) validateExactCloseClaim(claim host.HostAttachmentClaim) error {
	attachment := claim.Attachment
	if attachment.IntegrationInstanceID == "" {
		return errors.New("herdr: exact attachment close requires an integration instance ID")
	}
	if err := h.requireInstance(attachment.IntegrationInstanceID); err != nil {
		return err
	}
	if attachment.PaneID == "" {
		return errors.New("herdr: exact attachment close requires a pane ID")
	}
	if attachment.HostContainerID == "" {
		return errors.New("herdr: exact attachment close requires a terminal container ID")
	}
	if !birthProven(claim.LastKnownProcessBirth) {
		return errors.New("herdr: exact attachment close requires proven process birth")
	}
	return nil
}

func evidenceMatchesCloseClaim(evidence host.Evidence, claim host.HostAttachmentClaim) bool {
	attachment := claim.Attachment
	return evidence.IntegrationInstanceID == attachment.IntegrationInstanceID &&
		evidence.HostServerEpoch == attachment.HostServerEpoch &&
		evidence.HostContainerID == attachment.HostContainerID &&
		evidence.PaneID == attachment.PaneID &&
		sameBirth(evidence.ProcessBirth, claim.LastKnownProcessBirth)
}
