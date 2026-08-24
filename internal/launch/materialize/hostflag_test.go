package materialize_test

import (
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// TestParseHostFlag covers the grammar step 14 wires --host to. The two
// accepted shapes, the absent flag, and the malformed values that must
// refuse rather than be guessed at.
func TestParseHostFlag(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		wantKind     string
		wantInstance string
		wantErr      bool
	}{
		{name: "absent", raw: ""},
		{name: "whitespace only", raw: "   "},
		{name: "kind only", raw: "herdr", wantKind: "herdr"},
		{
			name: "kind and instance", raw: "herdr:/run/user/1000/herdr/api.sock",
			wantKind: "herdr", wantInstance: "/run/user/1000/herdr/api.sock",
		},
		{
			name: "surrounding whitespace", raw: "  herdr:/run/a.sock  ",
			wantKind: "herdr", wantInstance: "/run/a.sock",
		},
		// An instance locator may itself contain a colon, so the cut is at
		// the first one — which is what makes the grammar a true inverse of
		// HostBinding.Locator.
		{
			name: "instance containing a colon", raw: "herdr:host:1234",
			wantKind: "herdr", wantInstance: "host:1234",
		},
		{name: "empty kind", raw: ":/run/a.sock", wantErr: true},
		{name: "empty instance", raw: "herdr:", wantErr: true},
		{name: "whitespace instance", raw: "herdr:   ", wantErr: true},
		{name: "whitespace in kind", raw: "her dr:/run/a.sock", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := materialize.ParseHostFlag(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseHostFlag(%q) = %+v, want an error", tc.raw, got)
				}
				var derr *duoerr.Error
				if !errors.As(err, &derr) {
					t.Fatalf("error is %T, want a *duoerr.Error", err)
				}
				if derr.Code != "invalid.request" {
					t.Errorf("code = %q, want invalid.request", derr.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHostFlag(%q): %v", tc.raw, err)
			}
			if got.Kind != tc.wantKind || got.Instance != tc.wantInstance {
				t.Errorf("ParseHostFlag(%q) = %+v, want kind %q instance %q",
					tc.raw, got, tc.wantKind, tc.wantInstance)
			}
			if got.Present() != (tc.wantKind != "") {
				t.Errorf("Present() = %v", got.Present())
			}
			if got.Complete() != (tc.wantInstance != "") {
				t.Errorf("Complete() = %v", got.Complete())
			}
		})
	}
}

// TestHostFlagRoundTripsALocator: what the audited rebind verb prints is
// what --host takes. A binding's locator parses back to the same pair.
func TestHostFlagRoundTripsALocator(t *testing.T) {
	binding := domain.HostBinding{Kind: "herdr", Instance: "/run/user/1000/herdr/api.sock"}
	got, err := materialize.ParseHostFlag(binding.Locator())
	if err != nil {
		t.Fatalf("ParseHostFlag(%q): %v", binding.Locator(), err)
	}
	if got.Kind != binding.Kind || got.Instance != binding.Instance {
		t.Errorf("round trip = %+v, want %+v", got, binding)
	}
	if got.String() != binding.Locator() {
		t.Errorf("String() = %q, want %q", got.String(), binding.Locator())
	}
}
