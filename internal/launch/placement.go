package launch

import (
	"fmt"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/host"
)

// Closed target_source vocabulary on the launch-resolution report
// (duo.external/v1 launch_target_source; notes/51 record 2).
const (
	TargetSourceExplicitFlag  = "explicit-flag"
	TargetSourceConfigDefault = "config-default"
	TargetSourceBuiltIn       = "built-in"
)

// ResolvePlacement picks the effective launch target and its source.
//
// Order (notes/51 record 2): explicit --target, then the kind's
// launch_target, then the host's built-in default. The resolver never
// calls this (I-3); the launcher stamps the pair onto the record after
// resolution and before commit.
//
// Herdr's built-in remains tab (notes/51 record 3).
func ResolvePlacement(flag host.LaunchTarget, kind string, policy config.SessionHostPolicy) (host.LaunchTarget, string, error) {
	switch flag {
	case "":
		// fall through
	case host.LaunchTargetTab, host.LaunchTargetPane:
		return flag, TargetSourceExplicitFlag, nil
	default:
		return "", "", fmt.Errorf("launch: target %q is not a placement this build knows (tab, pane)", flag)
	}

	if kind != "" {
		if stanza, ok := policy.Kinds[kind]; ok && stanza.LaunchTarget != nil {
			t := host.LaunchTarget(*stanza.LaunchTarget)
			switch t {
			case host.LaunchTargetTab, host.LaunchTargetPane:
				return t, TargetSourceConfigDefault, nil
			default:
				return "", "", fmt.Errorf("launch: session_hosts.kinds.%s.launch_target %q is not tab or pane", kind, t)
			}
		}
	}

	return builtInLaunchTarget(kind), TargetSourceBuiltIn, nil
}

// builtInLaunchTarget is the host's product default when neither --target
// nor launch_target is set. Herdr stays tab (notes/51 record 3).
func builtInLaunchTarget(kind string) host.LaunchTarget {
	_ = kind
	return host.LaunchTargetTab
}

// ApplyPlacement stamps target / target_source onto the resolution record
// so Report() and the committed evidence carry them. Call after Resolve
// and before Commit (or on a dry-run / fixture preview that skips Launcher).
func ApplyPlacement(res *Resolution, flag host.LaunchTarget, policy config.SessionHostPolicy) error {
	return applyPlacement(res, flag, policy)
}

// applyPlacement is the unexported twin Launcher calls.
func applyPlacement(res *Resolution, flag host.LaunchTarget, policy config.SessionHostPolicy) error {
	target, source, err := ResolvePlacement(flag, res.Record.Host.Kind, policy)
	if err != nil {
		return err
	}
	res.Record.Target = string(target)
	res.Record.TargetSource = source
	return nil
}
