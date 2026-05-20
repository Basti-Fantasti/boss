package domain_test

import (
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

func TestIsGitSHA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"abcdef", false}, // 6 chars: too short
		{"abcdef0", true}, // 7 chars: minimum
		{"abcdef0123456789abcdef0123456789abcdef01", true},    // 40 chars: full
		{"abcdef0123456789abcdef0123456789abcdef0123", false}, // 42 chars: too long
		{"ABCDEF0", false}, // uppercase: reject
		{"abcdefg", false}, // non-hex
		{"v1.2.3", false},  // semver
		{"^1.2.0", false},  // constraint
		{"main", false},    // branch
	}
	for _, c := range cases {
		if got := domain.IsGitSHA(c.in); got != c.want {
			t.Errorf("IsGitSHA(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
