// Package version provides a strict "is newer" comparison for the dotted
// numeric version strings used across the update/anti-rollback paths
// (daemon self-update and plugin manifests). Non-numeric or missing segments
// compare as 0, so empty/garbage versions are never "newer" — callers rely
// on that for fail-closed anti-rollback.
package version

import (
	"strconv"
	"strings"
)

// IsNewer reports whether latest is strictly greater than current.
// Equal or older (or unparseable) ⇒ false.
func IsNewer(latest, current string) bool {
	parse := func(v string) []int {
		parts := make([]int, 0, 3)
		for s := range strings.SplitSeq(v, ".") {
			n, atoiErr := strconv.Atoi(s)
			if atoiErr != nil {
				n = 0
			}
			parts = append(parts, n)
		}
		return parts
	}
	latestParts, currentParts := parse(latest), parse(current)
	for i := 0; i < len(latestParts) || i < len(currentParts); i++ {
		lp, cp := 0, 0
		if i < len(latestParts) {
			lp = latestParts[i]
		}
		if i < len(currentParts) {
			cp = currentParts[i]
		}
		if lp > cp {
			return true
		}
		if lp < cp {
			return false
		}
	}
	return false
}
