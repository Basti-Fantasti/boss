# SHA Pinning and Host Aliases — Design

**Date:** 2026-05-20
**Status:** Validated, ready for implementation plan

## Problem

Two related gaps in bossy block its use as a reproducible-build dependency
manager on internal CI:

1. **No commit-SHA pinning.** The lock file (`bossy-lock.json`) stores a
   resolved tag/branch short name plus a worktree-content hash. The hash
   catches local tampering, not upstream mutation. If a git tag is
   force-moved, `bossy install` silently picks up new content. For
   internally-hosted libraries that are untagged (`gtrlib`, `extlib`),
   the only options today are mutable branches.

2. **No way to abbreviate non-GitHub dependencies.** The bare-name
   shortcut (`installer/utils.go:75-83`) hard-codes
   `github.com/hashload/...` for no-slash names and `github.com/...` for
   one-slash names. Users on internal GitLab must always spell out the
   full host/path.

## Goals

- Make `bossy install` deterministic across machines and CI when a
  lock file is present, by pinning to immutable commit SHAs.
- Let users define short aliases for their own git hosts so internal
  dependencies are as terse to type as GitHub ones, without polluting
  `bossy.json` with user-local config.

## Scope

**In scope:**

- New `Commit` field on `LockedDependency` recording the resolved git
  SHA after every checkout.
- `bossy install` (lock present) checks out by SHA. `bossy update`
  re-resolves constraints and refreshes the SHA.
- Raw SHAs accepted as version strings in `bossy.json` and on the
  command line (`bossy install <dep>@<sha>`).
- New `bossy config alias <name> <host>` family of commands.
- Alias syntax `<alias>:<path>` parsed in CLI args and in `bossy.json`
  keys. Manifest stores the **canonical** form
  (`git@<host>:<path>` or `<host>/<path>`); aliases never round-trip.
- Both git adapters (`git_embedded`, `git_native`) gain a
  `CheckoutHash` method.

**Out of scope:**

- Verifying SHA content beyond what git already guarantees.
- Migrating existing lock files. Old locks without `Commit` work
  unchanged until the next install refreshes them. No flag day.
- Per-project aliases. User-level only for v1. If needed later, the
  preferred path is an `"aliases"` block in `bossy.json` (manifest
  beats user config); no per-project config file.

## Data model

### `internal/core/domain/lock.go`

Add one field to `LockedDependency`:

```go
type LockedDependency struct {
    Name      string              `json:"name"`
    Version   string              `json:"version"`   // resolved tag/branch short name
    Commit    string              `json:"commit"`    // NEW: 40-char git SHA
    Hash      string              `json:"hash"`      // worktree fingerprint
    Artifacts DependencyArtifacts `json:"artifacts"`
    Failed    bool                `json:"-"`
    Changed   bool                `json:"-"`
}
```

`Version` stays as a human-readable label. `Commit` is the immutable
pin. Empty `Commit` triggers the legacy tag-resolution path.

### `internal/core/domain/package.go`

No changes. SHAs are stored as ordinary strings in `Dependencies`;
the resolver classifies them.

### `internal/core/services/auth/auth.go`

Add to `Configuration`:

```go
type Configuration struct {
    // ... existing fields
    Aliases map[string]string `json:"aliases,omitempty"` // alias → host
}
```

### `pkg/consts`

```go
// Alias names: lowercase, start with letter, no dots/colons/slashes.
// Disjoint from host names and from "git@" SSH prefix.
var AliasNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
```

## Resolver logic

### Input parsing — `installer/utils.go`

`normalizeDepArg` and `ParseDependency` gain an alias step before the
GitHub fallback:

1. Match `^([a-z][a-z0-9_-]*):(.+)$` and confirm prefix is not `git`,
   `http`, or `https`.
2. Look up the alias in `auth.Configuration.Aliases`.
3. On hit: expand to `git@<host>:<path>` when
   `HostProtocols[host] == "ssh"`, else `<host>/<path>`. The existing
   protocol table determines transport — aliases only encode *where*.
4. On miss: fall through to current bare-name / one-slash behavior.

### Version resolution — `installer/core.go:getVersion`

```go
func (ic *installContext) getVersion(dep domain.Dependency, repo *goGit.Repository) *plumbing.Reference {
    // 1. SHA in bossy.json — terminal
    if isGitSHA(dep.GetVersion()) {
        return plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(dep.GetVersion()))
    }

    // 2. Locked SHA — terminal when useLockedVersion
    if ic.useLockedVersion {
        locked := ic.rootLocked.GetInstalled(dep)
        if locked.Commit != "" {
            return plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(locked.Commit))
        }
        // legacy: tag-by-version fallback for old locks
    }

    // 3. existing semver resolution path
}
```

`isGitSHA` accepts 7-40 lowercase hex characters; short SHAs are
canonicalized after checkout.

### Checkout — `installer/core.go:checkoutAndUpdate`

After successful `git.Checkout` (or new `git.CheckoutHash`):

1. Read `repository.Head().Hash().String()` to get the 40-char SHA.
2. **Skip `git.Pull` when checking out a SHA.** Detached-HEAD pull
   would error; pinning means we do not want updates.
3. Pass the SHA to `lockSvc.AddDependency(lock, dep, version, commit, modulesDir)`.

### Git port — `internal/core/ports`

Extend the `Git` port with:

```go
CheckoutHash(dep domain.Dependency, hash plumbing.Hash) error
```

Implementations:

- `git_embedded.go` — `worktree.Checkout(&CheckoutOptions{Hash: hash})`
- `git_native.go` — `git checkout --detach <sha>`

### Lock service — `internal/core/services/lock/lock_service.go`

`AddDependency` signature extends from
`(lock, dep, version, modulesDir)` to
`(lock, dep, version, commit, modulesDir)`. The domain-level
`PackageLock.AddDependency` (legacy direct path) keeps its old
signature; new code uses the service.

## CLI surface

```
bossy config alias <name> <host>      # set
bossy config alias --list             # show all
bossy config alias --unset <name>     # remove
```

Implementation mirrors `protocolCmd` in `config/git.go:24` — mutates
`env.GlobalConfiguration().Aliases`, calls `SaveConfiguration()`.
Validates name against `AliasNamePattern`. Refuses to set an alias
whose name collides with an existing host in `HostProtocols`.

**Existing commands — contract unchanged, behavior extended:**

- `bossy install <alias>:<path>` — alias-aware via section above.
- `bossy install` (no args) — uses lock SHA when present; falls back
  to tag for old locks.
- `bossy update` — re-resolves constraints, refreshes both `Version`
  and `Commit`. The only way a locked SHA moves.
- `bossy install <dep>@<sha>` — falls out of existing `@` parsing
  combined with the new SHA fast-path.

**Output:**

- Checkout-by-SHA log line: `📌 <name> pinned to <sha[:7]>`.
- `bossy dependencies` listing shows the resolved SHA alongside the
  version.

## Edge cases

1. **Shallow clone missing the SHA.** `boss config git shallow true`
   user pins a SHA older than the shallow depth. On `CheckoutHash`
   "object not found", retry with a deepening fetch:
   - native: `git fetch --unshallow` then retry
   - embedded: `Fetch` with `Depth: 0` then retry

   If still missing, `msg.Die` with a message naming the dep and SHA.

2. **Short SHA in `bossy.json`.** Both go-git and native git accept
   short SHAs; after checkout we read `repository.Head().Hash()` to
   obtain the canonical 40-char SHA for the lock.

3. **Locked SHA no longer exists upstream** (force-push, branch
   deleted, repo rewritten). Fatal error, no tag fallback.
   Reproducibility requires failing loudly.

4. **Mixed lock** (some entries with `Commit`, some without). Per-entry
   branching in `getVersion` handles it; no global flag day.

5. **Alias collision with a real host.** `config alias` refuses if the
   name matches a host already in `HostProtocols`. The reverse —
   adding `config git protocol <host>` for a host that shadows an
   alias — should warn but proceed; the alias becomes unreachable
   until renamed.

6. **`bossy update` on an alias-keyed dep.** User installed via
   `gtr:foo/bar`; bossy.json stores `git@gitlab.mydomain.com:foo/bar`.
   Update reuses the canonical key; no alias re-expansion. Aliases
   are write-only.

7. **Submodules.** Existing `initSubmodules` (`git.go:47`) runs after
   checkout. Submodule SHAs are determined by the parent repo's tree
   at the pinned commit. No additional lock fields needed.

## Testing

**Unit:**

- `normalizeDepArg` alias-expansion table: hit, miss, name-collision
  with `git@`, name-collision with `http(s)://`.
- `isGitSHA` boundary cases: 6 chars (reject), 7 chars (accept), 40
  chars (accept), 41 chars (reject), uppercase hex (reject), non-hex.
- `getVersion` SHA fast-path: raw SHA in version, locked SHA, locked
  entry without `Commit` (legacy fallback).
- `checkoutAndUpdate` writes the SHA into the lock and skips pull on
  SHA checkout.
- `git_embedded` and `git_native` `CheckoutHash` parity using a
  table-driven test with a fixture repo.

**Integration** (cmd-level harness):

- Init project, install at tag, verify lock contains `Commit`; mutate
  the `bossy.json` constraint to a different range; reinstall; verify
  same SHA is reused.
- Lock contains a SHA absent from the upstream repo → install fails
  with non-zero exit and a clear message; no silent tag fallback.
- Alias round-trip: `config alias gtr gitlab.example.com` +
  `config git protocol gitlab.example.com ssh` +
  `install gtr:foo/bar` → manifest key is
  `git@gitlab.example.com:foo/bar`; lock has `Commit`.
- Shallow-clone deepening: shallow-clone a repo, pin to an old SHA
  outside the shallow depth, verify install succeeds after one retry.

## Backward compatibility

- Lock files without `Commit` continue to work via the tag-resolution
  fallback; first install/update writes `Commit` and they're upgraded.
- `bossy.json` files without `aliases` (today's state) are unchanged.
- No CLI flag removals or contract changes on existing commands.
