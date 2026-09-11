# Changelog

All notable changes to the **Bossy** fork are documented here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project itself does not yet
track formal release versions, so changes are grouped under `Unreleased` until a tag is cut.

Bossy is a fork of [HashLoad/boss](https://github.com/HashLoad/boss). This file only
lists changes introduced **in the fork** (commits after upstream `d722ebf`). For prior
history, see the upstream repository.

## [Unreleased]

### Added

- A catalog of predefined dependencies, and `bossy add` to pick from it. Adding a
  dependency no longer means typing its repository URL, its branch and its search-path
  restriction by hand — the catalog carries all three and a project picks by name. The
  interactive picker is a filterable list with a detail pane offering each entry's
  branches and tags, listed from the remote without cloning and only for the entry you
  open. `bossy add --tag <tag>` and `bossy add <id>...` do the same non-interactively;
  without a terminal and without either, the command refuses rather than waiting on a
  keystroke that will never arrive in CI.
- `bossy preset` manages that catalog: `list`, `show`, `add`, `update`, `rm`, `import`
  and `sync`. Two catalogs are merged — a shared one pulled from a git repository by
  `sync`, and a local one holding your own entries — with local entries shadowing
  shared ones by id. Client-side writes only ever touch the local catalog, so
  contributing a shared entry goes through a merge request on the catalog repository.
  A sync validates the whole downloaded catalog before writing, so a repository with
  one bad entry cannot leave a machine with no catalog at all.
- An opt-in `remote` submodule policy, for dependencies whose submodules are meant to
  track their branch tips rather than the commits the superproject records —
  `git submodule update --remote`, which bossy previously had no equivalent of.
  Declared per preset with `bossy preset add --submodules remote` and carried into
  `bossy.json`, since CI installs from the manifest and never reads the catalog.
  Floating submodules and a lock file pull in opposite directions, so the two are
  split: resolving advances the submodules once and records the resulting commits in
  `bossy-lock.json`, and an install replaying a lock checks those commits out instead
  of chasing the tips again. `bossy update` re-resolves. Implemented on both git
  backends — `--remote` natively, and by resolving each submodule's branch tip
  directly under go-git, which has no equivalent flag. A commit read back from a lock
  is refused unless it is a full hex SHA, since it reaches a git command line.
- Remote branches and tags can be listed without cloning on both git backends
  (`ls-remote` natively, an in-memory storer under go-git). Both apply the same
  `refs/heads` and `refs/tags` allowlist, so neither offers a server-advertised
  `refs/pull/*` or `refs/merge-requests/*` entry as a pin.

### Changed

- `bossy install` no longer accepts `add` as an alias. That verb is now a command of
  its own taking preset ids rather than repository keys, and keeping the alias would
  make `bossy add dmvcframework` mean two different things. `bossy install` and its
  `i` alias are unchanged; an argument to `bossy add` that still looks like a
  repository is recognised and pointed back at `bossy install`.
- The environment variables are now `BOSSY_HOME` and `BOSSY_GIT_SHALLOW`, matching the
  binary's name. `BOSS_HOME` and `BOSS_GIT_SHALLOW` are still read as a fallback and the
  new names take precedence, because these are set in CI job definitions and build-server
  configurations that do not live in this repository.

### Fixed

- A declared version that is exactly the name of a branch or tag now resolves to that
  ref instead of being read as a semver constraint first. zeoslib's development branch is
  called `8.0-patches`, which semver reads as 8.0 with the prerelease `patches`, so the
  constraint path won and settled on the nearest match: the unrelated branch
  `8.0.0-stable`, a commit from 2024. Nothing warned; `bossy-lock.json` simply recorded a
  different branch than `bossy.json` declared. Ranges are untouched, since no ref can be
  named `>0.0.0` or `^3.0.0`, and a bare `1.2` is normalised to `1.2.0` before it gets
  here. A branch pin no longer produces the `Version constraint 'main' not supported`
  warning either: the name is recognised before anything tries to parse it as a range.
- The `remote` submodule policy works at all. Every `git submodule` command ran in
  `modules/<name>`, which carries no `.git` of its own because a dependency's git
  directory lives in the cache, so each one failed with "not a git repository" and the
  policy silently degraded to a warning on every install. The pointer linking the two is
  now restored for the duration of the submodule work and removed again afterwards. The
  tests missed this because their fixture was a plain repository; there is now one
  reproducing bossy's separate-git-dir layout.
- A dependency using the `remote` policy no longer breaks every other project that
  depends on it. Advancing a submodule leaves the superproject dirty, and the git
  directory holding that state is shared across projects, so the next install elsewhere
  aborted with "worktree contains unstaged changes". The advanced gitlinks are staged,
  which is what makes the deliberate move indistinguishable from a clean tree.
- A submodule that cannot be initialised no longer fails the install. go-git works
  against an in-memory worktree in the cache path, where a configured submodule with
  nothing on disk behind it reports unstaged changes; that cost the dependency, not just
  its submodules.
- A dependency already sitting at its locked commit still has its submodule policy
  applied. The skip decision looks at the superproject's commit, which says nothing
  about where its submodules are, so a tree populated before the policy was declared
  stayed stale indefinitely.
- `bossy preset list` measures the reference column instead of assuming 22 characters,
  so a long branch name no longer pushes the repository column out of line for every
  other row.
- Project files keep the line endings they already had. `.dproj` and `.lpi` files are
  parsed as XML, and the XML spec requires a parser to fold CRLF into LF, so writing the
  document back rewrote every line of a project file saved by the Delphi IDE. Beyond the
  noise in `git status`, it broke CI: with `core.autocrlf=true` the runner checks the
  project file out as CRLF, `bossy install` wrote it back as LF, and a build step
  comparing the bytes before and after the install reported the committed project file
  as out of date. `git diff` could not see it, because git normalises line endings
  before comparing.
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

- Directory matching for search-path generation compared unescaped dots, so any
  extensionless file whose name merely ended in the letters of an extension —
  zoneinfo data such as `Buenos_Aires`, for one — pulled its whole directory
  onto the Delphi search path.

- On Windows, bossy's `GIT_SSH_COMMAND` default silently changed which `ssh`
  ran. Git executes that variable through its bundled shell, whose PATH puts
  Git's own `usr/bin` first, so the bare word `ssh` resolved to the ssh inside
  Git for Windows no matter what the process PATH said — while git left to
  itself resolves `ssh` against the process PATH, which usually finds
  Microsoft's `C:\Windows\System32\OpenSSH`. The two are linked against OpenSSL
  and LibreSSL respectively and do not accept the same private keys, so a key
  in the legacy PEM format turned into `error in libcrypto: unsupported` and
  `Permission denied (publickey)` under bossy while `git clone` and `ssh` by
  hand kept working. The default now names the ssh git itself would have run,
  resolved against the same PATH.
- A wrapper configured through git's `core.sshCommand` is no longer overridden
  by bossy's default; `BatchMode=yes` is appended to it instead.

### Added — Search path scoping

- `bossy.json` gains an optional `searchpaths` map restricting which
  directories of a dependency reach the compiler search path. Without it bossy
  walks a dependency's whole tree, which for a repository shipping samples and
  unit tests alongside its library (delphimvcframework contributes roughly 190
  directories) can push `dcc`'s command line past the Windows 32767-character
  limit and fail the build with `MSB6003 ... The filename or extension is too
  long`. Keys accept either the full dependency reference or the bare module
  name; paths are relative to the module directory and still walked
  recursively.

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
