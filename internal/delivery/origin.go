package delivery

import "github.com/procrastivity/duo/internal/domain"

// DuoCreated reports whether this session's current host attachment was
// stamped at spawn. The launch path records the pane with
// domain.SourceLaunchPlan (recordLaunchAttachments Bind after Start).
// Enroll writes "host-report" instead. There is no later human-attach
// signal on Herdr (notes/19); the carve-out is therefore this stamp
// plus launch-settled idle, not a live writer-presence check.
func DuoCreated(a *domain.Authority, session domain.SessionID) bool {
	if a == nil {
		return false
	}
	s, ok := a.Session(session)
	if !ok || s.Attachment == "" {
		return false
	}
	for _, c := range a.Correlations(domain.TargetAttachment, string(s.Attachment)) {
		if c.Status != domain.CorrelationActive {
			continue
		}
		if c.Source == string(domain.SourceLaunchPlan) {
			return true
		}
	}
	return false
}
