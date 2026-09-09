//nolint:testpackage // Testing internal functions
package gitadapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/auth"
	"github.com/basti-fantasti/bossy/pkg/env"
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
// through the native path.
//
// The full ref set is asserted rather than a subset, which catches a mangled
// SHA, a duplicate ref, and an extra or missing ref; the annotated tag pins
// the tag-object-versus-peeled-commit distinction. It does not catch removal
// of the --heads --tags flags, and no output-based fixture could: without them
// ls-remote additionally advertises only HEAD, refs/notes/*, refs/pull/* and
// refs/merge-requests/*, every one of which parseLsRemote's allowlist already
// rejects, so the parsed set is identical either way. The flags are defence in
// depth, not behaviour observable at this layer.
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

// TestListRefsNative_Timeout verifies the deadline actually fires. A listing
// failure is terminal for the install, so an unbounded hang would leave the
// user with no output at all rather than an error - strictly worse than the
// error path.
//
// The ssh transport is replaced by a stand-in that blocks. git runs
// GIT_SSH_COMMAND through a shell as `<cmd> "$@"`, so the trailing "#"
// comments out the host and command arguments git appends and the stand-in
// simply sleeps. The timeout is injected rather than waiting out
// refListTimeout, which keeps a passing run to a fraction of a second.
//
// The elapsed-time assertion is what gives the test teeth, and it only has
// teeth because the stand-in sleeps far longer than the bound: with a one
// second sleep the process ran to completion well inside the bound, so the
// test passed even when the command was detached from its context entirely
// and nothing was ever killed. Keep timeoutSleep >> maxElapsed.
func TestListRefsNative_Timeout(t *testing.T) {
	// The deadline the listing is given, and the wall-clock ceiling a run that
	// honours it must stay under. maxElapsed leaves room for the WaitDelay the
	// implementation applies to the output pipes after killing git; the
	// stand-in outlives it by an order of magnitude, so a run that is not
	// killed cannot slip under it.
	const (
		timeout    = 100 * time.Millisecond
		maxElapsed = 3 * time.Second
	)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}
	t.Setenv("GIT_SSH_COMMAND", "sleep 30 #")

	dep := domain.Dependency{Repository: "example.com/foo/bar"}
	decision := auth.Decision{
		URL:       "git@bossy-test.invalid:foo/bar",
		Transport: auth.TransportSSH,
	}

	start := time.Now()
	refs, err := listRefsNative(dep, decision, timeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the listing exceeds its deadline, got nil")
	}
	if refs != nil {
		t.Errorf("refs must be nil on error, got %v", refs)
	}
	if !strings.Contains(err.Error(), "timed out") {
		if elapsed < timeout {
			// The failure landed before the deadline could have expired, so
			// the stand-in never blocked: no POSIX sleep is reachable from
			// git's shell on this machine and there is nothing to measure.
			t.Skipf("ssh stand-in did not block, deadline not exercised: %v", err)
		}
		t.Fatalf("listing blocked past its deadline but did not report a timeout: %v", err)
	}
	if elapsed > maxElapsed {
		t.Errorf("listing took %s, want under %s: the stand-in sleeps 30s, "+
			"so anything near that means the process was never killed", elapsed, maxElapsed)
	}
}

// TestGetVersions_SSHListingFailureIsAnError is the regression test for the
// design mistake this change exists to prevent. GetVersions must not degrade a
// failed SSH ref listing into an empty slice with a nil error: getVersion
// would then find no match, getReferenceName would see a nil best match, and
// the install would silently check out the main branch instead of the branch
// the user asked for - the exact bug the transport dispatch is meant to fix.
func TestGetVersions_SSHListingFailureIsAnError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}

	// Keep the higher-precedence auth layers out of the way so the dependency
	// resolves through layer 3 (explicit SSH URL) whatever the ambient
	// environment: a GitLab runner sets GITLAB_CI and would rewrite the dep to
	// authenticated HTTPS, taking the embedded path instead.
	t.Setenv("GITLAB_CI", "")
	t.Setenv("BOSSY_AUTH_BOSSY_TEST_INVALID", "")
	// Point the ssh transport at a binary that does not exist, so ls-remote
	// fails immediately without touching the network, DNS, or the developer's
	// ssh configuration.
	t.Setenv("GIT_SSH_COMMAND", "bossy-test-no-such-ssh-binary")

	dep := domain.Dependency{Repository: "git@bossy-test.invalid:no/such-repo"}

	decision, err := auth.Resolve(dep)
	if err != nil {
		t.Fatalf("auth.Resolve: %v", err)
	}
	if decision.Transport != auth.TransportSSH {
		t.Fatalf("dependency must resolve to SSH transport, got %s (layer %q)",
			decision.Transport, decision.Layer)
	}

	// The nil repository is deliberate: the SSH path must not touch the go-git
	// cache at all, so a panic here would itself be a dispatch failure.
	refs, err := GetVersions(env.GlobalConfiguration(), nil, dep)
	if err == nil {
		t.Fatal("expected an error when the SSH ref listing fails, got nil")
	}
	if refs != nil {
		t.Errorf("refs must be nil on error, got %v", refs)
	}
}
