# SHA Pinning and Host Aliases Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `bossy install` deterministic via commit-SHA pinning in the lock file, and add user-level host aliases so internal-GitLab dependencies can be referenced as `<alias>:<path>`.

**Architecture:** Add `Commit` to `LockedDependency`; capture the resolved SHA after every checkout; on `install` with a populated lock, check out by SHA instead of tag. Add an `Aliases map[string]string` to the user config and resolve `<alias>:<path>` at CLI parse time, storing the canonical `git@<host>:<path>` (or `<host>/<path>`) form in `bossy.json`.

**Tech Stack:** Go 1.24, go-git v5 (embedded), system `git` (native), cobra, pterm, semver/v3.

**Reference design:** `docs/plans/2026-05-20-sha-pinning-and-aliases-design.md`

---

## Pre-flight

1. Confirm `make test` is green on the current `bossy-fork` branch before starting. If it fails, fix or stash uncommitted work first.
2. Read `docs/plans/2026-05-20-sha-pinning-and-aliases-design.md` end-to-end. The plan below assumes you've internalized the design's edge-case list.
3. Skill prerequisites: use `git-config` skill before every commit; use `verification-before-completion` before claiming any task done.

---

### Task 1: Add `Commit` field to `LockedDependency`

**Files:**
- Modify: `internal/core/domain/lock.go`
- Test: `internal/core/domain/lock_test.go`

**Step 1: Write a failing test that asserts `Commit` is persisted through marshal/unmarshal.**

Append to `internal/core/domain/lock_test.go`:

```go
func TestLockedDependency_CommitFieldRoundTrip(t *testing.T) {
	original := PackageLock{
		Installed: map[string]LockedDependency{
			"github.com/foo/bar": {
				Name:    "bar",
				Version: "1.2.3",
				Commit:  "abcdef0123456789abcdef0123456789abcdef01",
				Hash:    "deadbeef",
			},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round PackageLock
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := round.Installed["github.com/foo/bar"].Commit
	if got != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("Commit roundtrip: got %q", got)
	}
}
```

If `encoding/json` isn't imported in this test file yet, add it.

**Step 2: Run the test — expect failure** (`Commit` undefined):

```
go test ./internal/core/domain -run TestLockedDependency_CommitFieldRoundTrip -v
```

**Step 3: Add the field.** In `internal/core/domain/lock.go:18`, insert `Commit` after `Version`:

```go
type LockedDependency struct {
	Name      string              `json:"name"`
	Version   string              `json:"version"`
	Commit    string              `json:"commit,omitempty"`
	Hash      string              `json:"hash"`
	Artifacts DependencyArtifacts `json:"artifacts"`
	Failed    bool                `json:"-"`
	Changed   bool                `json:"-"`
}
```

`omitempty` keeps existing lock files unchanged on rewrite when no SHA is known yet.

**Step 4: Verify test passes:**

```
go test ./internal/core/domain -v
```

All tests in the package should pass.

**Step 5: Commit.**

```
git add internal/core/domain/lock.go internal/core/domain/lock_test.go
git commit -m "feat(domain): add Commit field to LockedDependency for SHA pinning"
```

---

### Task 2: Add `isGitSHA` helper

**Files:**
- Create: `internal/core/domain/sha.go`
- Create: `internal/core/domain/sha_test.go`

**Step 1: Write the table-driven test.** Create `internal/core/domain/sha_test.go`:

```go
package domain

import "testing"

func TestIsGitSHA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"abcdef", false},                                                                  // 6 chars: too short
		{"abcdef0", true},                                                                  // 7 chars: minimum
		{"abcdef0123456789abcdef0123456789abcdef01", true},                                 // 40 chars: full
		{"abcdef0123456789abcdef0123456789abcdef0123", false},                              // 42 chars: too long
		{"ABCDEF0", false},                                                                 // uppercase: reject
		{"abcdefg", false},                                                                 // non-hex
		{"v1.2.3", false},                                                                  // semver
		{"^1.2.0", false},                                                                  // constraint
		{"main", false},                                                                    // branch
	}
	for _, c := range cases {
		if got := IsGitSHA(c.in); got != c.want {
			t.Errorf("IsGitSHA(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
```

**Step 2: Run — expect fail** (`IsGitSHA` undefined):

```
go test ./internal/core/domain -run TestIsGitSHA -v
```

**Step 3: Implement.** Create `internal/core/domain/sha.go`:

```go
package domain

import "regexp"

var gitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// IsGitSHA reports whether s is a valid abbreviated or full git SHA-1
// (7–40 lowercase hex chars). Git itself accepts shorter prefixes when
// unambiguous, but 7 is the conventional minimum.
func IsGitSHA(s string) bool {
	return gitSHAPattern.MatchString(s)
}
```

**Step 4: Verify:**

```
go test ./internal/core/domain -v
```

**Step 5: Commit.**

```
git add internal/core/domain/sha.go internal/core/domain/sha_test.go
git commit -m "feat(domain): add IsGitSHA helper for version-string classification"
```

---

### Task 3: Extend `LockService.AddDependency` with a `commit` parameter

**Files:**
- Modify: `internal/core/services/lock/lock_service.go`
- Modify: `internal/core/services/lock/lock_service_test.go`
- Modify: `internal/core/services/installer/core.go:528` (call site)

**Step 1: Write a test that asserts a commit is stored.** Append to `internal/core/services/lock/lock_service_test.go`:

```go
func TestLockService_AddDependency_StoresCommit(t *testing.T) {
	fs := newMockFs()
	repo := &mockLockRepo{}
	svc := NewLockService(repo, fs)
	lock := &domain.PackageLock{Installed: map[string]domain.LockedDependency{}}
	dep := domain.ParseDependency("github.com/foo/bar", ">0.0.0")

	svc.AddDependency(lock, dep, "1.2.3", "abcdef0123456789abcdef0123456789abcdef01", t.TempDir())

	got := lock.Installed[dep.GetKey()]
	if got.Commit != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("Commit = %q, want pinned SHA", got.Commit)
	}
}
```

Use whatever fixtures the existing tests in this file already provide for `newMockFs` / `mockLockRepo`; mirror an existing test.

**Step 2: Run — expect fail** (signature mismatch):

```
go test ./internal/core/services/lock -v
```

**Step 3: Change the signature.** In `internal/core/services/lock/lock_service.go:72`:

```go
func (s *LockService) AddDependency(lock *domain.PackageLock, dep domain.Dependency, version, commit, modulesDir string) {
	depDir := filepath.Join(modulesDir, dep.Name())
	hash := utils.HashDir(depDir)

	key := dep.GetKey()
	if existing, ok := lock.Installed[key]; !ok {
		lock.Installed[key] = domain.LockedDependency{
			Name:    dep.Name(),
			Version: version,
			Commit:  commit,
			Hash:    hash,
			Changed: true,
			Artifacts: domain.DependencyArtifacts{
				Bin: []string{},
				Bpl: []string{},
				Dcp: []string{},
				Dcu: []string{},
			},
		}
	} else {
		existing.Version = version
		if commit != "" {
			existing.Commit = commit
		}
		existing.Hash = hash
		lock.Installed[key] = existing
	}
}
```

The `if commit != ""` guard preserves an existing SHA on a no-op update where the caller couldn't determine the SHA (defensive — the new code paths always pass it).

**Step 4: Fix the single call site.** In `internal/core/services/installer/core.go:528` change:

```go
ic.lockSvc.AddDependency(ic.rootLocked, dep, referenceName.Short(), ic.modulesDir)
```

to (temporarily, until Task 5 wires up the actual SHA):

```go
ic.lockSvc.AddDependency(ic.rootLocked, dep, referenceName.Short(), "", ic.modulesDir)
```

**Step 5: Update any other callers and existing tests.** Run:

```
go build ./...
```

Fix every compile error by adding a `""` for the `commit` parameter. Likely only test files in `internal/core/services/lock` need updates.

**Step 6: Run full test suite for the package:**

```
go test ./internal/core/services/lock -v
```

**Step 7: Commit.**

```
git add internal/core/services/lock/ internal/core/services/installer/core.go
git commit -m "feat(lock): thread commit SHA through LockService.AddDependency"
```

---

### Task 4: Add `CheckoutHash` to the git adapter

**Files:**
- Modify: `internal/adapters/secondary/git/git.go`
- Modify: `internal/adapters/secondary/git/git_embedded.go`
- Modify: `internal/adapters/secondary/git/git_native.go`
- Test: `internal/adapters/secondary/git/git_test.go`

**Step 1: Look at how the existing `Checkout` is structured.** Read `internal/adapters/secondary/git/git.go:166-175`, `git_embedded.go` for `CheckoutEmbedded`, `git_native.go` for `CheckoutNative`. Mirror exactly.

**Step 2: Write the failing test.** Append to `git_test.go` a test that creates a fixture repo with two commits, calls `CheckoutHash` with the first commit's SHA, and asserts `HEAD` resolves to that SHA. Use the existing fixture pattern in the file (don't invent a new one).

If the file has no fixture helper today, skip the unit test here and rely on the integration test in Task 11. State this explicitly in the commit message.

**Step 3: Add the dispatcher in `git.go`.** Append after the existing `Checkout` function:

```go
// CheckoutHash checks out a specific commit SHA in detached-HEAD state.
// Returns an error if the SHA is not present locally; callers should
// trigger a deepening fetch and retry on "object not found".
func CheckoutHash(_ env.ConfigProvider, dep domain.Dependency, hash plumbing.Hash) error {
	decision, err := auth.Resolve(dep)
	if err != nil {
		return err
	}
	if decision.Transport == auth.TransportSSH {
		return CheckoutHashNative(dep, decision, hash)
	}
	return CheckoutHashEmbedded(dep, hash)
}
```

**Step 4: Implement `CheckoutHashEmbedded` in `git_embedded.go`.**

```go
func CheckoutHashEmbedded(dep domain.Dependency, hash plumbing.Hash) error {
	repo := GetRepository(dep)
	if repo == nil {
		return fmt.Errorf("repository not found for %s", dep.Repository)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}
	return worktree.Checkout(&goGit.CheckoutOptions{Hash: hash, Force: true})
}
```

Add any missing imports.

**Step 5: Implement `CheckoutHashNative` in `git_native.go`.** Mirror the existing `CheckoutNative` exec pattern (shell out to system `git`). The command is:

```
git -C <modules_dir>/<dep.Name()> checkout --detach <hash.String()>
```

Use the existing helpers in `git_native.go` for running the command and surfacing stderr; do not reinvent the wrapper.

**Step 6: Build and run package tests:**

```
go build ./...
go test ./internal/adapters/secondary/git -v
```

**Step 7: Commit.**

```
git add internal/adapters/secondary/git/
git commit -m "feat(git): add CheckoutHash for SHA-based checkout in detached HEAD"
```

---

### Task 5: Wire SHA fast-path into `getVersion` and capture SHA after checkout

**Files:**
- Modify: `internal/core/services/installer/core.go` (`getVersion`, `checkoutAndUpdate`)
- Test: `internal/core/services/installer/core_test.go`

**Step 1: Write tests for `getVersion`'s SHA fast-path.** Add a test that constructs an `installContext` with `useLockedVersion=true` and a lock entry whose `Commit` is set, and asserts the returned reference is a hash reference pointing at that SHA — without touching the network. Use the existing mock-git pattern in this file.

Mirror the structure of any existing `getVersion` test. If none exists, write a minimal one using a fake `GitClient`.

**Step 2: Run — expect fail.**

```
go test ./internal/core/services/installer -run TestGetVersion -v
```

**Step 3: Modify `getVersion`** (`internal/core/services/installer/core.go:549`). Insert at the top of the function, before the existing `if ic.useLockedVersion` block:

```go
// Raw SHA in bossy.json — terminal, skip resolution.
if domain.IsGitSHA(dep.GetVersion()) {
	return plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(dep.GetVersion()))
}
```

Then modify the existing locked-version block (lines 553-559) to prefer `Commit` over the tag:

```go
if ic.useLockedVersion {
	lockedDependency := ic.rootLocked.GetInstalled(dep)
	if lockedDependency.Commit != "" {
		return plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(lockedDependency.Commit))
	}
	if tag := git.GetByTag(repository, lockedDependency.Version); tag != nil &&
		lockedDependency.Version != dep.GetVersion() {
		return tag
	}
}
```

**Step 4: Modify `checkoutAndUpdate`** (lines 519-547) to dispatch on hash vs ref and capture the SHA:

```go
func (ic *installContext) checkoutAndUpdate(
	dep domain.Dependency,
	repository *goGit.Repository,
	referenceName plumbing.ReferenceName,
) error {
	isHashRef := referenceName.IsTag() == false && referenceName.IsBranch() == false && referenceName.IsRemote() == false
	var err error
	if isHashRef {
		if !ic.progress.IsEnabled() {
			msg.Debug("  📌 %s pinned to %s", dep.Name(), referenceName.Short()[:min(7, len(referenceName.Short()))])
		}
		err = git.CheckoutHash(ic.config, dep, plumbing.NewHash(referenceName.Short()))
	} else {
		if !ic.progress.IsEnabled() {
			msg.Debug("  🔍 Checking out %s to %s", dep.Name(), referenceName.Short())
		}
		err = git.Checkout(ic.config, dep, referenceName)
	}

	commit := ""
	if head, headErr := repository.Head(); headErr == nil {
		commit = head.Hash().String()
	}
	ic.lockSvc.AddDependency(ic.rootLocked, dep, referenceName.Short(), commit, ic.modulesDir)

	if err != nil {
		return err
	}

	// Skip pull on detached-HEAD checkouts — they're pinned.
	if isHashRef {
		return nil
	}

	if !ic.progress.IsEnabled() {
		msg.Debug("  📥 Pulling latest changes for %s", dep.Name())
	}
	err = git.Pull(ic.config, dep)
	if err != nil && !errors.Is(err, goGit.NoErrAlreadyUpToDate) {
		warnMsg := fmt.Sprintf("Error on pull from dependency %s\n%s", dep.Repository, err)
		if !ic.progress.IsEnabled() {
			msg.Warn("  " + warnMsg)
		}
		ic.addWarning(fmt.Sprintf("%s: %s", dep.Name(), warnMsg))
	}
	return nil
}
```

Add `"github.com/basti-fantasti/bossy/internal/core/domain"` to imports if it's not already present (it is — line 18).

**Step 5: Verify build + tests:**

```
go build ./...
go test ./internal/core/services/installer -v
```

**Step 6: Commit.**

```
git add internal/core/services/installer/core.go internal/core/services/installer/core_test.go
git commit -m "feat(installer): pin by SHA when lock has Commit or version is raw SHA"
```

---

### Task 6: Handle shallow-clone "object not found" with one deepening retry

**Files:**
- Modify: `internal/core/services/installer/core.go` (`checkoutAndUpdate`)
- Modify: `internal/adapters/secondary/git/git_embedded.go` and `git_native.go` (add deepening fetch helpers if needed)

**Step 1: Read the shallow-clone fetch path.** Grep for `Depth` and `Shallow` in `internal/adapters/secondary/git/`. Locate the existing fetch helpers.

**Step 2: Add an `UnshallowFetch(dep)` package function** in `git.go` mirroring the `Checkout`/`Pull` dispatch pattern. Implementations:

- embedded: `repo.Fetch(&FetchOptions{Depth: 0, Force: true, Tags: AllTags, Auth: httpsAuth(decision)})`
- native: `git -C <dir> fetch --unshallow` (and tolerate the "fatal: --unshallow on a complete repository" exit code as success — check existing native helpers for how stderr is matched).

**Step 3: Wrap the `CheckoutHash` call in `checkoutAndUpdate`** with a one-shot retry:

```go
err = git.CheckoutHash(ic.config, dep, plumbing.NewHash(referenceName.Short()))
if err != nil && isObjectNotFound(err) {
	if fetchErr := git.UnshallowFetch(ic.config, dep); fetchErr != nil {
		return fmt.Errorf("checkout %s: object missing and deepening fetch failed: %w", referenceName.Short(), fetchErr)
	}
	err = git.CheckoutHash(ic.config, dep, plumbing.NewHash(referenceName.Short()))
}
```

Define `isObjectNotFound` in the same file: match against `plumbing.ErrObjectNotFound` (embedded) and substring `"unknown revision"` / `"object not found"` in native stderr.

**Step 4: Add a unit test** that simulates the retry path with a fake git client (`internal/core/services/installer/core_test.go`). If the existing mock-git surface doesn't support this, defer to the integration test in Task 11 and note it.

**Step 5: Build + test:**

```
go build ./...
go test ./internal/core/services/installer -v
```

**Step 6: Commit.**

```
git add internal/adapters/secondary/git/ internal/core/services/installer/core.go
git commit -m "feat(installer): deepen shallow clone once when pinned SHA is missing"
```

---

### Task 7: Add `Aliases` to user configuration

**Files:**
- Modify: `pkg/env/configuration.go`
- Modify: `pkg/consts/consts.go`
- Create: `pkg/consts/alias.go` (regex)
- Modify: `pkg/env/configuration_test.go` (or create one if absent)

**Step 1: Write a failing test** that the alias-name regex accepts `gtr`, `my-org`, `org_2` and rejects `Gtr`, `2org`, `gtr.de`, `gtr:`, `gtr/foo`, `git`, empty string. Place in `pkg/consts/alias_test.go`.

**Step 2: Run — expect fail:**

```
go test ./pkg/consts -v
```

**Step 3: Create `pkg/consts/alias.go`:**

```go
package consts

import "regexp"

// AliasNamePattern restricts alias names so they cannot collide with
// host names (which may contain dots) or the "git@" SSH prefix.
var AliasNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
```

Note: `git` matches the pattern. Document this and rely on the
config-command-level collision check (Task 8) to refuse reserved names.

**Step 4: Add `Aliases` to `Configuration`** in `pkg/env/configuration.go:31` (after `HostProtocols`):

```go
HostProtocols       map[string]string `json:"host_protocols,omitempty"`
Aliases             map[string]string `json:"aliases,omitempty"`
```

**Step 5: Build + verify tests:**

```
go build ./...
go test ./pkg/env ./pkg/consts -v
```

**Step 6: Commit.**

```
git add pkg/env/configuration.go pkg/consts/alias.go pkg/consts/alias_test.go
git commit -m "feat(config): add Aliases map and AliasNamePattern"
```

---

### Task 8: Add `bossy config alias` commands

**Files:**
- Create: `internal/adapters/primary/cli/config/alias.go`
- Create: `internal/adapters/primary/cli/config/alias_test.go`
- Modify: `internal/adapters/primary/cli/config/config.go` (register the new command)

**Step 1: Look at how `protocolCmd` is registered** in `internal/adapters/primary/cli/config/git.go:24` and how `config.go` wires it. Mirror.

**Step 2: Write tests** for: setting a valid alias, rejecting an invalid name, rejecting a collision with a configured `HostProtocols` entry, unset, and list. Use cobra's in-test invocation. Mirror `config_test.go`'s setup.

**Step 3: Run — expect fail:**

```
go test ./internal/adapters/primary/cli/config -v
```

**Step 4: Create `alias.go`:**

```go
package config

import (
	"sort"

	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

var reservedAliasNames = map[string]struct{}{
	"git": {}, "http": {}, "https": {}, "ssh": {},
}

func aliasCmd() *cobra.Command {
	var list bool
	var unset string

	cmd := &cobra.Command{
		Use:   "alias <name> <host>",
		Short: "Manage host aliases for dependency shorthand (`<alias>:<path>`)",
		Long: `Define short aliases for git hosts so dependencies can be referenced
as <alias>:<path>. The alias is expanded at install time and the canonical
host/path form is stored in bossy.json (aliases never round-trip).`,
		Args: cobra.MaximumNArgs(2),
		Run: func(_ *cobra.Command, args []string) {
			cfg := env.GlobalConfiguration()
			if cfg.Aliases == nil {
				cfg.Aliases = map[string]string{}
			}

			switch {
			case list:
				if len(cfg.Aliases) == 0 {
					msg.Info("No aliases configured")
					return
				}
				keys := make([]string, 0, len(cfg.Aliases))
				for k := range cfg.Aliases {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					msg.Info("%s → %s", k, cfg.Aliases[k])
				}

			case unset != "":
				if _, ok := cfg.Aliases[unset]; !ok {
					msg.Die("alias %q not set", unset)
				}
				delete(cfg.Aliases, unset)
				cfg.SaveConfiguration()
				msg.Success("✅ removed alias %q", unset)

			default:
				if len(args) != 2 {
					msg.Die("usage: bossy config alias <name> <host>")
				}
				name, host := args[0], args[1]
				if !consts.AliasNamePattern.MatchString(name) {
					msg.Die("alias name %q invalid (must match %s)", name, consts.AliasNamePattern.String())
				}
				if _, reserved := reservedAliasNames[name]; reserved {
					msg.Die("alias name %q is reserved", name)
				}
				if _, ok := cfg.HostProtocols[name]; ok {
					msg.Die("alias name %q collides with a configured host protocol", name)
				}
				cfg.Aliases[name] = host
				cfg.SaveConfiguration()
				msg.Success("✅ %s → %s", name, host)
			}
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "List all configured aliases")
	cmd.Flags().StringVar(&unset, "unset", "", "Remove the named alias")
	return cmd
}
```

**Step 5: Register the command.** In `internal/adapters/primary/cli/config/config.go`, find where `registryGitCmd` is called and add a sibling registration that adds `aliasCmd()` under the `config` parent. Read the file first to match the existing pattern.

**Step 6: Build + verify tests:**

```
go build ./...
go test ./internal/adapters/primary/cli/config -v
```

**Step 7: Manual smoke test:**

```
go run . config alias gtr gitlab.mydomain.com
go run . config alias --list
go run . config alias --unset gtr
```

**Step 8: Commit.**

```
git add internal/adapters/primary/cli/config/
git commit -m "feat(cli): add 'bossy config alias' set/list/unset commands"
```

---

### Task 9: Resolve `<alias>:<path>` in `normalizeDepArg`

**Files:**
- Modify: `internal/core/services/installer/utils.go`
- Modify: `internal/core/services/installer/utils_test.go`

**Step 1: Look at the existing `normalizeDepArg`** (`utils.go:32`). The new step must run **before** `ParseDependency` (the GitHub fallback) so an alias-prefixed argument is never misclassified as `owner/repo`.

**Step 2: Write tests** that mirror the table in `utils_test.go`. Cases:

- `"gtr:foo/bar"` with alias `gtr → gitlab.mydomain.com` and HostProtocols `gitlab.mydomain.com → ssh` → key `git@gitlab.mydomain.com:foo/bar`.
- Same alias with HostProtocols `gitlab.mydomain.com → https` → key `gitlab.mydomain.com/foo/bar`.
- Same alias with no protocol configured → defaults to HTTPS form (matches existing default-transport behavior; verify by reading `auth.Resolve` defaults).
- `"gtr:foo/bar.git"` → `.git` suffix stripped (existing behavior).
- `"git@host:path"` (SSH) unchanged — colon-after-prefix still parses as SSH, not as alias `git`.
- `"https://host/path"` unchanged.
- `"unknown:foo/bar"` (no alias for `unknown`) → currently invalid; should return `ok=false` rather than misclassify. Document this in the test.

**Step 3: Run — expect fail.**

**Step 4: Add the alias step.** Before the `if strings.HasPrefix(urlPart, "git@") || ...` block in `normalizeDepArg`, insert:

```go
if expanded, ok := tryExpandAlias(urlPart); ok {
	urlPart = expanded
}
```

Add helper at the bottom of `utils.go`:

```go
// tryExpandAlias rewrites "<alias>:<path>" into the canonical SSH or
// host/path form based on the user-config alias table and the host's
// configured protocol. Returns (expanded, true) on hit, ("", false)
// otherwise — including when the prefix is "git" / "http" / "https"
// (already a real URL form).
func tryExpandAlias(s string) (string, bool) {
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return "", false
	}
	name := s[:colon]
	if !consts.AliasNamePattern.MatchString(name) {
		return "", false
	}
	cfg := env.GlobalConfiguration()
	host, ok := cfg.Aliases[name]
	if !ok {
		return "", false
	}
	path := strings.TrimSuffix(s[colon+1:], ".git")
	if path == "" {
		return "", false
	}
	if cfg.HostProtocols[host] == "ssh" {
		return "git@" + host + ":" + path, true
	}
	return host + "/" + path, true
}
```

Add the necessary imports: `"github.com/basti-fantasti/bossy/pkg/consts"`, `"github.com/basti-fantasti/bossy/pkg/env"`.

**Step 5: Verify "unknown:..." case.** If `unknown` matches the alias pattern but isn't configured, `tryExpandAlias` returns `(_, false)` and the existing code path takes over — which will mis-parse it. Add a defensive check at the start of `normalizeDepArg`:

```go
if colon := strings.Index(raw, ":"); colon > 0 && !strings.HasPrefix(raw, "git@") &&
	!strings.Contains(raw[:colon], "/") && !strings.Contains(raw[:colon], ".") {
	prefix := raw[:colon]
	if consts.AliasNamePattern.MatchString(prefix) {
		if _, ok := env.GlobalConfiguration().Aliases[prefix]; !ok {
			return "", "", false
		}
	}
}
```

This rejects `unknown:foo/bar` before `@` version-suffix splitting confuses things.

**Step 6: Run tests:**

```
go test ./internal/core/services/installer -run TestNormalize -v
go test ./internal/core/services/installer -v
```

**Step 7: Commit.**

```
git add internal/core/services/installer/utils.go internal/core/services/installer/utils_test.go
git commit -m "feat(installer): expand <alias>:<path> to canonical host form"
```

---

### Task 10: Integration test — end-to-end SHA pinning round-trip

**Files:**
- Modify: `cmd/cmd_test.go` (or create a new file alongside it)

**Step 1: Look at the existing harness in `cmd/cmd_test.go`.** Identify the helpers it uses to scaffold a project + a fixture upstream repo. Mirror that style.

**Step 2: Add the test.** The flow:

1. Create a fixture upstream git repo with two commits: tag `v1.0.0` at commit A, tag `v1.1.0` at commit B (B is HEAD).
2. Create a project with `bossy.json` declaring the dep at `"^1.0.0"`.
3. Run `bossy install`. Assert lock has `Commit == B` (latest matching tag) and the resolved `Version` is `v1.1.0`.
4. Move tag `v1.1.0` upstream to a new commit C (simulate force-move).
5. Run `bossy install` again (no `update`). Assert checkout is still at B — the SHA wins.
6. Run `bossy update`. Assert `Commit` is now C.
7. Append a new test branch step: write a raw SHA (commit A) into `bossy.json`, run `bossy install`, assert checkout lands on A and `Commit == A`.

**Step 3: Add a negative test:** lock contains a SHA that does not exist in the upstream repo → `bossy install` exits non-zero with a clear error. No silent tag fallback.

**Step 4: Run:**

```
go test ./cmd -v -run TestSHAPinning
```

**Step 5: Commit.**

```
git add cmd/
git commit -m "test(cmd): end-to-end SHA pinning round-trip and force-move resilience"
```

---

### Task 11: Integration test — alias round-trip

**Files:**
- Modify: `cmd/cmd_test.go` or new sibling file.

**Step 1: Test flow.**

1. Set up a local fixture git host (or reuse an existing fixture pattern).
2. `bossy config alias gtr <fixture-host>`.
3. `bossy config git protocol <fixture-host> ssh` (if SSH fixture is available) — otherwise `https`.
4. `bossy install gtr:foo/bar@v1.0.0`.
5. Read the resulting `bossy.json`. Assert the dep key is the canonical form (`git@<host>:foo/bar` for SSH, `<host>/foo/bar` for HTTPS) — never `gtr:foo/bar`.
6. Assert the lock has both `Version: v1.0.0` and a populated `Commit`.

**Step 2: Run:**

```
go test ./cmd -v -run TestAlias
```

**Step 3: Commit.**

```
git add cmd/
git commit -m "test(cmd): alias round-trip stores canonical key, not alias form"
```

---

### Task 12: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/ci.md`

**Step 1: Update `README.md`.** Add a section under the existing "Versioning" docs explaining:

- `bossy.json` accepts a raw 7-40 char hex SHA as a version (e.g. `"git@gitlab.mydomain.com:gtr/gtrlib": "a1b2c3d"`).
- The lock pins by SHA after every install. `bossy install` reuses the pinned SHA; `bossy update` re-resolves and refreshes it.
- The `bossy config alias` family with three worked examples (set / list / unset).

Use the existing tone — short, example-led. No marketing.

**Step 2: Update `docs/ci.md`.** Add a "Reproducible builds" section:

- Commit `bossy-lock.json` alongside `bossy.json` in CI.
- Run `bossy install` (not `bossy update`) in CI.
- The SHA pin survives upstream tag moves and unreleased internal libraries.

**Step 3: Commit.**

```
git add README.md docs/ci.md
git commit -m "docs: SHA pinning, raw-SHA versions, host aliases"
```

---

### Task 13: Final verification

**Step 1:** Run the full test suite and lint:

```
make test
make test-style
```

Both must pass. Fix anything that fails before declaring done — see `superpowers:verification-before-completion`.

**Step 2:** Manual smoke against a real public repo:

```
go run . install github.com/HashLoad/horse@v1.6.4
```

Then inspect `bossy-lock.json`. Verify `commit` is populated. Run `bossy install` again — should be a no-op (lock has SHA).

**Step 3:** When ready for PR, use the `prepare-for-pr` skill.

---

## Skills to invoke during execution

- Before every commit: `git-config`
- Before every "task complete" claim: `superpowers:verification-before-completion`
- For TDD discipline within tasks: `superpowers:test-driven-development`
- Before opening the PR: `prepare-for-pr`
