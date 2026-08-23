package pi

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
)

// extensionAsset is the generated Pi reporter extension, embedded so the
// binary always carries the exact source it installs. It is a template only
// in the narrow sense that three names are substituted (see RenderExtension);
// everything load-bearing — the ctx.mode gate, the load-then-scrub sequence,
// the quit-only terminality check — is fixed in the asset and guarded by
// extension_test.go.
//
//go:embed extension/duo-pi-reporter.ts
var extensionAsset string

// ExtensionFileName is the file name Duo writes the rendered extension to.
// Pi discovers extensions in ~/.pi/agent/extensions/ (global) and
// .pi/extensions/ (project, after trust); choosing between those is an
// installation concern, not this package's.
const ExtensionFileName = "duo-pi-reporter.ts"

// ReporterProtocol tags the correlation record the extension writes. It
// versions the socket line, not the extension file: a change to the record's
// field set requires a new tag so a stale installed extension is refused
// rather than misread.
const ReporterProtocol = "duo.pi.reporter/v1"

// Default environment-variable names for the reporter socket and the
// per-runtime-instance credential. §8's spawn builder constructs the whole
// environment; these are the two entries this adapter adds.
const (
	DefaultSocketEnvVar = "DUO_REPORTER_SOCKET"
	DefaultTokenEnvVar  = "DUO_REPORTER_TOKEN"
)

// MaxUnixSocketPath is the longest reporter socket path that can be bound.
// sun_path holds 108 bytes including the terminating NUL. This is not
// theoretical: the 0.83.0 probe's first listen attempt failed with EINVAL on
// a long scratchpad path, so the check belongs at path-construction time,
// where the failure is legible.
const MaxUnixSocketPath = 107

// Asset placeholders. The asset carries no secret and no path: the socket
// path and credential travel in the environment and are read (and scrubbed)
// at module load, so only the variable names and the protocol tag are
// substituted here.
const (
	placeholderProtocol  = "__DUO_PROTOCOL__"
	placeholderSocketEnv = "__DUO_SOCKET_ENV__"
	placeholderTokenEnv  = "__DUO_TOKEN_ENV__"
)

// ExtensionConfig names the environment variables the rendered extension
// reads at module load. The zero value renders the defaults.
type ExtensionConfig struct {
	SocketEnvVar string
	TokenEnvVar  string
}

func (c ExtensionConfig) withDefaults() ExtensionConfig {
	if c.SocketEnvVar == "" {
		c.SocketEnvVar = DefaultSocketEnvVar
	}
	if c.TokenEnvVar == "" {
		c.TokenEnvVar = DefaultTokenEnvVar
	}
	return c
}

// ExtensionSource returns the embedded asset with its placeholders intact.
// It exists so tests and diagnostics can inspect the shipped source without
// choosing an environment; installation uses RenderExtension.
func ExtensionSource() string { return extensionAsset }

// RenderExtension returns the extension source Duo writes to disk for one
// runtime instance.
func RenderExtension(cfg ExtensionConfig) (string, error) {
	cfg = cfg.withDefaults()
	if err := validEnvVarName(cfg.SocketEnvVar); err != nil {
		return "", fmt.Errorf("pi: socket env var: %w", err)
	}
	if err := validEnvVarName(cfg.TokenEnvVar); err != nil {
		return "", fmt.Errorf("pi: token env var: %w", err)
	}

	out := extensionAsset
	out = strings.ReplaceAll(out, placeholderProtocol, ReporterProtocol)
	out = strings.ReplaceAll(out, placeholderSocketEnv, cfg.SocketEnvVar)
	out = strings.ReplaceAll(out, placeholderTokenEnv, cfg.TokenEnvVar)
	if strings.Contains(out, "__DUO_") {
		return "", fmt.Errorf("pi: rendered extension still contains a placeholder")
	}
	return out, nil
}

// ReporterEnvironment returns the two environment entries the rendered
// extension expects. Duo's spawn builder merges these into the constructed
// environment; the extension deletes both from process.env at module load,
// so no tool subprocess inherits the credential.
func ReporterEnvironment(cfg ExtensionConfig, socketPath, credential string) (map[string]string, error) {
	cfg = cfg.withDefaults()
	if err := ValidateSocketPath(socketPath); err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, fmt.Errorf("pi: reporter credential is empty: a reporter credential names one exact runtime instance")
	}
	if err := validEnvVarName(cfg.SocketEnvVar); err != nil {
		return nil, fmt.Errorf("pi: socket env var: %w", err)
	}
	if err := validEnvVarName(cfg.TokenEnvVar); err != nil {
		return nil, fmt.Errorf("pi: token env var: %w", err)
	}
	return map[string]string{
		cfg.SocketEnvVar: socketPath,
		cfg.TokenEnvVar:  credential,
	}, nil
}

// ValidateSocketPath rejects a reporter socket path the extension could not
// bind. Both rules are failure modes observed at 0.83.0 rather than style
// preferences: a relative path binds relative to pi's cwd (which the user can
// change), and an over-long path fails at listen with EINVAL.
func ValidateSocketPath(socketPath string) error {
	if socketPath == "" {
		return fmt.Errorf("pi: reporter socket path is empty")
	}
	if !filepath.IsAbs(socketPath) {
		return fmt.Errorf("pi: reporter socket path %q is not absolute", socketPath)
	}
	if len(socketPath) > MaxUnixSocketPath {
		return fmt.Errorf(
			"pi: reporter socket path is %d bytes, over the %d-byte sun_path limit: %q",
			len(socketPath), MaxUnixSocketPath, socketPath)
	}
	return nil
}

func validEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("name %q is not a POSIX environment-variable name", name)
		}
	}
	return nil
}
