package domain

import "testing"

func TestParseProcessBirthValueRoundTrip(t *testing.T) {
	cases := []ProcessBirth{
		{},
		{PID: 41210, StartedAt: "2026-08-24T18:31:07.412Z"},
		{PID: 41210, StartedAt: "2026-08-24T18:31:07.412Z", Host: "boot-abc"},
		{PID: 41210, StartedAt: "2026-08-24T18:31:07.412Z", Executable: "/usr/bin/claude"},
		{PID: 41210, StartedAt: "2026-08-24T18:31:07.412Z", Host: "boot-abc", Executable: "/usr/bin/claude"},
		{PID: 41210, StartedAt: "2026-08-24T18:31:07.412Z", Executable: "/opt/Claude Code/claude"},
	}
	for _, want := range cases {
		got := parseProcessBirthValue(processBirthValue(want))
		if got != want {
			t.Errorf("parse(processBirthValue(%+v)) = %+v", want, got)
		}
	}
	if got := parseProcessBirthValue(""); got != (ProcessBirth{}) {
		t.Errorf("empty string parsed as %+v, want zero (unknown)", got)
	}
	if got := parseProcessBirthValue("not-a-birth"); got != (ProcessBirth{}) {
		t.Errorf("unknown string parsed as %+v, want zero", got)
	}
}
