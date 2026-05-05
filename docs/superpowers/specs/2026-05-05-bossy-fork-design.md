# Bossy — Standalone Fork of Boss

**Date:** 2026-05-05
**Status:** Approved (awaiting implementation plan)
**Author:** Bastian Teufel

## Background

`boss` is the upstream Delphi/Lazarus dependency manager from HashLoad
(https://github.com/HashLoad/boss). Upstream maintenance has been slow and
several bugs that block our daily use are not getting fixed in time. We are
forking the project as `bossy` so we can ship fixes ourselves while keeping the
ability to cherry-pick upstream improvements.

The fork lives at https://github.com/Basti-Fantasti/bossy (renamed in place
from `Basti-Fantasti/boss`, preserving the GitHub fork relationship with
`HashLoad/boss`).

## Goals

1. Ship a standalone, rebranded `bossy` binary whose self-update fetches from
   our fork's GitHub releases — no dependency on the upstream repo at runtime.
2. Fix SSH cloning so it works for `gitlab.mydomain.com` and any other self-hosted
   Git server, with the ergonomics a developer expects from `git` itself.
3. Make `bossy install` "just work" inside GitLab CI jobs without per-host
   setup, using `CI_JOB_TOKEN`.
4. Keep the diff against upstream small and well-isolated, so cherry-picks
   from `HashLoad/boss` apply with minimal conflicts.

## Non-goals

- Re-architecting hexagonal layout, swapping CLI framework, or refactoring
  unrelated subsystems.
- Changing the `boss.json` manifest format in breaking ways.
- Changing the default org for bare-name installs (`bossy install horse`
  continues to resolve to `github.com/hashload/horse` because that is where
  the actual library code lives).
- Supporting non-GitLab self-hosted Git providers' CI auto-detection in this
  iteration. The generic `BOSSY_AUTH_<HOST>` env var covers them.

## Scope

### In scope

- Rename: binary, Go module path, release-asset prefix, self-update target,
  user-level config directory.
- New auth-resolution package replacing the scattered URL/protocol/credential
  logic across `domain.Dependency.GetURL`, `env.Configuration.GetAuth`, and
  the embedded/native git adapters.
- System-`git` shell-out for all SSH operations (hard fail if `git` is
  missing).
- Deletion of the existing `boss login`/`boss logout` commands and their
  underlying credential storage; replacement with a new `bossy auth`
  subcommand tree and a one-shot config migrator.
- New documentation: README rewrite of the auth section; new `docs/ci.md`
  covering the GitLab CI/CD scenario end-to-end including the job-token
  allowlist remediation.

### Out of scope

- Any feature work unrelated to the rebrand or SSH/CI fixes.
- Changing how Delphi compilation, library paths, or the lock file work.

## Design

### 1. Rebrand

| Aspect | Old | New |
|---|---|---|
| Binary name | `boss` | `bossy` |
| Go module path | `github.com/hashload/boss` | `github.com/basti-fantasti/bossy` |
| Self-update org/repo | `HashLoad/boss` | `Basti-Fantasti/bossy` |
| Release asset name | `boss-<os>-<arch>.{zip,tar.gz}` | `bossy-<os>-<arch>.{zip,tar.gz}` |
| User config dir | `~/.boss/` | `~/.bossy/` (with one-shot migration from `~/.boss/`) |
| Default org for bare-name install | `github.com/hashload/` | unchanged (libraries still live upstream) |

Mixed-case `Basti-Fantasti` in the GitHub URL is preserved for HTTP fetches
(GitHub is case-insensitive); the Go module path is lowercased per
convention.

All rebrand-only constants are consolidated into `pkg/consts/branding.go`
so the merge surface for trivial upstream-version-bump cherry-picks is a
single file.

The Makefile, CI workflows (`.github/workflows/ci.yml`,
`release.yml`, `pre-commit.yml`), and `scripts/` are updated to reference
the new binary name and module path.

### 2. Auth resolution chain

A new package `internal/core/services/auth` owns protocol and credential
selection. Single entry point:

```go
// Resolve returns how bossy should clone/fetch the given dependency,
// taking host, environment, per-host config, and stored credentials into
// account.
func Resolve(dep domain.Dependency) (Decision, error)

type Decision struct {
    URL         string          // fully-formed clone URL
    Transport   Transport       // SSH (system git) | HTTPS (go-git)
    Credential  CredentialSpec  // populated for HTTPS-with-token paths
}
```

The resolution chain, evaluated top-down per host. The first matching layer
wins, except where noted.

1. **GitLab CI auto-detection (highest priority).** If `GITLAB_CI=true`,
   `CI_SERVER_HOST` is set, `CI_JOB_TOKEN` is set, and the dependency host
   matches `$CI_SERVER_HOST`, rewrite the clone URL to
   `https://gitlab-ci-token:${CI_JOB_TOKEN}@<host>/<path>` and use the HTTPS
   (go-git) transport. **This intentionally overrides per-dependency
   explicit URLs**, so a developer can declare a dep as
   `git@gitlab.mydomain.com:foo/bar.git` for local SSH use and the same manifest
   still works in CI without any branching. The SSH-form path
   (`foo/bar.git`) is normalized to the HTTPS-form path (`foo/bar`) by
   stripping the `.git` suffix and replacing the `:` separator with `/`.
   If `GITLAB_CI=true` but `CI_SERVER_HOST` or `CI_JOB_TOKEN` is missing or
   empty, this layer is skipped (do not produce a half-formed URL); fall
   through to the next layer.
2. **Generic env-var override.** `BOSSY_AUTH_<HOST_UPCASED_UNDERSCORED>`,
   format `<scheme>:<value>`. Schemes:
   - `https-token:<user>:<token>` → HTTPS with basic auth
   - `ssh` → force system-git SSH
   Lets non-GitLab CI runners or one-off builds override behavior without
   touching config files. Sits below GitLab CI auto-detection so CI users
   don't need to set anything, but above per-dep declarations so an
   operator can override even an explicit URL (e.g. forcing SSH when the
   manifest says HTTPS).
3. **Per-dependency explicit URL.** If the `boss.json` key is a full URL
   (`git@gitlab.mydomain.com:foo/bar.git` or `https://...`), it is used as-is for
   protocol selection. The cache hash is computed from the canonical
   `host/path` form so the same logical repo doesn't get cached twice when
   declared in different forms.
4. **Per-host config** from `bossy config git protocol <host> ssh|https`,
   stored in `~/.bossy/config.json`. Selects protocol; credentials come from
   ssh-agent / system git for SSH, or from the next layer for HTTPS.
5. **Stored HTTPS credentials** from `bossy auth set <host>` (replaces
   `boss login`). Used only for HTTPS basic-auth.
6. **Default.** HTTPS, no auth. Works for public repos.

### 3. SSH transport — shell out to system `git`

For any decision with `Transport: SSH`, bossy invokes the system `git`
binary for `clone`, `fetch`, `pull`, and tag enumeration, instead of
go-git. Rationale:

- Transparently honors `~/.ssh/config`, ssh-agent, `IdentityFile`,
  `IdentitiesOnly`, hardware keys, agent forwarding, and `known_hosts`.
- Eliminates the brittle "load one PEM file" code path in
  `pkg/env/configuration.go:GetAuth`.
- Matches developer mental model — if `git clone git@gitlab.mydomain.com:x/y.git`
  works in the shell, `bossy install` will work too.

If `git` is not on `PATH` and an SSH operation is requested, bossy fails
with:

```
SSH cloning requires `git` to be installed and on PATH.
Dependency <name> resolved to SSH transport (host: <host>).
Either install git, or change the dependency to an HTTPS URL.
```

HTTPS operations continue to use go-git (the embedded path), since
go-git's HTTPS+BasicAuth support is well-trodden and avoids a hard
dependency on system `git` for the public-deps-only case.

`bossy config git mode embedded|native` is removed — protocol now drives
the implementation choice automatically. (The setting was always poorly
understood and is no longer needed.)

### 4. Manifest changes (non-breaking)

`boss.json` continues to accept the existing flat `"deps": { "<key>": "<version>" }`
format. The change is purely in **what `<key>` is allowed to be**:

| Form | Behavior |
|---|---|
| `horse` | Resolves to `github.com/hashload/horse` (unchanged). |
| `gitlab.mydomain.com/foo/bar` | Existing form. Protocol from auth chain. |
| `git@gitlab.mydomain.com:foo/bar.git` | **NEW.** Treated as an explicit SSH URL; `.git` suffix stripped for canonical form; layer-1 protocol decision is SSH. |
| `https://github.com/x/y` | **NEW.** Treated as an explicit HTTPS URL. |

The undocumented `:ssh` suffix in the version string (e.g. `"foo": "*:ssh"`)
is removed. The migrator (see §6) rewrites any such entries to full SSH
URLs in the user's `boss.json`.

The `git@host:repo` URL parsing bug
(`installer/utils.go` regex producing malformed `https://gitlab.mydomain.com:foo/bar`)
is fixed by handling SSH-form URLs as a distinct case before falling
through to the `host/path` normalizer.

### 5. CLI changes

| Old command | New command | Notes |
|---|---|---|
| `boss login <host>` | `bossy auth set <host>` | HTTPS user/pass only. SSH no longer needs registration. |
| `boss login <host> --ssh ...` | _(removed)_ | SSH uses system git + agent. Replace usage with system `ssh-agent`/`~/.ssh/config`. |
| `boss logout <host>` | `bossy auth rm <host>` | |
| _(none)_ | `bossy auth list` | Lists hosts with stored HTTPS creds. |
| `boss config git mode ...` | _(removed)_ | Protocol drives transport. |
| _(none)_ | `bossy config git protocol <host> ssh\|https` | Per-host protocol default. |
| `boss config cache rm` | `bossy config cache rm` | Unchanged behavior. |

The removed commands print a one-line migration hint pointing at the new
command and exit non-zero, for one release cycle.

### 6. Config migration

On startup, if `~/.boss/` exists and `~/.bossy/` does not, `bossy` runs a
one-shot migrator (silent for the user; logged at debug level):

1. Copy `~/.boss/` to `~/.bossy/`.
2. Convert any `auth` entries with `UseSSH: true` to a no-op (SSH no longer
   stores per-host config).
3. Walk the current project's `boss.json`; if any version string contains
   `:ssh`, rewrite the dep key to a `git@host:path` form and drop the
   `:ssh` suffix. Print a one-line summary of changes made.

Idempotent — re-running on an already-migrated home does nothing.

### 7. Self-update

`internal/upgrade/upgrade.go` constants change to:

```go
const (
    githubOrganization = "Basti-Fantasti"
    githubRepository   = "bossy"
)
```

`getAssetName()` already uses `runtime.GOOS`/`runtime.GOARCH`; only the
binary-name prefix changes (`bossy-` instead of `boss-`).

The `release.yml` workflow is updated so released artifacts match the new
asset names.

## Testing strategy

### Unit tests

- `internal/core/services/auth`: table-driven tests for the resolution
  chain — one row per precedence level, one row per layer-skip case.
- Manifest URL parser: cases for bare names, `host/path`, `git@host:path`,
  `git@host:path.git`, `https://host/path`, `https://host/path.git`.
- Migrator: idempotency, `:ssh` rewriting, no-op on fresh installs.
- CI rewriter: behaves correctly when `GITLAB_CI` is unset, when set with
  missing `CI_SERVER_HOST` or `CI_JOB_TOKEN` (must not produce a
  half-formed URL), and when host doesn't match `CI_SERVER_HOST` (must
  fall through to next layer).

### Integration tests

Provided a real internal test repo on `gitlab.mydomain.com`:

- `bossy install` against the test repo via SSH from a developer
  workstation (system `git`, ssh-agent loaded with the user's key).
- Same install simulating a CI run:
  `GITLAB_CI=true CI_SERVER_HOST=gitlab.mydomain.com CI_JOB_TOKEN=<token> bossy install`,
  asserting the URL was rewritten and clone succeeded with a fresh cache.
- 403 path: same as above with a deliberately scope-restricted token,
  asserting the user-friendly remediation message is printed.

### Manual / smoke

- `bossy upgrade` against a freshly tagged release of the fork.
- `bossy upgrade --dev` against a pre-release.
- Running `boss login` (legacy) prints the migration hint and exits non-zero.
- `bossy install horse` still resolves to `github.com/hashload/horse`.

## Documentation deliverables

### README

Rewrite the "Login" section as "Authentication". Cover:

- Public repos: nothing to do.
- Private HTTPS: `bossy auth set <host>` once.
- Private SSH: ensure `git` and ssh-agent work in your shell — bossy
  inherits from them.
- Per-dep explicit URLs (the new manifest forms).

Remove the `boss config git mode` section. Add a short note about the
`BOSSY_AUTH_<HOST>` escape hatch.

### `docs/ci.md` — new file

Required content:

1. **The 30-second story.** Minimal `.gitlab-ci.yml` that works
   out-of-the-box for repos on the same GitLab instance — no auth setup.
2. **What bossy does in CI.** Explain that `GITLAB_CI=true` +
   `CI_SERVER_HOST` + `CI_JOB_TOKEN` triggers automatic HTTPS+token
   rewriting, and that this overrides any SSH form the dependency was
   declared with. So local devs can use SSH, CI uses HTTPS, same
   `boss.json`, no configuration drift.
3. **Troubleshooting: 403 from GitLab.** This is the section the user
   specifically asked to be well-documented. Must spell out:
   - Symptom: `bossy install` fails with `403` or "remote rejected" on a
     repo that exists.
   - Cause: GitLab restricts `CI_JOB_TOKEN` so a job in **project A** can
     only clone **project B** if **project B has explicitly added project
     A to its CI/CD job-token allowlist** (since GitLab 15).
   - Fix, with click-path:
     1. Open project B (the *dependency* repo) in GitLab.
     2. Settings → CI/CD → Job token permissions.
     3. Under "Allow access to this project with a CI_JOB_TOKEN", add
        project A (the *consumer* repo) to the allowlist.
     4. Save. Re-run the failing pipeline.
   - For a monorepo of N internal libraries pulled by M apps, each
     library needs each app added once. (Or, if your security model
     allows it, switch the library to "All groups and projects" — note
     the security tradeoff.)
   - GitLab docs link:
     https://docs.gitlab.com/ee/ci/jobs/ci_job_token.html
4. **Troubleshooting: other failures.** The error-message catalog
   (missing git, missing CI vars, no creds outside CI).
5. **Non-GitLab CI runners.** Use `BOSSY_AUTH_<HOST>` to inject a token
   or force SSH. Worked example for a generic shell runner.
6. **Runner image checklist.** What needs to be installed: `git`, `bossy`,
   the Delphi compiler. Note that ssh-agent / SSH keys are *not* needed
   for the GitLab auto-detection path.

The 403 troubleshooting must be discoverable: linked from the README, and
the actual error message bossy prints includes a pointer to it
(`See docs/ci.md for the fix.`).

## Cherry-pick hygiene

To keep merging upstream tractable:

- All branding strings live in `pkg/consts/branding.go`. Cherry-picks
  that change unrelated code rarely touch this file.
- `internal/core/services/auth` is a new package; upstream has nothing
  there to conflict with.
- Existing `Dependency.GetURL()` keeps its signature; its body becomes a
  thin call into the auth package. Upstream changes to `GetURL` semantics
  may need attention but the surface is small.
- The git adapter files (`git_embedded.go`, `git_native.go`) keep their
  exported function signatures so upstream behavior tweaks still apply.
- Removed CLI files (`login.go`, the embedded/native mode-toggle) leave
  stub command files that print migration hints, so upstream additions
  to those commands surface as a clean conflict rather than silently
  resurrecting removed behavior.

## Open questions

None. Implementation can proceed once a written plan is in place.

## Risks

- **Cherry-pick pain if upstream rewrites the auth code.** Mitigated by
  isolating our auth package and keeping `GetURL` shaped the same. If
  upstream does a major refactor we re-do the integration once.
- **`CI_JOB_TOKEN` allowlist toil for users.** Not a bossy bug, but the
  documentation must make the fix obvious. Covered in `docs/ci.md`.
- **System `git` version differences in CI runner images.** The
  shell-out path uses only standard `git clone`/`fetch`/`tag` — any git
  ≥2.x is fine. Documented in the runner-image checklist.
