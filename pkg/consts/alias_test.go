package consts_test

import (
	"testing"

	"github.com/basti-fantasti/bossy/pkg/consts"
)

func TestAliasNamePattern(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain", "gtr", true},
		{"with hyphen", "my-org", true},
		{"with underscore and digit", "org_2", true},
		{"capital letter rejected", "Gtr", false},
		{"digit start rejected", "2org", false},
		{"dot rejected", "gtr.de", false},
		{"colon rejected", "gtr:", false},
		{"slash rejected", "gtr/foo", false},
		{"empty string rejected", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := consts.AliasNamePattern.MatchString(c.input); got != c.want {
				t.Errorf("AliasNamePattern.MatchString(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}
