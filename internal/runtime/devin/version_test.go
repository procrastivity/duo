package devin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime/devin"
)

func TestParseVersionOutput(t *testing.T) {
	got, err := devin.ParseVersionOutput([]byte("devin 3000.10.21 (611c1cba)\n"))
	if err != nil {
		t.Fatalf("ParseVersionOutput: %v", err)
	}
	want := devin.DetectedVersion{Version: "3000.10.21", Build: "611c1cba"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("version = %+v, want %+v", got, want)
	}
}

func TestParseVersionOutputRejectsMalformedOutput(t *testing.T) {
	for _, output := range []string{
		"",
		"devin 3000.10.21",
		"devin 3000.10.21 (build)\nupdate available",
		"version 3000.10.21 (build)",
	} {
		t.Run(output, func(t *testing.T) {
			if _, err := devin.ParseVersionOutput([]byte(output)); err == nil {
				t.Fatalf("ParseVersionOutput(%q) succeeded", output)
			}
		})
	}
}

func TestSupportedVersionPolicyUsesExactProjectionRange(t *testing.T) {
	policy := devin.SupportedVersionPolicy()
	if len(policy) != 1 || policy[0] != devin.PinnedExternalVersion {
		t.Fatalf("policy = %v, want [%s]", policy, devin.PinnedExternalVersion)
	}
	if devin.TestedVersionRange() != policy[0] {
		t.Fatalf("tested version range = %q, want %q", devin.TestedVersionRange(), policy[0])
	}
	policy[0] = "changed-by-caller"
	if devin.SupportedVersionPolicy()[0] != devin.PinnedExternalVersion {
		t.Fatal("SupportedVersionPolicy returned mutable package state")
	}
}

func TestVersionPolicyMatchesExactAndRangeRules(t *testing.T) {
	policy := devin.VersionPolicy{"3000.10.21", ">=3000.11.0 <3000.12.0"}
	for _, test := range []struct {
		version string
		want    bool
	}{
		{"3000.10.21", true},
		{"3000.11.0", true},
		{"3000.11.99", true},
		{"3000.12.0", false},
		{"3000.10.20", false},
	} {
		if got := policy.Matches(test.version); got != test.want {
			t.Errorf("Matches(%q) = %v, want %v", test.version, got, test.want)
		}
	}
}

func TestFactoryProbeOutsidePolicyIsUnverified(t *testing.T) {
	f := devin.Factory{
		Binary: "/bin/sh",
		VersionProbe: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return []byte("devin 3000.10.22 (other-build)"), nil
		},
	}
	probe, err := f.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.DetectedVersion != "3000.10.22" || probe.Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("probe = %+v, want detected outside policy and unverified", probe)
	}
	wantReason := "detected version 3000.10.22 is outside supported policy 3000.10.21"
	if probe.CompatibilityReason != wantReason {
		t.Fatalf("CompatibilityReason = %q, want %q", probe.CompatibilityReason, wantReason)
	}
}

func TestFactoryProbeVersionCommandErrorIsUnverified(t *testing.T) {
	f := devin.Factory{
		Binary: "/bin/sh",
		VersionProbe: func(context.Context, string, []string) ([]byte, error) {
			return nil, errors.New("status 1")
		},
	}
	probe, err := f.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified || probe.ConnectionState != "version-probe-failed" {
		t.Fatalf("probe = %+v, want explicit unverified command failure", probe)
	}
	if probe.CompatibilityReason == "" {
		t.Fatal("unverified command failure has no compatibility reason")
	}
}

func TestFactoryProbeRejectsMalformedVersionOutput(t *testing.T) {
	f := devin.Factory{
		Binary: "/bin/sh",
		VersionProbe: func(context.Context, string, []string) ([]byte, error) {
			return []byte("devin: update required"), nil
		},
	}
	probe, err := f.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified || probe.ConnectionState != "version-invalid" {
		t.Fatalf("probe = %+v, want explicit unverified malformed output", probe)
	}
	if probe.CompatibilityReason == "" {
		t.Fatal("unverified malformed output has no compatibility reason")
	}
}

func TestFactoryProbeUsesReadOnlyVersionArgsAndConfig(t *testing.T) {
	var gotArgs []string
	f := devin.Factory{
		Binary: "/bin/sh",
		VersionProbe: func(_ context.Context, _ string, args []string) ([]byte, error) {
			gotArgs = append([]string(nil), args...)
			if len(args) != 3 || args[0] != "--config" || args[2] != "--version" {
				t.Fatalf("args = %v, want --config TEMP --version", args)
			}
			if strings.Contains(strings.Join(args, " "), "update") {
				t.Fatalf("version probe args contain an update operation: %v", args)
			}
			config, err := os.ReadFile(args[1])
			if err != nil {
				t.Fatalf("read temporary config: %v", err)
			}
			if string(config) != `{"version":1,"auto_update":false}` {
				t.Fatalf("config = %s, want auto_update false", config)
			}
			info, err := os.Stat(args[1])
			if err != nil {
				t.Fatalf("stat temporary config: %v", err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
			}
			return []byte("devin 3000.10.21 (611c1cba)"), nil
		},
	}
	probe, err := f.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilitySupported {
		t.Fatalf("Compatibility = %s, want supported", probe.Compatibility)
	}
	if len(gotArgs) != 3 {
		t.Fatalf("args = %v, want three fixed args", gotArgs)
	}
	if _, err := os.Stat(filepath.Clean(gotArgs[1])); !os.IsNotExist(err) {
		t.Fatalf("temporary config still exists after probe, stat err = %v", err)
	}
}
