# Changelog

All notable changes to the **Bossy** fork are documented here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project itself does not yet
track formal release versions, so changes are grouped under `Unreleased` until a tag is cut.

Bossy is a fork of [HashLoad/boss](https://github.com/HashLoad/boss). This file only
lists changes introduced **in the fork** (commits after upstream `d722ebf`). For prior
history, see the upstream repository.

## [Unreleased]

### Fixed

- `bossy install <pkg>` and `bossy update <pkg>` no longer wipe other installed modules
  and their lock entries. Post-install reconciliation now runs only on full-tree
  invocations; targeted runs touch only the requested dependencies.
- Compatibility check is skipped when a dependency has no `bossy.json`, instead of
  aborting the install.
- `requireGit` errors now include the offending dependency for diagnosis;
  `Checkout`/`Pull` are guarded against missing repositories.
- Cache hash is now canonical across hosts; corrected `Use` string and upgrade binary
  prefix.
- Dep keys in the form `git@host:path` and `https://…` are accepted by the installer.
- Global configuration is reloaded after `MigrateHome` so the first run after migration
  sees the new paths.
- SSH dependencies now discover all branches and tags through the system `git` binary.
  Previously go-git's SSH transport failed silently against hosts using `~/.ssh/config`
  aliases, custom keys or non-standard ports, and version resolution fell back to the
  main branch — so a dependency pinned to a non-default branch silently built the wrong
  branch. A ref-listing failure is now a hard error instead of a fallback.
- Shallow clones no longer restrict the remote refspec, so branch-pinned dependencies
  resolve correctly. Caches created by earlier versions are repaired on next use. Note
  that `fetch --unshallow` never recovered the missing branches — the refspec, not the
  depth, was the limitation.
- The GitLab CI job token is carried as a credential rather than embedded in the clone
  URL, so it is neither written to the dependency cache on disk nor reused after expiry.
  This fixes `401`/`403` failures on the second and subsequent jobs on shell runners,
  where the cache persists between jobs. Tokens already persisted by earlier versions
  are scrubbed on next update.
- The lock file was written to `bossy-lock.json` but read back from a hardcoded
  `boss.lock`, so it was never loaded. `bossy install` re-resolved every dependency
  against the remote exactly as `bossy update` does, and a committed lock had no effect
  on what was built.
- A dependency pinned to a raw commit SHA silently checked out `master`. Reference
  resolution discarded the resolved hash and carried only its name forward, so the
  checkout was handed the zero hash.
- `bossy update <dep>` destroyed the version declared in `bossy.json`: it overwrote the
  entry with `>0.0.0` and then rewrote that to the resolved tag, turning a branch pin
  into a tag range. An argument carrying no explicit `@version` now leaves the declared
  version alone.
- An already-installed dependency is skipped only when the worktree is at the locked
  commit, and never when `modules/<name>` is missing. A lock entry with no `commit`
  field previously decided this on version strings alone, so the first clean checkout
  against such a lock reported `Skipped already installed` and left nothing to build.
  A branch or raw-SHA pin is also no longer reported as an error on every install.
- `bossy update --select` had no effect on the dependencies picked from the checklist.
  `ForceUpdate` was produced as repository keys and matched against short module names,
  and the run replayed the lock instead of re-resolving. Selecting a dependency now does
  the same work as naming it on the command line.
- Uninstalling the last remaining dependency left its module directory and lock entry
  on disk, because reconciliation was skipped whenever nothing was left to install.

### Security

- Credentials embedded in a URL are redacted from git stderr before it reaches error
  messages or debug logs.
- Bossy's git invocations no longer inherit `GIT_TRACE*` or `GIT_CURL_VERBOSE`, which
  would otherwise dump credentials to stderr and to trace2 file sinks. Matching is
  case-insensitive — Windows environment variable names are case-insensitive, so a
  lowercase `git_trace` previously bypassed the filter.

### Changed — Git transport

- `GetVersions` in the git port now returns `([]*plumbing.Reference, error)`.
- `GIT_SSH_COMMAND` defaults to `ssh -o BatchMode=yes` when unset, so unattended runs
  fail fast rather than blocking on an ssh prompt. This overrides `core.sshCommand` from
  git config; export `GIT_SSH_COMMAND` to keep a custom wrapper in effect.
- Native ref listing is bounded by a 60-second timeout.

### Added — SHA pinning

- `LockedDependency.Commit` carries the resolved commit SHA so installs reproduce
  bit-for-bit even when branches move.
- `domain.IsGitSHA` classifies version strings as raw SHAs.
- Installer pins by SHA when the lock has a `Commit` field or the requested version is
  a raw SHA; shallow clones are deepened once if the pinned SHA is missing locally.
- `git.CheckoutHash` performs detached-HEAD checkout against an explicit SHA.

### Added — Host aliases

- `bossy.json` `aliases` map and `AliasNamePattern` allow short aliases for hosts.
- `bossy config alias set|list|unset` CLI subcommands manage aliases.
- Installer expands `<alias>:<path>` dependency keys to the canonical host form before
  resolving.

### Added — Layered auth

- New `auth` package with `Decision` / `Transport` types and a layered `Resolve`
  orchestrator. Layers, in priority order:
  - dep URL classifier (bare / host-path / `git@` / `https://`)
  - GitLab CI auto-detection
  - `BOSSY_AUTH_<HOST>` environment override
  - per-host protocol config
  - stored HTTPS credentials
- Git adapter dispatches on the resolved `Decision`: system `git` for SSH, go-git for
  HTTPS.
- `bossy auth set|list|rm` replaces the previous `boss login`/`boss logout` commands.
- `bossy config git protocol <host> <ssh|https>` replaces the global `config git mode`
  switch with a per-host setting.
- CI hint: a `CI_JOB_TOKEN` allowlist suggestion is surfaced on HTTP 403 when running
  under GitLab CI.

### Changed — Rebranding (boss → bossy)

- Go module renamed to `github.com/basti-fantasti/bossy`; binary renamed to `bossy`.
- Manifest file renamed `boss.json` → `bossy.json`; lock file renamed
  `boss-lock.json` → `bossy-lock.json` with a one-shot migration on first run.
- User home directory renamed `~/.boss` → `~/.bossy` via the same one-shot migration;
  `:ssh` dep-key suffix is rewritten as part of the migration.
- Release artifacts renamed `boss-*` → `bossy-*` in the build workflow.
- Self-update points at the fork's release feed.
- Version banner now reads "Bossy".

### Added — Misc

- `build.cmd` convenience build entry on Windows.
- `docs/ci.md` with a GitLab CI guide and 403 troubleshooting steps.
- Documentation for SHA pinning, raw-SHA versions, and host aliases.
- Design and implementation plan documents for the fork under `docs/plans/`.

[Unreleased]: https://github.com/basti-fantasti/bossy/compare/d722ebf...HEAD
