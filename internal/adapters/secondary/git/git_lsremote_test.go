//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
)

// nativeFixture describes the on-disk repository built by buildNativeFixture:
// the file:// URL to list refs from, and the object ids git actually assigned,
// keyed by full ref name.
type nativeFixture struct {
	url  string
	refs map[string]string
}

// buildNativeFixture creates a real on-disk git repository using the system git
// binary, with a main branch, a develop branch, a lightweight tag and an
// annotated tag. It returns a file:// URL suitable for use as a clone/ls-remote
// source together with the object ids git assigned.
//
// The fixture is deliberately hermetic. It does not merely override branch
// naming and identity: it also detaches git from the user's global and system
// configuration and clears any inherited repository-selecting variables. A
// global core.hooksPath (which the pre-commit framework installs, and this
// project pins pre-commit) would make `git commit` run an ambient hook, a
// global commit.gpgsign could block on a pinentry prompt, and an ambient
// GIT_DIR would hijack `git init .` so that src/.git never exists.
func buildNativeFixture(t *testing.T) nativeFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}

	src := t.TempDir()
	// A path that does not exist: git >= 2.32 treats an unreadable
	// GIT_CONFIG_GLOBAL/GIT_CONFIG_SYSTEM as "no such config file".
	noConfig := filepath.Join(t.TempDir(), "absent-gitconfig")

	// Drop the repository-selecting variables outright; git treats an empty
	// GIT_DIR as set, so blanking them is not enough.
	env := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE":
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"GIT_CONFIG_GLOBAL="+noConfig,
		"GIT_CONFIG_SYSTEM="+noConfig,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)

	runOut := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = env
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %v\n%s%s", args, err, stdout.String(), stderr.String())
		}
		return strings.TrimSpace(stdout.String())
	}

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	runOut("init", "-q", "-b", "main", ".")
	write("main1")
	runOut("add", ".")
	runOut("commit", "-q", "-m", "main 1")
	runOut("tag", "v1.0.0")
	write("main2")
	runOut("commit", "-q", "-am", "main 2")
	// Annotated, so ls-remote emits both refs/tags/v1.1.0 (the tag object) and
	// refs/tags/v1.1.0^{} (the commit) and the parser's peeled-entry branch is
	// exercised against real git output.
	runOut("tag", "-a", "v1.1.0", "-m", "release 1.1.0")
	runOut("checkout", "-q", "-b", "develop")
	write("dev1")
	runOut("commit", "-q", "-am", "dev 1")
	runOut("checkout", "-q", "main")

	refs := map[string]string{
		"refs/heads/main":    runOut("rev-parse", "main"),
		"refs/heads/develop": runOut("rev-parse", "develop"),
		"refs/tags/v1.0.0":   runOut("rev-parse", "v1.0.0"),
		"refs/tags/v1.1.0":   runOut("rev-parse", "v1.1.0"),
	}

	// file:/// + absolute path. "file://" + path would put a Windows drive
	// letter into the URL authority component.
	url := "file:///" + strings.TrimPrefix(filepath.ToSlash(src), "/")

	return nativeFixture{url: url, refs: refs}
}

// TestListRefsNative_SeesAllBranchesAndTags is the regression test for the
// silent main-branch fallback: a non-default branch must be discoverable
// through the native path. The full ref set is asserted, not a subset, so that
// dropping --heads/--tags or leaking a bare HEAD entry fails the test, and the
// object ids are asserted so that a mangled SHA does too.
func TestListRefsNative_SeesAllBranchesAndTags(t *testing.T) {
	fixture := buildNativeFixture(t)
	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{URL: fixture.url, Transport: auth.TransportSSH}

	refs, err := ListRefsNative(dep, decision)
	if err != nil {
		t.Fatalf("ListRefsNative: %v", err)
	}

	got := map[string]string{}
	for _, r := range refs {
		name := r.Name().String()
		if prev, dup := got[name]; dup {
			t.Fatalf("ref %q returned twice (%s and %s)", name, prev, r.Hash())
		}
		got[name] = r.Hash().String()
	}

	if len(got) != len(fixture.refs) {
		t.Fatalf("ref set mismatch:\n got %v\nwant %v", got, fixture.refs)
	}
	for name, want := range fixture.refs {
		if got[name] != want {
			t.Errorf("ref %q: got %q, want %q", name, got[name], want)
		}
	}

	// v1.1.0 is annotated: the returned SHA must be the tag object's, not the
	// commit the peeled ^{} entry points at.
	if got["refs/tags/v1.1.0"] == got["refs/heads/main"] {
		t.Errorf("annotated tag v1.1.0 resolved to the peeled commit %q; want the tag object",
			got["refs/tags/v1.1.0"])
	}
}

// TestListRefsNative_UnreachableRemote verifies a failing ls-remote surfaces an
// error rather than an empty listing — an empty listing would silently degrade
// to the main-branch fallback, which is the bug this whole change removes.
func TestListRefsNative_UnreachableRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}
	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{
		URL:       "file:///nonexistent/definitely/not/a/repo",
		Transport: auth.TransportSSH,
	}

	refs, err := ListRefsNative(dep, decision)
	if err == nil {
		t.Fatal("expected error for unreachable remote, got nil")
	}
	if refs != nil {
		t.Errorf("refs must be nil on error, got %v", refs)
	}
}

// TestListRefsNative_ErrorRedactsCredentials locks the security property end to
// end: a token embedded in the remote URL (as the gitlab-ci auth layer does)
// must not survive into the returned error, whatever git wrote to stderr.
func TestListRefsNative_ErrorRedactsCredentials(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}
	const secret = "SUPERSECRET123"
	// GIT_TRACE would normally make git echo the full command line, secret
	// included; ListRefsNative must strip it from the inherited environment.
	t.Setenv("GIT_TRACE", "1")

	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{
		URL:       "file://gitlab-ci-token:" + secret + "@/nonexistent/not/a/repo",
		Transport: auth.TransportSSH,
	}

	refs, err := ListRefsNative(dep, decision)
	if err == nil {
		t.Fatal("expected error for unreachable remote, got nil")
	}
	if refs != nil {
		t.Errorf("refs must be nil on error, got %v", refs)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("credential leaked into error: %v", err)
	}
}
