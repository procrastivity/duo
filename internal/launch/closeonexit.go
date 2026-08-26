package launch

import "github.com/procrastivity/duo/internal/config"

// ResolveCloseOnExit picks the effective close-on-exit bool for a launch.
//
// Order (notes/51 record 7 stop-gate edit): explicit --remain-on-exit
// forces false; else the kind's close_on_exit when set; else true (the
// product default). The resolver never calls this (I-3); the launcher
// resolves it after Resolve and before Augment / PrepareLaunch.
func ResolveCloseOnExit(remainOnExit bool, kind string, policy config.SessionHostPolicy) bool {
	if remainOnExit {
		return false
	}
	if kind != "" {
		if stanza, ok := policy.Kinds[kind]; ok && stanza.CloseOnExit != nil {
			return *stanza.CloseOnExit
		}
	}
	return true
}
