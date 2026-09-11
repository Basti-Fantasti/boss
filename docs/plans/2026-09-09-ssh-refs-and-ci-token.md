# SSH Ref Discovery and CI Token Handling Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make branch-pinned dependencies resolve correctly over SSH and under shallow clone, and stop the GitLab CI job token from being persisted into the dependency cache where it goes stale and leaks.

**Architecture:** Three independent defects, all in the secondary git adapter and the auth layer. (A1) `GetVersions` is the only git operation that never dispatches on `auth.Decision.Transport`; give it the same two-branch dispatch the others have, with a native `git ls-remote` listing path. (A2) Shallow clone passes `--single-branch`, which writes a restricted remote refspec that makes every non-default branch permanently unreachable; drop it and repair refspecs on existing caches. (B) `tryGitLabCI` bakes the job token into the URL string, which go-git then persists into the cache's `.git/config`; move the token into `CredentialSpec` and override `RemoteURL` on every fetch.

**Tech Stack:** Go 1.24, go-git v5 (embedded HTTPS transport), system `git` binary (native SSH transport), cobra, pterm.

**Reference:** Design agreed in session; findings verified manually against a `file://` fixture repo on 2026-09-09.

---

## Background: why each change is needed

Read this before starting. The failure modes are silent, so tests that only assert "no error" will pass against the broken code.

**A1.** `internal/adapters/secondary/git/git.go:78` `GetVersions` always calls go-git's `repository.Fetch`. For a dependency resolved to `TransportSSH`, that uses go-git's own SSH stack, which does **not** parse `~/.ssh/config` and enforces strict `known_hosts`. On failure it emits `msg.Warn` and continues. `getVersion` then finds no matching ref, and `internal/core/services/installer/core.go:511` falls back to main/master with a warning. Net effect: the user asks for branch `develop` and silently gets `main`.

Verified structurally: after a native `git clone`, only `refs/heads/main` exists locally. `develop` exists solely as `refs/remotes/origin/develop`. go-git's `repository.Branches()` reads `refs/heads/*`, so it cannot see `develop` at all unless its own fetch succeeds.

**A2.** `doClone` (`git_native.go`) and `CloneCacheEmbedded` (`git_embedded.go`) both pass single-branch when `env.GetGitShallow()` is on. Measured behaviour of `git clone --depth 1 --single-branch`:

- remote refspec is written as `+refs/heads/main:refs/remotes/origin/main`
- `git fetch --all` brings nothing new — `develop` never arrives, exit 0
- `git fetch --unshallow` does **not** recover it — exit 0, zero `develop` refs. The existing `UnshallowFetch` recovery path deepens history for `main` only; it never widens the refspec
- `git checkout -f develop` → exit 1, `pathspec 'develop' did not match`

In bossy this never reaches the checkout: `GetVersions` returns only `main` plus tags, so resolution silently falls back to main. This affects HTTPS dependencies too, not just SSH.

Verified fix: `--depth 1 --no-single-branch` keeps the repo shallow (`git rev-parse --is-shallow-repository` → `true`, 84 KB on the fixture) while leaving the refspec at `+refs/heads/*:refs/remotes/origin/*`. All branch tips present, `checkout develop` exits 0.

**B.** `tryGitLabCI` (`internal/core/services/auth/gitlab_ci.go`) returns `URL: "https://gitlab-ci-token:<token>@host/path"` with an empty `CredentialSpec`. Two consequences:

1. go-git persists `CloneOptions.URL` into the cache's `.git/config`. On a **shell runner** — which is what the target build server uses (`C:/tools/shell-runner/builds/...`) — `~/.bossy/cache` survives between jobs, so job #2 fetches using job #1's expired token. `UpdateCacheEmbedded`, `GetVersions`, `UnshallowFetchEmbedded` and `PullEmbedded` all call fetch without `RemoteURL`, and `httpsAuth` returns `nil` because `CredentialSpec` is empty, so there is no override. Result: 401/403 on every job after the first.
2. The token is written to disk on a shared build machine.

`RemoteURL` is confirmed present in go-git v5's `FetchOptions` and `PullOptions`.

---

## Pre-flight

1. Confirm `make test` is green on `bossy-fork` before starting. If it fails, fix or stash uncommitted work first.
2. Confirm the `git` binary is on PATH: `git --version`. Several tasks add tests that shell out to it.
3. **Lint requires the pinned toolchain.** `mise` is not installed on this machine and Go 1.27 is on PATH. `GOTOOLCHAIN=auto` only ever switches *up*, so the `toolchain go1.24.1` directive in `go.mod` is ignored and golangci-lint v1.64.8 fails with `export data version 4 is greater than maximum supported version 2`. Prefix every lint invocation with the pin:

   ```bash
   GOTOOLCHAIN=go1.24.1 go tool golangci-lint run ./path/...
   ```

   Verified working (exit 0). Plain `go build` and `go test` are fine under Go 1.27 and need no prefix.
4. Skill prerequisites: use the `git-config` skill before every commit; use `superpowers:verification-before-completion` before claiming any task done.
5. Read the Background section above end-to-end.

---

## Task 1: Add `parseLsRemote` ref parser

Pure string parsing, no I/O. Split out from the command runner so it can be tested without a repo.

**Files:**
- Modify: `internal/adapters/secondary/git/git_native.go`
- Test: `internal/adapters/secondary/git/git_native_test.go`

**Step 1: Write the failing test**

Append to `internal/adapters/secondary/git/git_native_test.go` (note this file uses the internal `gitadapter` package, so unexported functions are directly callable):

```go
// TestParseLsRemote verifies that ls-remote output is parsed into references
// whose Short() names match what installer.getVersion compares against, and
// that peeled tag entries are discarded.
func TestParseLsRemote(t *testing.T) {
	out := "" +
		"81a517be28652c5c25656102a7cf4d592450beeb\trefs/heads/develop\n" +
		"c2b107ffbdc3246b1cb2696b722b39616621c878\trefs/heads/main\n" +
		"026988d4bf23b80ea67639de88b5e007a95831ee\trefs/tags/v1.0.0\n" +
		"c2b107ffbdc3246b1cb2696b722b39616621c878\trefs/tags/v1.1.0\n" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/tags/v1.1.0^{}\n"

	refs := parseLsRemote(out)

	if len(refs) != 4 {
		t.Fatalf("got %d refs, want 4 (peeled tag must be skipped)", len(refs))
	}

	got := map[string]string{}
	for _, r := range refs {
		got[r.Name().Short()] = r.Hash().String()
	}

	// Short() must yield "develop", not "origin/develop" — installer.getVersion
	// compares against the bare branch name.
	if got["develop"] != "81a517be28652c5c25656102a7cf4d592450beeb" {
		t.Errorf("develop: got %q", got["develop"])
	}
	if got["v1.0.0"] != "026988d4bf23b80ea67639de88b5e007a95831ee" {
		t.Errorf("v1.0.0: got %q", got["v1.0.0"])
	}
	if _, ok := got["main"]; !ok {
		t.Error("main ref missing")
	}
}

// TestParseLsRemote_Garbage verifies malformed lines are skipped, not fatal.
func TestParseLsRemote_Garbage(t *testing.T) {
	out := "not-a-ref-line\n\n   \nc2b107ffbdc3246b1cb2696b722b39616621c878\trefs/heads/main\n"
	refs := parseLsRemote(out)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if refs[0].Name().Short() != "main" {
		t.Errorf("got %q, want main", refs[0].Name().Short())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/secondary/git -run TestParseLsRemote -v`
Expected: FAIL — `undefined: parseLsRemote`

**Step 3: Write minimal implementation**

Add to `internal/adapters/secondary/git/git_native.go`:

```go
// parseLsRemote turns `git ls-remote` output ("<sha>\t<refname>" per line)
// into plumbing references. Peeled annotated-tag entries ("refs/tags/x^{}")
// are skipped: the tag object itself is the ref bossy resolves against.
// Malformed lines are skipped rather than treated as fatal — a partially
// parseable listing is more useful than none.
func parseLsRemote(out string) []*plumbing.Reference {
	refs := make([]*plumbing.Reference, 0)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		sha, name := fields[0], fields[1]
		if strings.HasSuffix(name, "^{}") {
			continue
		}
		refs = append(refs, plumbing.NewReferenceFromStrings(name, sha))
	}
	return refs
}
```

`strings` and `plumbing` are already imported in this file. Verify before adding imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/secondary/git -run TestParseLsRemote -v`
Expected: PASS (both tests)

**Step 5: Commit**

```bash
git add internal/adapters/secondary/git/git_native.go internal/adapters/secondary/git/git_native_test.go
git commit -m "feat(git): add parseLsRemote for native ref listing"
```

---

## Task 2: Add `ListRefsNative`

**Files:**
- Modify: `internal/adapters/secondary/git/git_native.go`
- Test: Create `internal/adapters/secondary/git/git_lsremote_test.go`

**Design note:** `ListRefsNative` passes the URL to `ls-remote` explicitly, so it needs no working directory, no `.git` pointer file, and no prior clone. This is deliberately simpler than the other native helpers. Do **not** use `runCommand` — it discards stdout into a debug log (`git_native.go:248-266`); this function needs stdout captured.

### Task 2a: Harden `parseLsRemote` first

Three defects were found reviewing Task 1. All three live in `parseLsRemote` and all three are cheap to close. Do this **before** adding `ListRefsNative`, as a separate commit, so the parser's contract is sound before it acquires a caller.

The unifying problem: the doc comment promises malformed lines are skipped, but the only check is `len(fields) != 2`. Everything else is accepted.

**(a) The SHA field is never validated.** `plumbing.NewReferenceFromStrings` calls `plumbing.NewHash`, which does `b, _ := hex.DecodeString(s)` — discarding the error and zero-padding. Verified against go-git v5.17.1:

| input | resulting hash | `IsZero()` |
|---|---|---|
| `zzzz` | `0000…0000` | true |
| `deadbeef` | `deadbeef0000000000000000000000000000` | **false** |

The second is the hazard: a truncated SHA becomes a plausible-looking but wrong 40-hex hash that nothing downstream can distinguish from a real one.

**(b) A `refs/` prefix guard is too narrow.** The concern is the `isHashRef` sentinel at `internal/core/services/installer/core.go:535` — `!IsTag() && !IsBranch() && !IsRemote()` — which marks a raw-SHA pin. A ref matching it reaches `checkoutHashWithDeepen` with a garbage hash and writes a garbage version into the lock at `core.go:558`. Verified: these all satisfy the sentinel *and* start with `refs/`:

```
refs/pull/12/head          Short="pull/12/head"           IsTag=false IsBranch=false IsRemote=false
refs/merge-requests/7/head Short="merge-requests/7/head"  IsTag=false IsBranch=false IsRemote=false
```

GitLab advertises `refs/merge-requests/*`, `refs/keep-around/*`, `refs/environments/*` and `refs/pipelines/*`; GitHub advertises `refs/pull/*`. Use an **allowlist of `refs/heads/` and `refs/tags/`**, not a `refs/` prefix. This also makes the parser correct regardless of whether a caller remembers `--heads --tags`, which is the whole point of guarding here.

Bare `git ls-remote <url>` also emits a `<sha>\tHEAD` line, which the allowlist drops. (`--heads --tags` already filters it — verified — so that part is defence in depth.)

**Implementation.** Replace the loop body's guards:

```go
		sha, name := fields[0], fields[1]
		// Only real branches and tags. Server-advertised refs such as
		// refs/pull/*, refs/merge-requests/* and a bare HEAD line all satisfy
		// installer's isHashRef sentinel (!IsTag && !IsBranch && !IsRemote)
		// and would be mistaken for a raw-SHA pin.
		if !strings.HasPrefix(name, "refs/heads/") && !strings.HasPrefix(name, "refs/tags/") {
			continue
		}
		if !isFullHexSHA(sha) {
			continue
		}
```

with the helper:

```go
// isFullHexSHA reports whether s is exactly 40 lowercase-or-uppercase hex
// digits. plumbing.NewHash silently zero-pads anything shorter, turning a
// truncated SHA into a plausible-looking but wrong hash, so the length must be
// checked before the string is handed to go-git.
func isFullHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
```

**(c) Correct the doc comment** so it states what is actually enforced: only `refs/heads/*` and `refs/tags/*` entries with a full 40-hex SHA are returned; peeled `^{}` entries are dropped; everything else — including `--symref` output, which arrives as three fields — is ignored.

**Tests.** Extend `TestParseLsRemote`'s fixture and assertions:

- Give `refs/tags/v1.1.0` a SHA distinct from both `refs/heads/main` and its peeled entry, so the fixture matches real annotated-tag output.
- **Assert `got["v1.1.0"]` equals the unpeeled tag-object SHA.** Without this the test cannot tell "skipped the peeled entry" from "used the peeled entry's hash" — an implementation that stripped `^{}` and deduped would pass unchanged while returning the wrong hash. This is the single highest-value assertion to add.
- Add cases proving the new guards: a bare `HEAD` line, a `refs/merge-requests/7/head` line, a `refs/pull/12/head` line, a short-SHA line (`deadbeef\trefs/heads/bad`), and a non-hex-SHA line. None may appear in the output.
- Include the expected value in every failure message (`got %q, want %q`), matching the style of `TestHostFromURL` above it.

**Commit** separately before Task 2's own work:

```bash
git add internal/adapters/secondary/git/git_native.go internal/adapters/secondary/git/git_native_test.go
git commit -m "fix(git): restrict parseLsRemote to valid branch and tag refs"
```

**Step 1: Write the failing test**

Create `internal/adapters/secondary/git/git_lsremote_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/secondary/git -run TestListRefsNative -v`
Expected: FAIL — `undefined: ListRefsNative`

**Step 3: Write minimal implementation**

Add to `internal/adapters/secondary/git/git_native.go`:

```go
// ListRefsNative enumerates the dependency's remote heads and tags using the
// system git binary. It is the SSH counterpart to the ref listing go-git does
// inside GetVersions: go-git's SSH transport does not read ~/.ssh/config and
// enforces strict known_hosts, so it cannot be relied on for internal hosts
// that use config aliases, custom keys or non-standard ports.
//
// The URL is passed explicitly, so no worktree, .git pointer or prior clone is
// required. Only stdout is parsed; git writes its "From <url>" banner to stderr.
func ListRefsNative(dep domain.Dependency, decision auth.Decision) ([]*plumbing.Reference, error) {
	if err := requireGit(dep, hostFromURL(decision.URL)); err != nil {
		return nil, err
	}

	//nolint:gosec,nolintlint // Git command with a resolved, validated remote URL
	cmd := exec.Command("git", "ls-remote", "--heads", "--tags", decision.URL) // #nosec G204
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-remote failed for %s: %w\nStderr: %s",
			dep.Repository, err, stderr.String())
	}

	return parseLsRemote(stdout.String()), nil
}
```

All of `bytes`, `fmt`, `os`, `os/exec`, `plumbing`, `domain` and `auth` are already imported in this file. Verify before adding imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/secondary/git -run TestListRefsNative -v`
Expected: PASS (both tests)

**Step 5: Commit**

```bash
git add internal/adapters/secondary/git/git_native.go internal/adapters/secondary/git/git_lsremote_test.go
git commit -m "feat(git): add ListRefsNative for SSH ref discovery"
```

---

## Task 2b: Harden `ListRefsNative`

Found reviewing Task 2. Do before Task 3, as its own commit.

**Files:**
- Modify: `internal/adapters/secondary/git/git_native.go`
- Modify: `internal/adapters/secondary/git/git_lsremote_test.go`

**(a) Redact credentials before wrapping stderr into the error.** `ListRefsNative` inherits the whole environment and folds stderr into its returned error. `tryGitLabCI` puts a job token in `decision.URL` today. Verified against git 2.55:

| case | result |
|---|---|
| HTTPS failure, no tracing | `fatal: unable to access 'https://nonexistent.invalid/foo/bar/'` — token stripped by git |
| HTTPS failure, `GIT_TRACE=1` set | token printed to stderr **verbatim, 3×** |
| `file://` (any non-curl transport) | `fatal: '//user:SUPERSECRET123@/no/such/path' ...` — **no anonymization at all** |

So a developer or runner with `GIT_TRACE` set puts the token into a returned error, and from there into logs. Add a redaction helper — the repo has none (`grep -rn "redact|sanitiz|maskURL"` → no hits):

```go
// redactURLCredentials replaces the userinfo component of any URL-like
// substring with "***". git anonymizes credentials on some transports but not
// all — file:// and ssh:// print them verbatim, and GIT_TRACE dumps the raw
// command line regardless — so stderr must be scrubbed before it is wrapped
// into an error that may reach a log.
func redactURLCredentials(s string) string {
	return reURLCredentials.ReplaceAllString(s, "$1***@")
}

var reURLCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)
```

Apply it to **both** the stderr text and `decision.URL` if the URL is ever added to a message.

**(b) Do not inherit tracing and prompting environment.** Replace `cmd.Env = os.Environ()` with a filtered environment: drop every `GIT_TRACE*`, `GIT_CURL_VERBOSE` and `GIT_REDACT_COOKIES` variable, and set `GIT_TERMINAL_PROMPT=0` so an unauthenticated host fails instead of blocking on a credential prompt. Task 3 calls this once per dependency, so a hang here stalls the whole install.

**(c) Fix two inaccurate comments.**
- The doc comment says *"git writes its `From <url>` banner to stderr"*. That banner is `git fetch`; `ls-remote` writes nothing to stderr on success. The real reason for splitting the streams is that `runCommand` routes stdout to `msg.Debug` and discards it. State that instead.
- The `#nosec` justification claims a "validated remote URL", but nothing validates it. Not currently exploitable — every `ResolveWith` branch prefixes `https://` or `git@`, so no leading-dash URL can reach here — but the comment should match reality. Either soften it or add `--end-of-options` before the URL.

**(d) Make `buildNativeFixture` hermetic.** It overrides `init.defaultBranch` and identity but still inherits global config. Two confirmed breakages: a global `core.hooksPath` — which `pre-commit` sets, and this project pins `pre-commit` in `mise.toml` — makes `git commit` run an ambient hook and fail the test; an ambient `GIT_DIR` hijacks `git init .` entirely. Set `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` to a nonexistent path (git ≥ 2.32) and explicitly clear `GIT_DIR`, `GIT_WORK_TREE` and `GIT_INDEX_FILE`.

**(e) Tighten the integration test.** `TestListRefsNative_SeesAllBranchesAndTags` asserts only that four names are *present*, so deleting `--heads --tags` leaves it green, a leaked `HEAD` goes unnoticed, and a mangled SHA goes unnoticed. Assert the exact ref set and the SHAs. Add an **annotated** tag (`git tag -a`) to the fixture: lightweight tags emit no `^{}` line, so the peeled-entry branch — the parser's subtlest — is currently never exercised against real git output.

**(f) Rename `TestListRefsNative_BadURL` → `TestListRefsNative_UnreachableRemote`** (the URL is well-formed; the path is missing), and assert the returned slice is `nil` on error rather than discarding it.

**(g)** Use `file:///` + path rather than `"file://" + filepath.ToSlash(src)`, which on Windows yields `file://C:/...` and puts the drive letter in the URL authority.

**Commit:**

```bash
git add internal/adapters/secondary/git/git_native.go internal/adapters/secondary/git/git_lsremote_test.go
git commit -m "fix(git): redact credentials and harden ListRefsNative environment"
```

---

## Task 2c: Apply the same hardening to `runCommand`

Found during Task 2b. Small; do it before Task 3.

`runCommand` (`internal/adapters/secondary/git/git_native.go:248`) is the shared runner for clone, fetch, checkout and submodule init. It has both defects Task 2b just fixed in `ListRefsNative`: `cmd.Env = os.Environ()` and `fmt.Errorf("command failed: %w\nStderr: %s", ...)` with unredacted stderr. `doClone` passes `decision.URL` as an argument, so a GitLab CI job token reaches `git clone`'s command line today.

**Files:** `internal/adapters/secondary/git/git_native.go`

- Use `gitSafeEnv()` instead of `os.Environ()`.
- Wrap the stderr in the error with `redactURLCredentials(...)`.
- Also redact the `msg.Debug` stderr/stdout lines below it — debug output is still output.

Scope note: `internal/core/services/auth/gitlab_ci.go:27` is the **only** layer that puts credentials in `decision.URL` — verified; every other layer builds a clean `https://host/path` or `git@host:path`. Task 6 removes it, after which the redaction here is defence in depth. The `GIT_TERMINAL_PROMPT=0` from `gitSafeEnv()` is the part that keeps standing value: it stops a clone or fetch against an unauthenticated host from blocking a CI job forever.

**Commit:**

```bash
git add internal/adapters/secondary/git/git_native.go
git commit -m "fix(git): harden runCommand environment and redact stderr"
```

---

## Task 3: Dispatch `GetVersions` on transport

This is the fix that removes the silent main-branch fallback for SSH dependencies.

> **Plan correction.** The original Task 3 said to `msg.Warn` and return an empty slice when native listing fails. That does **not** work: `GetVersions` has no error return, so an empty slice sends `installer.getVersion` to `nil`, `getReferenceName` sees `bestMatch == nil` at `core.go:504`, and it falls back to the main branch at `core.go:511` — reproducing the exact bug this branch exists to fix, with the dispatch in place and looking correct. The port must carry the error.

**Files:**
- Modify: `internal/core/ports/git.go:26` and `:60` — both `GetVersions` declarations gain `error`
- Modify: `internal/adapters/secondary/git/git.go:78`
- Modify: `internal/core/services/installer/git_client.go:43-44`
- Modify: `internal/core/services/installer/core.go:603`
- Modify: any test double implementing the port

**Signature change:**

```go
GetVersions(repository *git.Repository, dep domain.Dependency) ([]*plumbing.Reference, error)
```

**At the single real caller** (`internal/core/services/installer/core.go:603`), a listing failure must be terminal — never a fallback:

```go
	versions, errVersions := git.GetVersions(ic.config, repository, dep)
	if errVersions != nil {
		// Never fall through to the main-branch fallback here: silently
		// building the wrong branch is worse than not building.
		msg.Die("❌ Could not list versions for '%s': %s", dep.Repository, errVersions)
	}
```

`msg.Die` is the established idiom for unrecoverable resolution failure and is already used sixteen lines later at `core.go:519`.

**Step 1: Read the current implementation**

Run: `sed -n '78,128p' internal/adapters/secondary/git/git.go`

Note its shape: resolve auth, fetch with `refs/*:refs/*`, then enumerate `repository.Tags()` and `repository.Branches()`.

**Step 2: Write the implementation**

Replace the body of `GetVersions` so it dispatches before doing any go-git work. Keep the existing go-git path intact as the HTTPS branch — only add the SSH branch and extract the existing body:

```go
// GetVersions returns all versions (tags and branches) of the repository.
// SSH dependencies are listed through the system git binary; HTTPS
// dependencies keep the embedded go-git path.
//
// A listing failure is returned, never swallowed: an empty result is
// indistinguishable from "no matching version" to the caller, which would
// silently fall back to the main branch.
func GetVersions(
	cfg env.ConfigProvider,
	repository *goGit.Repository,
	dep domain.Dependency,
) ([]*plumbing.Reference, error) {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return nil, fmt.Errorf("resolve auth for %s: %w", dep.Repository, err)
	}

	if decision.Transport == auth.TransportSSH {
		return ListRefsNative(dep, decision)
	}

	return getVersionsEmbedded(cfg, repository, dep, decision), nil
}
```

Note the auth-resolution failure also becomes terminal. It was previously a `msg.Warn` that continued with a zero-value `Decision`, which silently means "HTTPS, no credentials" — another path to the wrong result.

Then move the existing body — everything from `err = repository.Fetch(...)` to the final `return result` — into a new unexported function, taking the already-resolved decision so auth is not resolved twice:

```go
// getVersionsEmbedded is the go-git ref listing used for HTTPS dependencies.
// It mirrors the remote's refs into the cache so that branches appear under
// refs/heads/* and are visible to repository.Branches().
func getVersionsEmbedded(
	_ env.ConfigProvider,
	repository *goGit.Repository,
	dep domain.Dependency,
	decision auth.Decision,
) []*plumbing.Reference {
	// ... existing body, unchanged, minus the auth.Resolve call ...
}
```

**Step 3: Add a regression test proving a listing failure is not swallowed**

This is the assertion that would have caught the original plan's mistake. In `internal/adapters/secondary/git/git_lsremote_test.go`, assert that `GetVersions` for a dependency resolving to SSH with an unreachable remote returns a non-nil error and nil refs — not an empty slice and nil error.

**Step 4: Build and run the full suite**

Run: `go build ./... && go test ./... 2>&1 | tail -20`
Expected: build OK, all packages PASS. `TestGetVersions_EmptyRepo` needs its call site updated for the new two-value return. Update every test double implementing `ports.Git` / `ports.GitClientV2`.

**Step 5: Verify lint is clean**

Run: `GOTOOLCHAIN=go1.24.1 go tool golangci-lint run ./...`
Expected: no findings.

**Step 6: Commit**

```bash
git add internal/core/ports/git.go internal/adapters/secondary/git/git.go \
        internal/core/services/installer/git_client.go \
        internal/core/services/installer/core.go
git commit -m "fix(git): dispatch GetVersions on transport so SSH deps see all refs"
```

---

## Task 4: Stop shallow clone from restricting the refspec

**Files:**
- Modify: `internal/adapters/secondary/git/git_native.go` (in `doClone`)
- Modify: `internal/adapters/secondary/git/git_embedded.go` (in `CloneCacheEmbedded`)
- Test: Create `internal/adapters/secondary/git/git_shallow_test.go`

**Step 1: Write the failing test**

Create `internal/adapters/secondary/git/git_shallow_test.go`. This test drives the real `git` binary through the exact clone flags `doClone` uses, so it fails against the current flags and passes after the fix — without needing bossy's env plumbing:

```go
//nolint:testpackage // Testing internal behaviour
package gitadapter

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShallowCloneKeepsAllBranchesReachable is the regression test for the
// shallow-clone refspec defect: with --single-branch, git writes a restricted
// refspec (+refs/heads/main:refs/remotes/origin/main) and every other branch
// becomes permanently unreachable — `fetch --all` brings nothing and even
// `fetch --unshallow` does not recover it.
func TestShallowCloneKeepsAllBranchesReachable(t *testing.T) {
	url := buildNativeFixture(t)
	dest := filepath.Join(t.TempDir(), "mod")

	// These must stay in sync with the flags doClone passes when
	// env.GetGitShallow() is true.
	args := []string{"clone", "--depth", "1", "--no-single-branch", url, dest}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	refspec := gitOut(t, dest, "config", "remote.origin.fetch")
	if !strings.Contains(refspec, "refs/heads/*") {
		t.Fatalf("refspec restricted to a single branch: %q", refspec)
	}

	// The repo must still actually be shallow — we are not trading the bug
	// for a full clone.
	if shallow := gitOut(t, dest, "rev-parse", "--is-shallow-repository"); shallow != "true" {
		t.Errorf("expected shallow repository, got %q", shallow)
	}

	// The non-default branch must be checkoutable.
	cmd := exec.Command("git", "checkout", "-f", "develop")
	cmd.Dir = dest
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout develop on shallow clone: %v\n%s", err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
```

**Step 2: Confirm the test encodes the real defect**

Temporarily change `--no-single-branch` to `--single-branch` in the test and run it.

Run: `go test ./internal/adapters/secondary/git -run TestShallowCloneKeepsAllBranchesReachable -v`
Expected: FAIL with `refspec restricted to a single branch: "+refs/heads/main:refs/remotes/origin/main"`

Change it back to `--no-single-branch` and confirm it now passes. This step proves the assertion has teeth; do not skip it.

**Step 3: Fix the native adapter**

In `internal/adapters/secondary/git/git_native.go`, inside `doClone`:

```go
	if env.GetGitShallow() {
		msg.Debug("Using shallow clone for %s", dep.Repository)
		// --no-single-branch is required: git implies --single-branch with
		// --depth, which writes a refspec restricted to the default branch and
		// makes every other branch permanently unreachable. Not even
		// `fetch --unshallow` recovers them.
		args = append(args, "--depth", "1", "--no-single-branch")
	}
```

**Step 4: Fix the embedded adapter**

In `internal/adapters/secondary/git/git_embedded.go`, inside `CloneCacheEmbedded`:

```go
	if env.GetGitShallow() {
		msg.Debug("Using shallow clone for %s", dep.Repository)
		cloneOpts.Depth = 1
		// SingleBranch must stay false — see doClone for the reasoning.
		cloneOpts.SingleBranch = false
	}
```

**Step 5: Run the suite**

Run: `go test ./internal/adapters/secondary/git -v`
Expected: all PASS

**Step 6: Commit**

```bash
git add internal/adapters/secondary/git/git_native.go \
        internal/adapters/secondary/git/git_embedded.go \
        internal/adapters/secondary/git/git_shallow_test.go
git commit -m "fix(git): drop --single-branch so shallow clones keep all branches reachable"
```

---

## Task 5: Repair refspecs on caches already cloned single-branch

Task 4 fixes new clones. Build servers and developer machines already hold caches with a restricted refspec; those stay broken forever without a repair.

**Files:**
- Modify: `internal/adapters/secondary/git/git_native.go` (in `getWrapperFetch`)

**Step 1: Add the repair before the fetch**

In `getWrapperFetch`, after `writeDotGitFile(dep)` and the `git reset --hard`, before `git fetch --all`:

```go
	// Repair caches cloned by an older bossy that passed --single-branch: their
	// refspec is pinned to one branch, so `fetch --all` silently brings nothing
	// for every other branch. Rewriting the refspec is idempotent and harmless
	// on healthy caches.
	cmdRefspec := exec.Command("git", "config", "remote.origin.fetch",
		"+refs/heads/*:refs/remotes/origin/*")
	cmdRefspec.Dir = dirModule
	if err := runCommand(cmdRefspec); err != nil {
		msg.Debug("Could not normalise refspec for %s: %s", dep.Repository, err)
	}
```

Failure is logged at debug level, not returned: a cache that cannot be repaired should still attempt the fetch rather than abort the install.

**Step 2: Build and test**

Run: `go build ./... && go test ./internal/adapters/secondary/git -v`
Expected: build OK, all PASS

**Step 3: Manually verify the repair against a broken cache**

This reproduces the exact broken state and confirms the repair recovers it. Run in a scratch directory:

```bash
SRC=$(mktemp -d); DEST=$(mktemp -d)/mod
git init -q -b main "$SRC" && cd "$SRC"
git -c user.email=t@t.de -c user.name=T commit -q --allow-empty -m one
git checkout -q -b develop
git -c user.email=t@t.de -c user.name=T commit -q --allow-empty -m two
git checkout -q main
git clone -q --depth 1 --single-branch "file://$SRC" "$DEST"
cd "$DEST"
git fetch --all -q; git show-ref | grep -c develop   # expect 0 — the defect
git config remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
git fetch --all -q; git show-ref | grep -c develop   # expect 1 — repaired
```

Expected: first count `0`, second count `1`.

**Step 4: Commit**

```bash
git add internal/adapters/secondary/git/git_native.go
git commit -m "fix(git): repair restricted refspec on caches cloned single-branch"
```

---

## Task 6: Move the CI job token out of the URL

**Files:**
- Modify: `internal/core/services/auth/gitlab_ci.go`
- Test: `internal/core/services/auth/gitlab_ci_test.go`

**Step 1: Write the failing test**

Append to `internal/core/services/auth/gitlab_ci_test.go` (match the env-var setup style of the existing tests in that file):

```go
// TestGitLabCI_TokenNotInURL guards the two defects that follow from baking the
// job token into the URL string: go-git persists CloneOptions.URL into the
// cache's .git/config, which (a) writes a secret to disk on a shared build
// machine and (b) leaves an expired token behind for the next CI job to reuse.
func TestGitLabCI_TokenNotInURL(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_SERVER_HOST", "gitlab.example.com")
	t.Setenv("CI_JOB_TOKEN", "s3cr3t-token")

	parsed, err := ParseDepURL("git@gitlab.example.com:group/lib")
	if err != nil {
		t.Fatalf("ParseDepURL: %v", err)
	}

	d, ok := tryGitLabCI(parsed)
	if !ok {
		t.Fatal("expected CI layer to match")
	}

	if strings.Contains(d.URL, "s3cr3t-token") {
		t.Errorf("token leaked into URL: %q", d.URL)
	}
	if d.URL != "https://gitlab.example.com/group/lib" {
		t.Errorf("URL: got %q, want clean HTTPS form", d.URL)
	}
	if d.Credential.User != "gitlab-ci-token" {
		t.Errorf("Credential.User: got %q, want gitlab-ci-token", d.Credential.User)
	}
	if d.Credential.Password != "s3cr3t-token" {
		t.Errorf("Credential.Password: got %q, want the job token", d.Credential.Password)
	}
	if d.Transport != TransportHTTPS {
		t.Errorf("Transport: got %v, want HTTPS", d.Transport)
	}
}
```

Add `"strings"` to that file's imports if not already present.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/services/auth -run TestGitLabCI_TokenNotInURL -v`
Expected: FAIL — `token leaked into URL`

**Step 3: Write the implementation**

In `internal/core/services/auth/gitlab_ci.go`, replace the return:

```go
	// The token goes in the credential, never the URL. go-git persists the
	// clone URL into the cache's .git/config, so a token embedded here would be
	// written to disk and then reused — expired — by the next CI job.
	return Decision{
		URL:        "https://" + parsed.Host + "/" + parsed.Path,
		Transport:  TransportHTTPS,
		Credential: CredentialSpec{User: "gitlab-ci-token", Password: token},
		Layer:      "gitlab-ci",
	}, true
```

Update the function's doc comment: it currently says "rewrite any dep URL on the same GitLab instance to authenticated HTTPS" — change "authenticated HTTPS" to "HTTPS with the job token supplied as a credential".

`httpsAuth` in `git_embedded.go` needs no change: it already converts a populated `CredentialSpec` into `BasicAuth`, and will now start returning a real auth method where it previously returned `nil`.

**Step 4: Run the auth suite**

Run: `go test ./internal/core/services/auth -v`
Expected: all PASS. Check `TestGitLabCI_Match` and `TestGitLabCI_OverridesSSHForm` in particular — they assert on the old URL shape and **will need updating** to expect the clean URL plus the credential. Update them; do not weaken them.

**Step 5: Commit**

```bash
git add internal/core/services/auth/gitlab_ci.go internal/core/services/auth/gitlab_ci_test.go
git commit -m "fix(auth): carry CI job token as credential instead of in the URL"
```

---

## Task 7: Override `RemoteURL` on every fetch

Without this, Task 6 is incomplete: caches cloned by an older bossy still hold a token URL, and go-git would keep using it.

**Files:**
- Modify: `internal/adapters/secondary/git/git_embedded.go` (`UpdateCacheEmbedded`, `UnshallowFetchEmbedded`, `PullEmbedded`)
- Modify: `internal/adapters/secondary/git/git.go` (`getVersionsEmbedded`, created in Task 3)

**Step 1: Add `RemoteURL` to all four call sites**

Each already passes `Auth: httpsAuth(decision)`. Add `RemoteURL: decision.URL` alongside it.

`UpdateCacheEmbedded`:

```go
	err = repository.Fetch(&git.FetchOptions{
		Force:     true,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
	})
```

`UnshallowFetchEmbedded`:

```go
	err := repository.Fetch(&git.FetchOptions{
		Depth:     0,
		Force:     true,
		Tags:      git.AllTags,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
	})
```

`PullEmbedded`:

```go
	return worktree.Pull(&git.PullOptions{
		Force:     true,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
	})
```

`getVersionsEmbedded` in `git.go` — add to the existing `FetchOptions` that carries the `RefSpecs`:

```go
	err = repository.Fetch(&goGit.FetchOptions{
		Force:     true,
		Prune:     true,
		Auth:      httpsAuth(decision),
		RemoteURL: decision.URL,
		RefSpecs: []gitConfig.RefSpec{
			"refs/*:refs/*",
			"HEAD:refs/heads/HEAD",
		},
	})
```

Add a comment at the first site explaining the shared reason:

```go
	// RemoteURL overrides whatever was persisted in the cache's .git/config at
	// clone time. This is what keeps a stale CI job token — or a changed host
	// protocol or credential — from being reused on a later run.
```

**Step 2: Verify no fetch site was missed**

Run: `grep -n "FetchOptions\|PullOptions" internal/adapters/secondary/git/*.go`
Expected: every non-test occurrence sets `RemoteURL`. Confirm the count matches four.

**Step 3: Build and test**

Run: `go build ./... && go test ./internal/adapters/secondary/git -v`
Expected: build OK, all PASS

**Step 4: Commit**

```bash
git add internal/adapters/secondary/git/git_embedded.go internal/adapters/secondary/git/git.go
git commit -m "fix(git): override RemoteURL on fetch so stale cached URLs are not reused"
```

---

## Task 8: Scrub tokens from existing cache configs

Tasks 6 and 7 stop new tokens being written and stop old ones being used. Tokens already on disk still need removing.

**Files:**
- Modify: `internal/adapters/secondary/git/git_embedded.go`
- Test: `internal/adapters/secondary/git/git_embedded_test.go`

**Step 1: Write the failing test**

Append to `internal/adapters/secondary/git/git_embedded_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/secondary/git -run TestScrubTokenFromURL -v`
Expected: FAIL — `undefined: scrubTokenFromURL`

**Step 3: Write the implementation**

Add to `internal/adapters/secondary/git/git_embedded.go`:

```go
// scrubTokenFromURL removes embedded basic-auth credentials from an HTTP(S)
// URL. Older bossy versions persisted the GitLab CI job token into the cache's
// remote URL; this clears that secret from disk. Non-HTTP inputs and URLs
// without credentials are returned unchanged.
func scrubTokenFromURL(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "http") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}
```

Add `"net/url"` to the imports of `git_embedded.go`.

**Step 4: Call it during cache update**

In `UpdateCacheEmbedded`, after the repository is successfully opened and before the fetch:

```go
	// Clear any credential persisted into the remote URL by an older bossy.
	if cfg, cfgErr := repository.Config(); cfgErr == nil {
		changed := false
		for _, remote := range cfg.Remotes {
			for i, u := range remote.URLs {
				if scrubbed := scrubTokenFromURL(u); scrubbed != u {
					remote.URLs[i] = scrubbed
					changed = true
				}
			}
		}
		if changed {
			if setErr := repository.SetConfig(cfg); setErr != nil {
				msg.Debug("Could not scrub cached remote URL for %s: %s", dep.Repository, setErr)
			}
		}
	}
```

This is safe because Task 7 made every fetch pass `RemoteURL` explicitly, so the stored URL is no longer load-bearing.

**Step 5: Run the suite**

Run: `go test ./internal/adapters/secondary/git -v`
Expected: all PASS

**Step 6: Commit**

```bash
git add internal/adapters/secondary/git/git_embedded.go internal/adapters/secondary/git/git_embedded_test.go
git commit -m "fix(git): scrub persisted CI tokens from cached remote URLs"
```

---

## Task 9: Correct the shallow-clone documentation

README currently recommends shallow clone for CI/CD. Until Task 4 that advice actively broke branch-pinned dependencies, and the reasoning needs recording either way.

**Files:**
- Modify: `README.md` (the `### > Shallow Clone` section, around line 275)
- Modify: `docs/ci.md`

**Step 1: Update the README section**

Keep the recommendation — with Task 4 applied it is sound — and add the constraint that makes it safe:

```markdown
Shallow clones fetch only the most recent commit of each branch, which
speeds up installs significantly. All branch tips are still fetched, so
branch-pinned dependencies continue to resolve.

If a dependency is pinned to a commit SHA that predates the shallow
cut-off, bossy deepens the clone automatically on first use.
```

**Step 2: Add a note to `docs/ci.md`**

Under `## Reproducible builds`, note that shallow clone is compatible with branch-pinned and SHA-pinned dependencies, and that caches created by bossy versions before this change are repaired automatically on the next `bossy install`.

**Step 3: Commit**

```bash
git add README.md docs/ci.md
git commit -m "docs: correct shallow clone guidance for branch-pinned deps"
```

---

## Task 10: Full verification

**Step 1: Run the complete suite**

Run: `make test`
Expected: style and unit tests all pass, with `-race`.

**Step 2: Run the linter**

Run: `GOTOOLCHAIN=go1.24.1 go tool golangci-lint run ./...`

Expected: **56 findings — the pre-existing baseline — and no more.** The repository does not lint clean on `bossy-fork` and did not before this branch; `make test-style` is currently red on `main` too. Two of the 56 are goimports violations in `internal/core/ports/git.go` and `internal/core/services/installer/git_client.go`, both pre-existing: this branch's diff on those files touches only the `GetVersions` declarations and their comments, never the import blocks (verified with `git diff c40c10a..HEAD`).

The gate for this branch is therefore **"no new findings versus baseline"**, not "clean". To check, count with:

```bash
GOTOOLCHAIN=go1.24.1 go tool golangci-lint run ./... 2>&1 | grep -cE "^[a-zA-Z_/\\\\.-]+\.go:[0-9]+"
```

Cleaning up the 56 pre-existing findings is worthwhile but is separate maintenance — do not fold it into this branch, where it would bury the functional changes in formatting noise.

**Step 3: Build the binary**

Run: `make build`
Expected: `./bin/bossy` produced.

**Step 4: Smoke-test against a real branch-pinned dependency**

From a scratch directory, using a real repository with a non-default branch:

```bash
./bin/bossy init -q
./bin/bossy install github.com/HashLoad/horse@main
cat bossy-lock.json    # confirm a "commit" field is recorded
```

Expected: install succeeds and the lock records a commit SHA.

**Step 5: Update the changelog**

Add a `### Fixed` block to `CHANGELOG.md` under `[Unreleased]` covering:

- SSH dependencies now discover all branches and tags through the system git binary; previously go-git's SSH stack failed silently and resolution fell back to the main branch.
- Shallow clones no longer restrict the remote refspec, so branch-pinned dependencies resolve correctly. Existing caches are repaired on next use.
- The GitLab CI job token is carried as a credential rather than embedded in the clone URL, so it is neither written to the dependency cache nor reused after expiry. Tokens already persisted by earlier versions are scrubbed on next update.

**Step 6: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for SSH ref discovery and CI token fixes"
```

---

## Out of scope

Deliberately excluded; tracked separately:

- **Item C** — developer documentation: branch-as-a-version, post-clone onboarding, dependency update recipes, shell-runner CI section, and the ~15 CLI help strings still printing `boss` instead of `bossy`.
- **Item D** — single-segment GitLab paths (`host/repo` with no group) are mangled into `github.com/host/repo` by the legacy bare-name expansion in `installer/utils.go:141`.
- **Item E** — ~~no cache locking; parallel jobs on one shell runner share `~/.bossy/cache`~~. **Closed 2026-09-09:** the target build server runs with `concurrent = 1`, so jobs never overlap and the shared cache has no contention. Revisit only if runner concurrency is raised.
