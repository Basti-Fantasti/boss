//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
)

// buildNativeFixture creates a real on-disk git repository using the system git
// binary, with a main branch, a develop branch, and two tags. It returns a
// file:// URL suitable for use as a clone/ls-remote source.
func buildNativeFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}

	src := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	run("init", "-q", "-b", "main", ".")
	write("main1")
	run("add", ".")
	run("commit", "-q", "-m", "main 1")
	run("tag", "v1.0.0")
	write("main2")
	run("commit", "-q", "-am", "main 2")
	run("tag", "v1.1.0")
	run("checkout", "-q", "-b", "develop")
	write("dev1")
	run("commit", "-q", "-am", "dev 1")
	run("checkout", "-q", "main")

	return "file://" + filepath.ToSlash(src)
}

// TestListRefsNative_SeesAllBranchesAndTags is the regression test for the
// silent main-branch fallback: a non-default branch must be discoverable
// through the native path.
func TestListRefsNative_SeesAllBranchesAndTags(t *testing.T) {
	url := buildNativeFixture(t)
	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{URL: url, Transport: auth.TransportSSH}

	refs, err := ListRefsNative(dep, decision)
	if err != nil {
		t.Fatalf("ListRefsNative: %v", err)
	}

	names := map[string]bool{}
	for _, r := range refs {
		names[r.Name().Short()] = true
	}
	for _, want := range []string{"main", "develop", "v1.0.0", "v1.1.0"} {
		if !names[want] {
			t.Errorf("ref %q missing from listing; got %v", want, names)
		}
	}
}

// TestListRefsNative_BadURL verifies a failing ls-remote surfaces an error
// rather than an empty listing — an empty listing would silently degrade to
// the main-branch fallback, which is the bug this whole change removes.
func TestListRefsNative_BadURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}
	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{
		URL:       "file:///nonexistent/definitely/not/a/repo",
		Transport: auth.TransportSSH,
	}

	if _, err := ListRefsNative(dep, decision); err == nil {
		t.Fatal("expected error for unreachable remote, got nil")
	}
}
