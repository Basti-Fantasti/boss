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
	rewritten := 0
	for _, remote := range cfg.Remotes {
		for i := range remote.URLs {
			remote.URLs[i] = deadURL
			rewritten++
		}
	}
	// A remote with no URLs would leave the cache perfectly fetchable and the
	// test green without the override ever being exercised.
	if rewritten == 0 {
		t.Fatal("no remote URL was replaced; the persisted URL is not actually unreachable")
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

// TestScrubTokenFromURL verifies embedded basic-auth credentials are stripped
// from a remote URL while the rest of the URL is preserved.
func TestScrubTokenFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"https://gitlab-ci-token:abc123@gitlab.example.com/group/lib",
			"https://gitlab.example.com/group/lib",
		},
		{
			"https://gitlab.example.com/group/lib",
			"https://gitlab.example.com/group/lib",
		},
		{
			"git@gitlab.example.com:group/lib",
			"git@gitlab.example.com:group/lib",
		},
		{"", ""},
	}
	for _, c := range cases {
		if got := scrubTokenFromURL(c.in); got != c.want {
			t.Errorf("scrubTokenFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestUpdateCacheEmbedded_ScrubsPersistedToken covers the wiring rather than
// the helper: a cache left behind by an older bossy carries the CI job token in
// its persisted remote URL, and a cache update must clear it from disk.
//
// The fetch still succeeds because decision.URL overrides the stored remote, so
// the scrub is observed in isolation from any fetch failure.
func TestUpdateCacheEmbedded_ScrubsPersistedToken(t *testing.T) {
	const secret = "glcbt-EXPIRED-JOB-TOKEN"

	fixture := buildNativeFixture(t)
	t.Setenv("BOSS_HOME", t.TempDir())

	dep := domain.Dependency{Repository: "https://example.com/owner/scrub-fixture"}
	decision := auth.Decision{URL: fixture.url, Transport: auth.TransportHTTPS}

	if _, err := CloneCacheEmbedded(dep, decision); err != nil {
		t.Fatalf("seed clone: %v", err)
	}

	repository := GetRepository(dep)
	if repository == nil {
		t.Fatal("GetRepository returned nil after seeding the cache")
	}
	cfg, err := repository.Config()
	if err != nil {
		t.Fatalf("read cache config: %v", err)
	}
	seeded := 0
	for _, remote := range cfg.Remotes {
		for i := range remote.URLs {
			remote.URLs[i] = "https://gitlab-ci-token:" + secret + "@gitlab.example.com/group/lib"
			seeded++
		}
	}
	if seeded == 0 {
		t.Fatal("seed clone persisted no remote URL; there is no token on disk to scrub")
	}
	if errSet := repository.SetConfig(cfg); errSet != nil {
		t.Fatalf("write cache config: %v", errSet)
	}

	if _, errUpdate := UpdateCacheEmbedded(dep, decision); errUpdate != nil {
		t.Fatalf("UpdateCacheEmbedded: %v", errUpdate)
	}

	// Re-open from disk: the point is that the secret is gone from the file,
	// not merely from the in-memory config the update happened to hold.
	after, err := GetRepository(dep).Config()
	if err != nil {
		t.Fatalf("re-read cache config: %v", err)
	}
	// Both loops below range over what the scrub left behind. If the update ever
	// stopped persisting a remote - or corrupted the config on its way to disk -
	// every body would be skipped and the test would report green without having
	// looked at a single URL, so the inspected count is asserted too.
	inspected := 0
	for name, remote := range after.Remotes {
		for _, u := range remote.URLs {
			inspected++
			if strings.Contains(u, secret) {
				t.Errorf("remote %q still holds the job token: %q", name, u)
			}
			if u != "https://gitlab.example.com/group/lib" {
				t.Errorf("remote %q URL = %q, want the credential stripped and nothing else changed", name, u)
			}
		}
	}
	if inspected != seeded {
		t.Fatalf("inspected %d remote URLs after the scrub, want the %d that were seeded", inspected, seeded)
	}
}
