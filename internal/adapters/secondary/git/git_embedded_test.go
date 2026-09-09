//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"errors"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
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

// TestUpdateCacheEmbedded_RemoteURLOverridesPersistedURL is the regression test
// for the stale-cache defect. A cache cloned by an older bossy has a GitLab CI
// job token baked into the remote URL persisted in its .git/config; the token
// has expired by the next job, so a fetch that trusts the stored URL fails
// with 401/403 and the dependency silently stays at the version job #1 left.
//
// The persisted URL here is simply unreachable rather than expired — the
// mechanism under test is the same: the fetch must use decision.URL and not
// what is on disk. Drop RemoteURL from UpdateCacheEmbedded's FetchOptions and
// this test fails, because the new upstream tag never arrives.
func TestUpdateCacheEmbedded_RemoteURLOverridesPersistedURL(t *testing.T) {
	fixture := buildNativeFixture(t)
	gitEnv := hermeticGitEnv(t)
	t.Setenv("BOSS_HOME", t.TempDir())

	dep := domain.Dependency{Repository: "https://example.com/owner/remote-url-fixture"}
	decision := auth.Decision{URL: fixture.url, Transport: auth.TransportHTTPS}

	if _, err := CloneCacheEmbedded(dep, decision); err != nil {
		t.Fatalf("seed clone: %v", err)
	}

	// Rewrite the persisted remote URL to something that cannot be fetched,
	// standing in for the expired token an older bossy wrote there.
	const deadURL = "file:///bossy-test/no/such/repository"
	repository := GetRepository(dep)
	if repository == nil {
		t.Fatal("GetRepository returned nil after seeding the cache")
	}
	cfg, err := repository.Config()
	if err != nil {
		t.Fatalf("read cache config: %v", err)
	}
	if len(cfg.Remotes) == 0 {
		t.Fatal("seed clone persisted no remote; the fixture cannot exercise the override")
	}
	for _, remote := range cfg.Remotes {
		for i := range remote.URLs {
			remote.URLs[i] = deadURL
		}
	}
	if errSet := repository.SetConfig(cfg); errSet != nil {
		t.Fatalf("write cache config: %v", errSet)
	}

	// Something new to fetch: without it a successful and a skipped fetch are
	// indistinguishable, since the seed clone already brought everything.
	const newTag = "v2.0.0"
	srcDir := fixture.dir
	runGit(t, gitEnv, srcDir, "tag", newTag)
	wantHash := runGit(t, gitEnv, srcDir, "rev-parse", newTag)

	if _, errUpdate := UpdateCacheEmbedded(dep, decision); errUpdate != nil {
		t.Fatalf("UpdateCacheEmbedded: %v", errUpdate)
	}

	ref, err := GetRepository(dep).Tag(newTag)
	if err != nil {
		t.Fatalf("tag %s missing after update: the fetch used the persisted URL, not decision.URL (%v)", newTag, err)
	}
	if ref.Hash().String() != wantHash {
		t.Errorf("tag %s = %s, want %s", newTag, ref.Hash(), wantHash)
	}
}
