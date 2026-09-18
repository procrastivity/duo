package doctor

// LauncherPreflightSchema is the stable launcher-readiness report version.
const LauncherPreflightSchema = "duo.launcher-preflight/v1"

// Stable launcher-preflight check IDs in required report order.
const (
	CheckDuoExecutable     = "duo_executable"
	CheckEffectiveConfig   = "effective_config"
	CheckAuthorityStore    = "authority_store"
	CheckWorkspace         = "workspace"
	CheckHostSelection     = "host_selection"
	CheckHostReachability  = "host_reachability"
	CheckSkillProjection   = "skill_projection"
	CheckHostCompatibility = "host_compatibility"
)

// Check is one closed launcher-preflight row. Action is empty only for a
// passing check or an informational check that was not reached.
type Check struct {
	ID       string `json:"id"`
	Stage    string `json:"stage"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Code     string `json:"code"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
}

// DuoIdentity is the exact running executable and compiled build triple.
type DuoIdentity struct {
	ExecutablePath string `json:"executable_path"`
	Version        string `json:"version"`
	Commit         string `json:"commit"`
	BuildDate      string `json:"build_date"`
}

// EffectiveConfig is the selected validated config's non-secret identity.
type EffectiveConfig struct {
	Path            string   `json:"path"`
	Schema          string   `json:"schema"`
	EffectiveDigest string   `json:"effective_digest"`
	Valid           bool     `json:"valid"`
	Presets         []string `json:"presets"`
}

// AuthorityPreflight is the read-only local authority-store diagnosis.
type AuthorityPreflight struct {
	Path          string `json:"path"`
	State         string `json:"state"`
	Present       bool   `json:"present"`
	Healthy       bool   `json:"healthy"`
	SchemaVersion int    `json:"schema_version"`
	WriterActive  bool   `json:"writer_active"`
}

// WorkspacePreflight is launch's selected workspace and its local shape.
type WorkspacePreflight struct {
	RequestedPath string `json:"requested_path"`
	SelectedPath  string `json:"selected_path"`
	Source        string `json:"source"`
	Exists        bool   `json:"exists"`
	Directory     bool   `json:"directory"`
}

// HostPreflight is M1's selected host plus bounded probe evidence.
type HostPreflight struct {
	Selected         bool   `json:"selected"`
	Kind             string `json:"kind"`
	InstanceID       string `json:"instance_id"`
	InstanceLabel    string `json:"instance_label"`
	HostSource       string `json:"host_source"`
	Reachable        bool   `json:"reachable"`
	DetectedVersion  string `json:"detected_version"`
	ProtocolIdentity string `json:"protocol_identity"`
	Compatibility    string `json:"compatibility"`
}

// SkillProjection is the copied portable launcher's full artifact identity.
type SkillProjection struct {
	Target         string `json:"target"`
	Name           string `json:"name"`
	FormatVersion  string `json:"format_version"`
	ContentDigest  string `json:"content_digest"`
	Root           string `json:"root"`
	File           string `json:"file"`
	Stamp          string `json:"stamp"`
	State          string `json:"state"`
	InstallationID string `json:"installation_id"`
}

// LauncherPreflight is the stable top-level launcher readiness object.
// Every field is deliberately non-omitempty: unavailable evidence is
// represented by the field's zero value, never by changing the shape.
type LauncherPreflight struct {
	Schema          string             `json:"schema"`
	Status          string             `json:"status"`
	AuthorityScope  string             `json:"authority_scope"`
	Checks          []Check            `json:"checks"`
	Duo             DuoIdentity        `json:"duo"`
	Config          EffectiveConfig    `json:"config"`
	Authority       AuthorityPreflight `json:"authority"`
	Workspace       WorkspacePreflight `json:"workspace"`
	Host            HostPreflight      `json:"host"`
	SkillProjection SkillProjection    `json:"skill_projection"`
}

// NewLauncherPreflight returns the closed eight-row check list in contract
// order. Callers update rows with SetCheck and then call Finalize.
func NewLauncherPreflight() LauncherPreflight {
	return LauncherPreflight{
		Schema:         LauncherPreflightSchema,
		Status:         "not_ready",
		AuthorityScope: "unavailable",
		Checks: []Check{
			{ID: CheckDuoExecutable, Stage: "executable", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckEffectiveConfig, Stage: "config", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckAuthorityStore, Stage: "authority", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckWorkspace, Stage: "workspace", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckHostSelection, Stage: "host_selection", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckHostReachability, Stage: "host_reachability", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckSkillProjection, Stage: "skill_projection", Required: true, Status: "not_checked", Code: "prerequisite.not_reached"},
			{ID: CheckHostCompatibility, Stage: "host_reachability", Required: false, Status: "not_checked", Code: "prerequisite.not_reached"},
		},
		Config: EffectiveConfig{Presets: []string{}},
		Host:   HostPreflight{Compatibility: "unknown"},
	}
}

// SetCheck replaces one existing row without changing order or allowing the
// closed list to grow. It returns false only for a programmer error (unknown
// ID), which lets callers turn that into an internal command failure.
func (p *LauncherPreflight) SetCheck(id, status, code, summary, action string) bool {
	for i := range p.Checks {
		if p.Checks[i].ID != id {
			continue
		}
		p.Checks[i].Status = status
		p.Checks[i].Code = code
		p.Checks[i].Summary = summary
		p.Checks[i].Action = action
		return true
	}
	return false
}

// Finalize derives readiness only from the seven required checks. Host
// compatibility is informational and may warn without refusing launch.
// authority_scope is independent of executable identity and an active writer:
// it is local when the exact config/store/workspace/projection resources are
// usable and the selected host answers. An active writer still makes the
// launch not ready, but does not make those resources remote.
func (p *LauncherPreflight) Finalize() {
	p.Status = "ready"
	for _, check := range p.Checks {
		if check.Required && check.Status != "pass" {
			p.Status = "not_ready"
			break
		}
	}
	p.AuthorityScope = "unavailable"
	authorityLocal := checkStatus(p.Checks, CheckAuthorityStore) == "pass" || p.Authority.State == "writer_active"
	if checkStatus(p.Checks, CheckEffectiveConfig) == "pass" && authorityLocal &&
		checkStatus(p.Checks, CheckWorkspace) == "pass" && checkStatus(p.Checks, CheckHostSelection) == "pass" &&
		checkStatus(p.Checks, CheckHostReachability) == "pass" && checkStatus(p.Checks, CheckSkillProjection) == "pass" {
		p.AuthorityScope = "local"
	}
}

func checkStatus(checks []Check, id string) string {
	for _, check := range checks {
		if check.ID == id {
			return check.Status
		}
	}
	return ""
}
