// Package skills embeds the canonical authored skill tree for bare-binary
// installs. The embedded bytes come directly from the normative source; there
// is no generated or separately edited copy under assets/.
package skills

import "embed"

// FS contains the canonical skills shipped by Duo.
//
//go:embed duo-delegation-loop/SKILL.md
var FS embed.FS
