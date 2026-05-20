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
