package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/manifest"
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/surface"
)

const portableInstallOperation = "projection.install"

func installCommand(streams *iostreams.Streams, build buildinfo.Info, root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "install product-owned workspace projections",
	}
	cmd.AddCommand(portableLaunchersInstallCommand(streams, build, root))
	return cmd
}

func portableLaunchersInstallCommand(streams *iostreams.Streams, build buildinfo.Info, root *cobra.Command) *cobra.Command {
	var workspace string
	var repair bool
	cmd := &cobra.Command{
		Use:   "portable-launchers",
		Short: "install the shared Amp, OpenCode, and Codex filesystem skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := manifest.Build(root, build)
			if err != nil {
				return duoerr.New("internal.manifest_build_failed", fmt.Sprintf("building the installation manifest: %v", err))
			}
			result, err := manifest.InstallPortableLaunchers(workspace, repair, m)
			if err != nil {
				var projectionErr *manifest.ProjectionError
				if errors.As(err, &projectionErr) {
					return writePortableInstallFailure(streams, cliflags.FromContext(cmd.Context()).Output, projectionErr)
				}
				return duoerr.New("internal.projection_install_failed", err.Error())
			}

			if cliflags.FromContext(cmd.Context()).JSON() {
				b, err := json.Marshal(newEnvelope(portableInstallOperation, result))
				if err != nil {
					return duoerr.New("internal.projection_install_failed", fmt.Sprintf("encoding installation result: %v", err))
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			_, err = fmt.Fprintf(streams.Out, "portable-launchers: %s (changed: %t)\nroot: %s\nidentity: %s@%s\ninstallation: %s\n",
				result.State, result.Changed, result.Root, result.FormatVersion, result.ContentDigest, result.InstallationID)
			return err
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace root (default: current directory)")
	cmd.Flags().BoolVar(&repair, "repair", false, "repair an owned missing or stale projection")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

type installRetryAdvice struct {
	Safe   bool   `json:"safe"`
	Action string `json:"action"`
}

func writePortableInstallFailure(streams *iostreams.Streams, mode string, failure *manifest.ProjectionError) error {
	if mode != cliflags.OutputJSON {
		return duoerr.New(failure.Code, failure.Message)
	}
	action := map[string]string{
		"invalid.precondition":          "inspect_and_retry_with_repair",
		"projection.modified":           "review_modified_projection",
		"projection.incompatible":       "upgrade_or_move_projection",
		"projection.user_file_conflict": "move_unowned_content",
	}[failure.Code]
	if action == "" {
		action = "inspect_projection"
	}
	class := "internal"
	if registered, ok := registry.StableErrorCodes()[failure.Code]; ok {
		class = string(registered)
	}
	envelope := struct {
		Schema    string `json:"schema"`
		RequestID string `json:"request_id"`
		Operation string `json:"operation"`
		Error     struct {
			Class   string             `json:"class"`
			Code    string             `json:"code"`
			Message string             `json:"message"`
			Retry   installRetryAdvice `json:"retry"`
			Effect  string             `json:"effect"`
		} `json:"error"`
	}{
		Schema:    "duo.external/v1",
		RequestID: requestID(),
		Operation: portableInstallOperation,
	}
	envelope.Error.Class = class
	envelope.Error.Code = failure.Code
	envelope.Error.Message = failure.Message
	envelope.Error.Retry = installRetryAdvice{Safe: false, Action: action}
	envelope.Error.Effect = "no_effect"
	b, err := json.Marshal(envelope)
	if err != nil {
		return duoerr.New("internal.projection_install_failed", fmt.Sprintf("encoding installation failure: %v", err))
	}
	if _, err := fmt.Fprintln(streams.Err, string(b)); err != nil {
		return err
	}
	return &failureWrittenError{code: failure.Code}
}
