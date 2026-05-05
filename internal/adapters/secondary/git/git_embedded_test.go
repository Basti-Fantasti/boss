//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"errors"
	"strings"
	"testing"
)

func TestIsCIPermissionError(t *testing.T) {
	tests := []struct {
		name   string
		gitlab string
		err    error
		want   bool
	}{
		{"403 in CI", "true", errors.New("authentication required: 403 Forbidden"), true},
		{"403 outside CI", "", errors.New("403 Forbidden"), false},
		{"non-permission error in CI", "true", errors.New("connection timeout"), false},
		{"nil error", "true", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITLAB_CI", tt.gitlab)
			if got := isCIPermissionError(tt.err); got != tt.want {
				t.Errorf("isCIPermissionError = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCIJobTokenErrorWording(t *testing.T) {
	cause := errors.New("403 Forbidden")
	got := ciJobTokenError("gitlab.mydomain.com/foo/bar", cause).Error()
	for _, want := range []string{
		"CI_JOB_TOKEN denied access to gitlab.mydomain.com/foo/bar",
		"CI/CD job-token allowlist",
		"Settings → CI/CD → Job token permissions",
		"docs/ci.md",
		"403 Forbidden",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error missing substring %q\nfull: %s", want, got)
		}
	}
}
