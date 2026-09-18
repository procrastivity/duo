//go:build linux

package portablelauncher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

const herdrTerminalEpochKind = "herdr.terminal_id"

// AuthorityAttachmentClaimResolver resolves an exact controller target from
// the durable authority log. Resolution is by AttachmentID only; it never
// infers a session from launch order, a PID, or mutable host coordinates.
type AuthorityAttachmentClaimResolver struct {
	load             AuthoritySnapshotLoader
	herdrIntegration string
}

// NewAuthorityAttachmentClaimResolver constructs a read-only durable claim
// resolver for one exact Herdr integration instance.
func NewAuthorityAttachmentClaimResolver(load AuthoritySnapshotLoader, herdrIntegration string) (*AuthorityAttachmentClaimResolver, error) {
	switch {
	case load == nil:
		return nil, fmt.Errorf("authority attachment claim resolver requires a snapshot loader")
	case herdrIntegration == "":
		return nil, fmt.Errorf("authority attachment claim resolver requires the exact Herdr integration identity")
	}
	return &AuthorityAttachmentClaimResolver{load: load, herdrIntegration: herdrIntegration}, nil
}

// Resolve replays one read-only snapshot and returns the exact durable claim
// selected by target.AttachmentID. Every relationship needed to authorize
// live process control is checked again on each call.
func (r *AuthorityAttachmentClaimResolver) Resolve(ctx context.Context, target ProcessIdentity) (host.HostAttachmentClaim, error) {
	if r == nil || r.load == nil || r.herdrIntegration == "" {
		return host.HostAttachmentClaim{}, fmt.Errorf("authority attachment claim resolver is not configured")
	}
	if !validProcessIdentity(target) {
		return host.HostAttachmentClaim{}, fmt.Errorf("authority attachment claim resolution requires a complete process identity")
	}
	facts, err := r.load(ctx)
	if err != nil {
		return host.HostAttachmentClaim{}, fmt.Errorf("load authority attachment snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return host.HostAttachmentClaim{}, err
	}
	projection, err := projectSnapshot(facts)
	if err != nil {
		return host.HostAttachmentClaim{}, fmt.Errorf("project authority attachment snapshot: %w", err)
	}

	attachment, ok := projection.authority.Attachment(domain.AttachmentID(target.AttachmentID))
	if !ok || string(attachment.ID) != target.AttachmentID {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q does not exist", target.AttachmentID)
	}
	if attachment.State != domain.Attached || attachment.Continuity != domain.ContinuityVerified {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q is not attached with verified continuity", target.AttachmentID)
	}
	if attachment.IntegrationInstance != r.herdrIntegration {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q belongs to a different Herdr integration", target.AttachmentID)
	}
	if attachment.Epoch.Kind != herdrTerminalEpochKind || attachment.Epoch.Scope != domain.EpochScopePane ||
		attachment.Epoch.Value == "" || attachment.Epoch.Value != target.TerminalID || attachment.Container == "" {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q has mismatched Herdr terminal or pane identity", target.AttachmentID)
	}
	if !attachment.Process.Present() || attachment.Process.PID != target.PID || attachment.Process.StartedAt != target.StartTime {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q has mismatched process birth", target.AttachmentID)
	}
	started, err := time.Parse(materialize.CaptureTimeLayout, attachment.Process.StartedAt)
	if err != nil || started.UTC().Format(materialize.CaptureTimeLayout) != attachment.Process.StartedAt || started.UnixMilli() < 0 {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q has a non-canonical process start time", target.AttachmentID)
	}

	session, ok := projection.authority.Session(attachment.Session)
	if !ok || session.ID != attachment.Session || session.State != domain.SessionActive ||
		session.Attachment != attachment.ID || session.Current == "" {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q is not the owning active session attachment", target.AttachmentID)
	}
	instance, ok := projection.authority.Instance(session.Current)
	if !ok || instance.ID != session.Current || instance.Session != session.ID || instance.State != domain.InstanceLive {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q has no consistent current live instance", target.AttachmentID)
	}
	fingerprint := domain.Fingerprint{
		IntegrationInstance: attachment.IntegrationInstance,
		Epoch:               attachment.Epoch,
		Container:           attachment.Container,
		Process:             attachment.Process,
	}
	claimRef := fingerprint.ClaimRef()
	active, held := projection.authority.ActiveClaim(claimRef)
	if !held || active.Ref != claimRef || active.Session != session.ID || active.Instance != instance.ID || active.Degraded {
		return host.HostAttachmentClaim{}, fmt.Errorf("durable attachment %q has no consistent active host claim", target.AttachmentID)
	}

	return host.HostAttachmentClaim{
		Attachment: host.Attachment{
			IntegrationInstanceID: attachment.IntegrationInstance,
			HostServerEpoch:       herdr.NoServerEpoch,
			HostContainerID:       attachment.Epoch.Value,
			PaneID:                attachment.Container,
		},
		LastKnownProcessBirth: host.ProcessBirthEvidence{
			PID:             attachment.Process.PID,
			StartTime:       started,
			StartTimeSource: herdr.StartTimeSourceProcfs,
		},
	}, nil
}

// ExactAttachmentHost is the complete Herdr seam needed by live fault
// induction: exact continuity validation and exact pane closure.
type ExactAttachmentHost interface {
	host.HostAttachmentValidator
	CloseExactAttachment(context.Context, host.HostAttachmentClaim) error
}

var _ ExactAttachmentHost = (*herdr.Host)(nil)

type attachmentClaimResolver interface {
	Resolve(context.Context, ProcessIdentity) (host.HostAttachmentClaim, error)
}

type exactProcessControl interface {
	AcquireExactTarget(context.Context, ProcessIdentity) error
	SendSIGSTOP(context.Context, ProcessIdentity) error
	VerifyStopped(context.Context, ProcessIdentity) error
	SendSIGKILL(context.Context, ProcessIdentity) error
	Release(ProcessIdentity) error
}

var _ exactProcessControl = (*LinuxProcessControl)(nil)

type acquiredLiveTarget struct {
	claim host.HostAttachmentClaim
}

// LinuxHerdrLiveFaultController composes durable authority resolution, Herdr
// continuity/close operations, and Linux pidfds behind the launcher-neutral
// FaultController contract.
type LinuxHerdrLiveFaultController struct {
	resolver attachmentClaimResolver
	host     ExactAttachmentHost
	process  exactProcessControl
	exact    *ExactFaultController

	mu       sync.Mutex
	acquired map[ProcessIdentity]acquiredLiveTarget
}

var _ FaultController = (*LinuxHerdrLiveFaultController)(nil)

// NewLinuxHerdrLiveFaultController constructs the production live controller.
// monotonic must be the same run-origin clock used by the checkpoint observer.
func NewLinuxHerdrLiveFaultController(
	resolver *AuthorityAttachmentClaimResolver,
	exactHost ExactAttachmentHost,
	monotonic MonotonicClock,
) (*LinuxHerdrLiveFaultController, error) {
	if resolver == nil {
		return nil, fmt.Errorf("linux Herdr live fault controller requires an authority claim resolver")
	}
	return newLinuxHerdrLiveFaultController(resolver, exactHost, monotonic, NewLinuxProcessControl())
}

// newLinuxHerdrLiveFaultController is the test seam for the pidfd primitive.
// Production callers cannot inject an arbitrary signaling implementation.
func newLinuxHerdrLiveFaultController(
	resolver attachmentClaimResolver,
	exactHost ExactAttachmentHost,
	monotonic MonotonicClock,
	process exactProcessControl,
) (*LinuxHerdrLiveFaultController, error) {
	switch {
	case resolver == nil:
		return nil, fmt.Errorf("linux Herdr live fault controller requires an authority claim resolver")
	case exactHost == nil:
		return nil, fmt.Errorf("linux Herdr live fault controller requires an exact attachment host")
	case monotonic == nil:
		return nil, fmt.Errorf("linux Herdr live fault controller requires the shared run-origin monotonic clock")
	case process == nil:
		return nil, fmt.Errorf("linux Herdr live fault controller requires Linux process control")
	}

	c := &LinuxHerdrLiveFaultController{
		resolver: resolver,
		host:     exactHost,
		process:  process,
		acquired: make(map[ProcessIdentity]acquiredLiveTarget),
	}
	exact, err := NewExactFaultController(ExactFaultControllerConfig{
		VerifyLiveTarget: c.verifyAndAcquireLiveTarget,
		StopProcess:      c.process.SendSIGSTOP,
		VerifyStopped:    c.process.VerifyStopped,
		ClosePane:        c.closeStablePane,
		VerifyPaneAbsent: c.verifyPaneAbsentAndRelease,
		Monotonic:        monotonic,
	})
	if err != nil {
		return nil, err
	}
	c.exact = exact
	return c, nil
}

// SuspendExactProcess implements FaultController.
func (c *LinuxHerdrLiveFaultController) SuspendExactProcess(ctx context.Context, target ProcessIdentity) (ControlCheckpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exact.SuspendExactProcess(ctx, target)
}

// CloseExactPane implements FaultController.
func (c *LinuxHerdrLiveFaultController) CloseExactPane(ctx context.Context, target ProcessIdentity) (ControlCheckpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exact.CloseExactPane(ctx, target)
}

func (c *LinuxHerdrLiveFaultController) verifyAndAcquireLiveTarget(ctx context.Context, target ProcessIdentity) error {
	claim, err := c.resolver.Resolve(ctx, target)
	if err != nil {
		return err
	}
	if err := requireClaimMatchesTarget(claim, target); err != nil {
		return err
	}
	if _, exists := c.acquired[target]; exists {
		return fmt.Errorf("exact live target is already acquired")
	}
	if err := c.requireSameLive(ctx, claim); err != nil {
		return fmt.Errorf("validate exact Herdr continuity before pidfd acquisition: %w", err)
	}
	if err := c.process.AcquireExactTarget(ctx, target); err != nil {
		return err
	}
	if err := c.requireSameLive(ctx, claim); err != nil {
		releaseErr := c.process.Release(target)
		return errors.Join(
			fmt.Errorf("revalidate exact Herdr continuity after pidfd acquisition: %w", err),
			wrapIfError("release pidfd after failed post-acquire validation", releaseErr),
		)
	}
	c.acquired[target] = acquiredLiveTarget{claim: claim}
	return nil
}

func (c *LinuxHerdrLiveFaultController) closeStablePane(ctx context.Context, target ProcessIdentity) error {
	state, ok := c.acquired[target]
	if !ok {
		return fmt.Errorf("exact target has no acquired durable claim")
	}
	claim, err := c.resolver.Resolve(ctx, target)
	if err != nil {
		return fmt.Errorf("re-resolve durable claim before pane close: %w", err)
	}
	if err := requireClaimMatchesTarget(claim, target); err != nil {
		return fmt.Errorf("revalidate target before pane close: %w", err)
	}
	if !sameAttachmentClaim(claim, state.claim) {
		return fmt.Errorf("durable attachment claim changed after process suspension")
	}
	return c.host.CloseExactAttachment(ctx, claim)
}

func (c *LinuxHerdrLiveFaultController) verifyPaneAbsentAndRelease(ctx context.Context, target ProcessIdentity) error {
	state, ok := c.acquired[target]
	if !ok {
		return fmt.Errorf("exact target has no acquired durable claim")
	}
	continuity, err := c.host.ValidateAttachment(ctx, state.claim)
	if err != nil {
		return err
	}
	if continuity.Class != host.ContinuityPaneAbsent {
		return fmt.Errorf("exact pane absence requires %s continuity, got %s", host.ContinuityPaneAbsent, continuity.Class)
	}
	if err := c.process.Release(target); err != nil {
		return fmt.Errorf("release exact target after pane absence: %w", err)
	}
	delete(c.acquired, target)
	return nil
}

func (c *LinuxHerdrLiveFaultController) requireSameLive(ctx context.Context, claim host.HostAttachmentClaim) error {
	continuity, err := c.host.ValidateAttachment(ctx, claim)
	if err != nil {
		return err
	}
	if continuity.Class != host.ContinuitySameLive || !continuity.SameProcess {
		return fmt.Errorf("exact live target requires %s continuity, got %s", host.ContinuitySameLive, continuity.Class)
	}
	if !evidenceMatchesClaim(continuity.Evidence, claim) {
		return fmt.Errorf("same-live continuity evidence does not exactly match the durable claim")
	}
	return nil
}

// Cleanup conservatively releases every still-owned pidfd. It attempts normal
// exact pane closure only while durable identity and same-live Herdr evidence
// remain exact. SIGKILL is an escalation only for a failed normal exact close;
// it is sent through the already-owned pidfd and never stands in for proof of
// pane absence.
func (c *LinuxHerdrLiveFaultController) Cleanup(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var cleanupErrors []error
	for target, state := range c.acquired {
		var targetErrors []error
		claim, err := c.resolver.Resolve(ctx, target)
		if err != nil {
			targetErrors = append(targetErrors, fmt.Errorf("re-resolve durable claim: %w", err))
		} else if err := requireClaimMatchesTarget(claim, target); err != nil {
			targetErrors = append(targetErrors, fmt.Errorf("revalidate cleanup target: %w", err))
		} else if !sameAttachmentClaim(claim, state.claim) {
			targetErrors = append(targetErrors, fmt.Errorf("durable attachment claim changed before cleanup"))
		} else {
			continuity, validateErr := c.host.ValidateAttachment(ctx, claim)
			switch {
			case validateErr != nil:
				targetErrors = append(targetErrors, fmt.Errorf("validate exact Herdr continuity for cleanup: %w", validateErr))
			case continuity.Class == host.ContinuityPaneAbsent:
				// Absence is independently proven; only descriptor release remains.
			case continuity.Class != host.ContinuitySameLive || !continuity.SameProcess || !evidenceMatchesClaim(continuity.Evidence, claim):
				targetErrors = append(targetErrors, fmt.Errorf("cleanup refused non-exact host continuity %s", continuity.Class))
			default:
				closeErr := c.host.CloseExactAttachment(ctx, claim)
				if closeErr != nil {
					targetErrors = append(targetErrors, fmt.Errorf("normal exact pane close failed: %w", closeErr))
				}
				absence, absenceErr := c.host.ValidateAttachment(ctx, claim)
				// A failed exact close can itself report a continuity race. Recheck
				// before escalation so a replaced or unproven host identity is
				// never killed based only on the earlier same-live observation.
				if closeErr != nil && absenceErr == nil &&
					absence.Class == host.ContinuitySameLive && absence.SameProcess &&
					evidenceMatchesClaim(absence.Evidence, claim) {
					if killErr := c.process.SendSIGKILL(ctx, target); killErr != nil {
						targetErrors = append(targetErrors, fmt.Errorf("exact pidfd SIGKILL escalation failed: %w", killErr))
					}
					absence, absenceErr = c.host.ValidateAttachment(ctx, claim)
				}
				if absenceErr != nil {
					targetErrors = append(targetErrors, fmt.Errorf("verify pane absence after cleanup close: %w", absenceErr))
				} else if absence.Class != host.ContinuityPaneAbsent {
					targetErrors = append(targetErrors, fmt.Errorf("pane cleanup not proven absent: got %s", absence.Class))
				}
			}
		}

		if releaseErr := c.process.Release(target); releaseErr != nil {
			targetErrors = append(targetErrors, fmt.Errorf("release exact target pidfd: %w", releaseErr))
		}
		delete(c.acquired, target)
		if err := errors.Join(targetErrors...); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup exact target attachment %s: %w", target.AttachmentID, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func requireClaimMatchesTarget(claim host.HostAttachmentClaim, target ProcessIdentity) error {
	attachment := claim.Attachment
	birth := claim.LastKnownProcessBirth
	if attachment.IntegrationInstanceID == "" || attachment.HostServerEpoch != herdr.NoServerEpoch ||
		attachment.HostContainerID != target.TerminalID || attachment.PaneID == "" ||
		birth.PID != target.PID || birth.StartTimeSource != herdr.StartTimeSourceProcfs ||
		birth.StartTime.IsZero() || birth.StartTime.UTC().Format(materialize.CaptureTimeLayout) != target.StartTime {
		return fmt.Errorf("durable attachment claim does not exactly match the requested process identity")
	}
	return nil
}

func sameAttachmentClaim(left, right host.HostAttachmentClaim) bool {
	return left.Attachment == right.Attachment &&
		left.LastKnownProcessBirth.PID == right.LastKnownProcessBirth.PID &&
		left.LastKnownProcessBirth.StartTimeSource == right.LastKnownProcessBirth.StartTimeSource &&
		left.LastKnownProcessBirth.StartTime.Equal(right.LastKnownProcessBirth.StartTime)
}

func evidenceMatchesClaim(evidence host.Evidence, claim host.HostAttachmentClaim) bool {
	attachment := claim.Attachment
	birth := claim.LastKnownProcessBirth
	return evidence.IntegrationInstanceID == attachment.IntegrationInstanceID &&
		evidence.HostServerEpoch == attachment.HostServerEpoch &&
		evidence.HostContainerID == attachment.HostContainerID &&
		evidence.PaneID == attachment.PaneID &&
		evidence.ProcessBirth.PID == birth.PID &&
		evidence.ProcessBirth.StartTimeSource == birth.StartTimeSource &&
		evidence.ProcessBirth.StartTime.Equal(birth.StartTime)
}

func wrapIfError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
