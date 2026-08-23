package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// manifestDigest computes the manifest_digest field: sha256 over m's own
// canonical JSON encoding with manifest_digest itself held empty, hex-
// encoded with a "sha256:" prefix — the same digest shape Checksum uses for
// one asset file, applied here to the whole document. Build calls this only
// after every other field is set, so the digest commits to the exact
// document a caller receives (minus the digest field, which cannot commit
// to itself).
//
// Determinism depends on every slice Build assembles already having a fixed
// order before this runs: registry.ManifestOperations() and walkAssets()
// both return in a stable order (table order and sorted-by-path,
// respectively), and encoding/json renders Go struct fields in their
// declared order, not map order — so two Build calls against the same
// binary and command tree always produce the same digest.
func manifestDigest(m Manifest) (string, error) {
	m.ManifestDigest = ""
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("manifest: encoding for digest: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
