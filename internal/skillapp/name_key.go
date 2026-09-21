package skillapp

import (
	"strconv"
	"strings"
)

// SkillNameKey is the identity a skill keeps across versions and across the two
// naming conventions in this product: the built-in catalog prefixes template
// names with "tpl-", the community bundle does not. "tpl-grill-me" and
// "grill-me" are the same skill, and treating them as two is what let duplicate
// rows and permanently-uninstallable market cards exist.
func SkillNameKey(name string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "tpl-")
}

// compareSkillVersions orders dotted numeric versions. Non-numeric segments
// count as 0 so a malformed version sorts below any real one instead of
// panicking or winning by string comparison ("10.0.0" < "9.0.0").
func compareSkillVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			if va > vb {
				return 1
			}
			return -1
		}
	}
	return 0
}

func versionParts(v string) []int {
	raw := strings.Split(strings.TrimSpace(v), ".")
	out := make([]int, 0, len(raw))
	for _, part := range raw {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
