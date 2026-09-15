package devin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// PinnedExternalVersion is the exact Devin CLI version covered by the
// complete devin-workspace.v1 conformance record.
const PinnedExternalVersion = "3000.10.21"

// supportedVersionPolicy is the one policy source used by the adapter
// descriptor, the version probe, and the projection stamp.
var supportedVersionPolicy = VersionPolicy{PinnedExternalVersion}

// SupportedVersionPolicy returns a copy of the selected version rules.
func SupportedVersionPolicy() VersionPolicy {
	return append(VersionPolicy(nil), supportedVersionPolicy...)
}

// TestedVersionRange renders the selected rules for the projection stamp.
func TestedVersionRange() string {
	return strings.Join(supportedVersionPolicy, " || ")
}

// ConformanceRecordDigest names the complete exact-version projection record.
const ConformanceRecordDigest = "devin-workspace-v1-3000.10.21-2026-09-15"

// VersionProbe runs the read-only Devin version command. The argv is supplied
// by Factory.Probe so tests can verify the command boundary without starting
// a Devin process.
type VersionProbe func(context.Context, string, []string) ([]byte, error)

// DetectedVersion is the version and build reported by Devin.
type DetectedVersion struct {
	Version string
	Build   string
}

var versionOutputPattern = regexp.MustCompile(`^devin ([0-9]+(?:\.[0-9]+)+) \(([^()\r\n]+)\)$`)

// ParseVersionOutput accepts only Devin's stable version line. Extra output is
// rejected so a shell error or an update notice cannot be treated as evidence.
func ParseVersionOutput(output []byte) (DetectedVersion, error) {
	line := strings.TrimSpace(string(output))
	matches := versionOutputPattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return DetectedVersion{}, fmt.Errorf("devin: malformed version output %q", line)
	}
	return DetectedVersion{Version: matches[1], Build: matches[2]}, nil
}

// VersionPolicy reports whether a version matches one of its exact or
// comparator rules. Rules can be an exact version or a space-separated set of
// constraints such as ">=3000.10.0 <3000.11.0".
type VersionPolicy []string

// Matches reports whether version is inside at least one policy rule.
func (p VersionPolicy) Matches(version string) bool {
	for _, rule := range p {
		if versionMatchesRule(version, rule) {
			return true
		}
	}
	return false
}

func versionMatchesRule(version, rule string) bool {
	versionParts, ok := parseNumericVersion(version)
	if !ok {
		return false
	}
	tokens := strings.Fields(rule)
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		op := "="
		value := token
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(token, candidate) {
				op, value = candidate, strings.TrimPrefix(token, candidate)
				break
			}
		}
		want, ok := parseNumericVersion(value)
		if !ok {
			return false
		}
		cmp := compareNumericVersions(versionParts, want)
		switch op {
		case "=":
			if cmp != 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case ">=":
			if cmp < 0 {
				return false
			}
		case "<":
			if cmp >= 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		}
	}
	return true
}

func parseNumericVersion(version string) ([]int, bool) {
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return nil, false
	}
	result := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		result[i] = n
	}
	return result, true
}

func compareNumericVersions(a, b []int) int {
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for i := 0; i < length; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func defaultVersionProbe(ctx context.Context, binary string, args []string) ([]byte, error) {
	return exec.CommandContext(ctx, binary, args...).Output()
}

func makeVersionConfig() (string, error) {
	file, err := os.CreateTemp("", "duo-devin-version-*.json")
	if err != nil {
		return "", fmt.Errorf("devin: creating version config: %w", err)
	}
	path := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("devin: setting version config permissions: %w", err)
	}
	config, err := json.Marshal(struct {
		Version    int  `json:"version"`
		AutoUpdate bool `json:"auto_update"`
	}{Version: 1, AutoUpdate: false})
	if err != nil {
		_ = file.Close()
		return "", fmt.Errorf("devin: encoding version config: %w", err)
	}
	if _, err := file.Write(config); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("devin: writing version config: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("devin: closing version config: %w", err)
	}
	return path, nil
}

func probeVersion(ctx context.Context, binary string, run VersionProbe) ([]byte, error) {
	configPath, err := makeVersionConfig()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(configPath) }()
	return run(ctx, binary, []string{"--config", configPath, "--version"})
}
