package domain

import "regexp"

var gitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// IsGitSHA reports whether s is a valid abbreviated or full git SHA-1
// (7–40 lowercase hex chars). Git itself accepts shorter prefixes when
// unambiguous, but 7 is the conventional minimum.
func IsGitSHA(s string) bool {
	return gitSHAPattern.MatchString(s)
}
