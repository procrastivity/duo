package opencode

import (
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/duoerr"
)

func TestValidatePluginSpecs(t *testing.T) {
	tests := []struct {
		name    string
		specs   []string
		wantErr bool
	}{
		{name: "nil list", specs: nil},
		{name: "empty list", specs: []string{}},
		{
			name:  "plain specs",
			specs: []string{"file://./modules/a", "some-plugin@1.2.3", "./local/plugin.ts"},
		},
		{
			name:  "exact-ID removal",
			specs: []string{"file://./modules/a", "-duo.example.target"},
		},
		{
			name:    "bare wildcard",
			specs:   []string{"-*"},
			wantErr: true,
		},
		{
			// The verified case: a high-priority "-*" sweeps earlier
			// siblings, so trailing position does not make it safe.
			name:    "wildcard after sibling",
			specs:   []string{"file://./modules/sibling", "-*"},
			wantErr: true,
		},
		{
			// A re-add after the wildcard keeps that same-document
			// sibling alive in the earlier capture, but the wildcard
			// still sweeps every lower-priority document the projection
			// cannot enumerate — still refused.
			name:    "wildcard with re-add",
			specs:   []string{"-*", "file://./modules/sibling"},
			wantErr: true,
		},
		{
			name:    "prefix glob removal",
			specs:   []string{"-duo.group.*"},
			wantErr: true,
		},
		{
			name:    "mid-selector glob",
			specs:   []string{"-duo.*.target"},
			wantErr: true,
		},
		{
			name:    "empty selector",
			specs:   []string{"-"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePluginSpecs(tt.specs)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidatePluginSpecs(%v) = %v, want nil", tt.specs, err)
				}
				return
			}
			var derr *duoerr.Error
			if !errors.As(err, &derr) {
				t.Fatalf("ValidatePluginSpecs(%v) returned a non-duoerr error: %v", tt.specs, err)
			}
			if derr.Code != ErrCodePluginWildcardRemoval {
				t.Errorf("error code = %q, want %q", derr.Code, ErrCodePluginWildcardRemoval)
			}
		})
	}
}
