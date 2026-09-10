# Bossy in CI/CD

This document covers running `bossy install` inside a CI runner. The
common case — GitLab Runner pulling private dependencies from the same
GitLab instance — needs zero configuration.

## One-time setup on a Windows build server

A shell runner needs bossy installed once on the machine, under the
account the runner service uses. Everything below is done as that
account — usually not the account you log in with.

**1. Copy the binary.** Put `bossy.exe` somewhere on PATH for the runner
account, for example `C:\tools\bossy\bossy.exe`. There is no installer
and nothing to register.

**2. Confirm the runner account can see Delphi.**

```powershell
C:\tools\bossy\bossy.exe config delphi list
```

Expected output is one line per installed platform:

```
Installations found:
  [0] 37.0 (Win32) (current)
  [1] 37.0 (Win64)
```

This is the step worth doing carefully. Delphi registers itself under
`HKEY_CURRENT_USER`, so an installation visible to your desktop session
is invisible to the runner account. An empty list here means bossy cannot
compile packages in CI, and the failure will otherwise surface much later
as a confusing build error.

The first run also installs bossy's `bpl-identifier` helper into
`%USERPROFILE%\.bossy`, so it needs network access to github.com. A
`Failed to save build order` warning during that first run is harmless.

**3. Nothing else.** In particular:

- No `bossy auth set`. GitLab CI credentials are detected from the job
  environment — see the next section.
- No SSH keys, no `ssh-agent`, no `~/.ssh/config`, even for dependencies
  declared as `git@gitlab.mydomain.com:...`.
- No global `bossy init`. Dependencies are resolved per project, in the
  job's checkout directory.

The dependency cache lands in `%USERPROFILE%\.bossy\cache` and persists
between jobs, which is what you want — see [Shell runners](#shell-runners).

### Upgrading bossy on the server

Replace `bossy.exe` and run `bossy config delphi list` again. The cache
format is stable across fork versions; there is nothing to migrate and no
need to clear `%USERPROFILE%\.bossy`.

## GitLab CI: zero-config

A minimal `.gitlab-ci.yml` that builds a Delphi project with private
dependencies on the same GitLab instance:

```yaml
build:
  image: registry.mydomain.com/devops/delphi-bossy:latest   # contains git, bossy, Delphi
  script:
    - bossy install
    - bossy run build
```

That's the entire CI auth story. No `before_script` SSH dance, no key
management, no extra env vars to set.

### How it works

GitLab Runner injects three env vars into every job:
- `GITLAB_CI=true`
- `CI_SERVER_HOST=<your gitlab host>`
- `CI_JOB_TOKEN=<short-lived token>`

When `bossy install` resolves a dependency whose host matches
`CI_SERVER_HOST`, it rewrites the clone URL to plain HTTPS and supplies
`gitlab-ci-token:${CI_JOB_TOKEN}` as basic-auth credentials for that
request. The token is never part of the URL. This applies regardless of
how the dependency was declared in `bossy.json`:

| Declared as | In CI, cloned from |
|---|---|
| `git@gitlab.mydomain.com:foo/bar.git` | `https://gitlab.mydomain.com/foo/bar` |
| `gitlab.mydomain.com/foo/bar` | `https://gitlab.mydomain.com/foo/bar` |
| `https://gitlab.mydomain.com/foo/bar` | `https://gitlab.mydomain.com/foo/bar` |

So the same `bossy.json` works for local SSH development and CI HTTPS
without any branching logic.

Because the token stays out of the URL, it is not written into the
dependency cache's `.git/config` on disk, and an expired token from an
earlier job can never be reused. Caches that still carry a token baked in
by an older bossy are scrubbed on the next update.

Public dependencies (e.g. `github.com/HashLoad/horse`) clone over plain
HTTPS — `CI_JOB_TOKEN` is not used for them.

## Reproducible builds

Commit `bossy-lock.json` alongside `bossy.json`. The lock records the
resolved git commit SHA for every dependency, so:

- In CI, run `bossy install` (not `bossy update`). `install` consumes the
  lock, checks out each dependency at the pinned commit in detached HEAD,
  and skips the pull.
- Force-pushed upstream tags do not change what CI installs; the SHA in
  the lock is immutable.
- Internal libraries that don't tag releases (branch-only workflows) are
  still reproducibly buildable, because the lock pins the branch tip's SHA
  as of the last run that resolved it — the first `bossy install`, or the
  most recent `bossy update`.

```yaml
build:
  image: registry.mydomain.com/devops/delphi-bossy:latest
  script:
    - bossy install      # uses bossy-lock.json — reproducible
    - bossy run build
```

> Warning: `bossy update` re-resolves every constraint against the remote
> and rewrites the lock. Run it locally, intentionally, when you want to
> pick up new upstream versions — never as part of a normal CI build.

Shallow clone (`bossy config git shallow true`, or `BOSS_GIT_SHALLOW=1`)
is safe to combine with this. All branch tips are fetched, so
branch-pinned dependencies still resolve, and a SHA older than the
shallow cut-off triggers an automatic deepening fetch. Caches created by
earlier bossy versions — which restricted the remote refspec to a single
branch — are repaired on the next `bossy install`.

### Locks without commit SHAs

A lock entry written by an older bossy has no `commit` field. Such an
entry is installed rather than skipped, since a dependency missing from
`modules/` is never treated as already installed, and the lock is
rewritten with the SHA it resolved to. The pipeline builds, but not
reproducibly: until an entry carries a `commit` it pins a version string,
which is not enough to replay a branch pin. Run `bossy update` once
locally and commit the rewritten lock so every entry carries a SHA before
a pipeline depends on it:

```sh
bossy update
git add bossy-lock.json && git commit -m "Record resolved commits in lock"
```

For the developer-side workflows that produce and change the lock, see
[`workflows.md`](workflows.md).

## Shell runners

The examples above use a Docker runner (`image:`), where every job starts
from a clean container and `~/.bossy/cache` is empty. On a shell runner
the cache directory lives on the machine and persists between jobs. That
is mostly a win: dependencies are fetched once and later jobs only update
them.

The historical hazard was the CI job token. An older bossy embedded it in
the clone URL, which git persisted into the cache; the next job then
fetched with the previous job's expired token and failed with `401` or
`403`. The token is now passed as a credential and the remote URL is
overridden on every fetch, so a persisted URL is never reused. No
`before_script` cache cleanup is needed.

## Troubleshooting

### `403 Forbidden` or "remote rejected" on a real repo

This is the common gotcha. Symptom:

```
❌ CI_JOB_TOKEN denied access to gitlab.mydomain.com/devops/some-lib.
   Add the calling project to the dependency project's CI/CD job-token allowlist:
     Settings → CI/CD → Job token permissions
```

**Cause:** Since GitLab 15, `CI_JOB_TOKEN` is scoped. A job in
**project A** can only clone **project B** if **project B has explicitly
added project A** to its CI/CD job-token allowlist. By default the
allowlist contains only the project itself.

**Fix:**

1. Open project B (the *dependency* repo, the one being cloned) in GitLab.
2. Go to **Settings → CI/CD → Job token permissions**.
3. Under **"Allow access to this project with a CI_JOB_TOKEN"**, add
   project A (the *consumer* repo, the one whose CI is failing) to the
   allowlist.
4. Save.
5. Re-run the failing pipeline.

If you have a monorepo of N internal libraries pulled by M apps, each
library needs each app added once — bookkeeping but a one-time cost.

If your security model allows it, GitLab also offers a per-project
toggle to allow access from any project under the same group. Use with
care: it removes the per-pair scoping.

GitLab docs: <https://docs.gitlab.com/ee/ci/jobs/ci_job_token.html>

### `SSH cloning requires git to be installed and on PATH`

The runner image does not have `git`. Install it (`apt-get install -y git`
in a Debian-based image, `apk add git` in Alpine) or switch the
dependency to an HTTPS URL.

### `no credentials configured for <host>` (outside CI)

Means none of the auth layers matched and the clone is failing. Either:
- Run `bossy auth set <host>` for HTTPS basic auth, or
- `bossy config git protocol <host> ssh` and ensure ssh-agent has your key.

## Non-GitLab CI runners

For Jenkins, GitHub Actions, Drone, etc., use the generic env-var
escape hatch:

```sh
# HTTPS with a token (recommended for CI)
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=https-token:gitlab-ci-token:$MY_CI_TOKEN

# Or force SSH (you'll need ssh-agent loaded too)
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=ssh
```

The env var name is `BOSSY_AUTH_<HOST>` with `.` and `-` in the host
replaced by `_` and the whole thing upper-cased. Examples:

| Host | Env var |
|---|---|
| `gitlab.mydomain.com` | `BOSSY_AUTH_GITLAB_MYDOMAIN_COM` |
| `github.com` | `BOSSY_AUTH_GITHUB_COM` |
| `git.example-corp.io` | `BOSSY_AUTH_GIT_EXAMPLE_CORP_IO` |

## Runner image checklist

Whatever base image your runners use, ensure it has:

- `git` (≥ 2.x) — required for SSH transports and recommended for HTTPS.
- `bossy` binary on PATH.
- The Delphi compiler if you build Delphi projects.
- **Not required:** `ssh-agent`, SSH keys, `~/.ssh/config` — none of
  these are needed for the GitLab CI auto-detection path. Add them only
  if you need to clone from a non-GitLab SSH host.
