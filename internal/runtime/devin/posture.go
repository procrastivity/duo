package devin

import (
	"encoding/json"
	"fmt"
	"os"
)

// DuoExecAllowRules is the curated exec allow-list Duo projects into a launch
// workspace's .devin/config.local.json so a print-mode builder sitting can run
// its ordinary commands without a permission rejection. Devin's Exec rules
// match token-wise prefixes with no glob support (live-verified on
// 3000.10.21, matter duo-devin-exec-posture step-01), so each entry names a
// command and its fixed leading arguments.
//
// The posture is additive-only: the file can grant but not fence — a
// user-level allow beat a local deny in the probe, so nothing here pretends
// to constrain the operator's own config. What the list deliberately leaves
// out: publishing/history-destroying git verbs (push, pull, rebase, reset,
// clean, remote), rm/sudo and other unscoped destructives, bare package
// installs, blanket Exec(git)/Exec(gh)/Exec(devin), wip's human-boundary and
// repo-config verbs (outbox approve/flush/retry, backlog delegate, init,
// install, gate, dispatch close), and duo's orchestration verbs (launch,
// prompt send, proc stop/restart/kill). Widening any of those stays an
// operator choice in their own config.
var DuoExecAllowRules = []string{
	// git: read plus local-commit working set — never push/pull/rebase/
	// reset/clean/remote/cherry-pick.
	"Exec(git status)",
	"Exec(git diff)",
	"Exec(git log)",
	"Exec(git show)",
	"Exec(git grep)",
	"Exec(git blame)",
	"Exec(git branch)",
	"Exec(git add)",
	"Exec(git commit)",
	"Exec(git checkout)",
	"Exec(git switch)",
	"Exec(git restore)",
	"Exec(git stash)",
	"Exec(git fetch)",
	// filesystem: inspect plus in-workspace create/move. No rm — token
	// prefix rules cannot scope it to the workspace, and a builder in a
	// disposable workspace does not need it.
	"Exec(ls)",
	"Exec(cat)",
	"Exec(head)",
	"Exec(tail)",
	"Exec(grep)",
	"Exec(find)",
	"Exec(wc)",
	"Exec(pwd)",
	"Exec(echo)",
	"Exec(sort)",
	"Exec(diff)",
	"Exec(file)",
	"Exec(which)",
	"Exec(mkdir)",
	"Exec(touch)",
	"Exec(cp)",
	"Exec(mv)",
	"Exec(rmdir)",
	"Exec(sed)",
	"Exec(awk)",
	"Exec(chmod)",
	"Exec(tar)",
	// toolchain: build/test/lint for the languages Duo dogfoods. Env
	// assignments are token 0 to Devin's matcher, so the CGO-prefixed
	// build needs its own entry.
	"Exec(go build)",
	"Exec(go test)",
	"Exec(go vet)",
	"Exec(go fmt)",
	"Exec(go run)",
	"Exec(go mod)",
	"Exec(go version)",
	"Exec(go env)",
	"Exec(CGO_ENABLED=0 go build)",
	"Exec(gofmt)",
	"Exec(gofumpt)",
	"Exec(golangci-lint run)",
	"Exec(make check)",
	"Exec(make build)",
	"Exec(make test)",
	"Exec(make lint)",
	"Exec(make fmt)",
	"Exec(npm run)",
	"Exec(npm test)",
	"Exec(npm exec)",
	"Exec(node)",
	"Exec(npx)",
	"Exec(python3)",
	"Exec(python)",
	"Exec(uv run)",
	"Exec(uv sync)",
	// wip: the verbs a builder sitting uses on its own work. The human
	// boundary (outbox approve/flush/retry, backlog delegate) and
	// repo-config verbs (init, install, outbox backend/target/level,
	// gate, label, dispatch close, clean, rebind, unbind) stay out.
	"Exec(wip status)",
	"Exec(wip next)",
	"Exec(wip session)",
	"Exec(wip finding)",
	"Exec(wip start)",
	"Exec(wip finish)",
	"Exec(wip pause)",
	"Exec(wip resume)",
	"Exec(wip cancel)",
	"Exec(wip step)",
	"Exec(wip stage)",
	"Exec(wip workplan)",
	"Exec(wip brief)",
	"Exec(wip body)",
	"Exec(wip refresh)",
	"Exec(wip depend)",
	"Exec(wip backlog list)",
	"Exec(wip backlog add)",
	"Exec(wip backlog plan)",
	"Exec(wip doctor)",
	"Exec(wip manifest)",
	"Exec(wip version)",
	"Exec(wip run list)",
	"Exec(wip run show)",
	"Exec(wip role list)",
	"Exec(wip role active)",
	"Exec(wip outbox list)",
	"Exec(wip clone list)",
	// duo: read-side self-inspection only — launch, prompt send, and
	// proc stop/restart/kill are orchestration actuation.
	"Exec(duo version)",
	"Exec(duo whoami)",
	"Exec(duo doctor)",
	"Exec(duo config)",
	"Exec(duo agent list)",
	"Exec(duo agent resolve)",
	"Exec(duo proc ls)",
	"Exec(duo proc logs)",
	"Exec(duo proc status)",
	"Exec(duo session list)",
	"Exec(duo session show)",
	// gh: read verbs only — no blanket Exec(gh) (it covers pr create and
	// merge) and no gh api (method flags decide mutation).
	"Exec(gh pr view)",
	"Exec(gh pr diff)",
	"Exec(gh pr checks)",
	"Exec(gh pr list)",
	"Exec(gh issue view)",
	"Exec(gh issue list)",
	"Exec(gh run view)",
	"Exec(gh run list)",
	"Exec(gh repo view)",
}

// renderPostureConfig encodes the Duo-owned .devin/config.local.json. Devin
// reads it once at process start and merges it above the project and user
// configs; print mode turns any unmatched exec into a rejection, so this
// file is the whole of Duo's exec posture for a builder sitting.
func renderPostureConfig() ([]byte, error) {
	config := map[string]any{
		"permissions": map[string]any{
			"allow": DuoExecAllowRules,
		},
	}
	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("devin: encoding the exec posture projection: %w", err)
	}
	return b, nil
}

// validatePostureConfig is the semantic companion to the stamp digests: the
// on-disk posture file must decode and carry exactly Duo's allow rules —
// nothing broader (a blanket Exec(git)) and nothing narrower.
func validatePostureConfig(posturePath string) ProjectionStatus {
	b, err := os.ReadFile(posturePath)
	if err != nil {
		return ProjectionModified
	}
	var config struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(b, &config); err != nil {
		return ProjectionIncompatible
	}
	if len(config.Permissions.Allow) != len(DuoExecAllowRules) {
		return ProjectionIncompatible
	}
	for i, rule := range DuoExecAllowRules {
		if config.Permissions.Allow[i] != rule {
			return ProjectionIncompatible
		}
	}
	return ProjectionCurrent
}
