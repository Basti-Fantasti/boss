# Bossy in CI/CD

This document covers running `bossy install` inside a CI runner. The
common case — GitLab Runner pulling private dependencies from the same
GitLab instance — needs zero configuration.

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
`CI_SERVER_HOST`, it transparently rewrites the clone URL to
`https://gitlab-ci-token:${CI_JOB_TOKEN}@<host>/<path>` and clones over
HTTPS. This applies regardless of how the dependency was declared in
`bossy.json`:

| Declared as | In CI, becomes |
|---|---|
| `git@gitlab.mydomain.com:foo/bar.git` | `https://gitlab-ci-token:TOKEN@gitlab.mydomain.com/foo/bar` |
| `gitlab.mydomain.com/foo/bar` | `https://gitlab-ci-token:TOKEN@gitlab.mydomain.com/foo/bar` |
| `https://gitlab.mydomain.com/foo/bar` | `https://gitlab-ci-token:TOKEN@gitlab.mydomain.com/foo/bar` |

So the same `bossy.json` works for local SSH development and CI HTTPS
without any branching logic.

Public dependencies (e.g. `github.com/HashLoad/horse`) clone over plain
HTTPS — `CI_JOB_TOKEN` is not used for them.

## Reproducible builds

Commit `bossy-lock.json` alongside `bossy.json`. The lock records the
resolved git commit SHA for every dependency, so:

- In CI, run `bossy install` (not `bossy update`). `install` consumes the
  lock and checks out each dependency at the exact pinned commit.
- Force-pushed upstream tags do not change what CI installs — the SHA in
  the lock is immutable.
- Internal libraries that don't tag releases (branch-only workflows) are
  still reproducibly buildable, because the lock pins the branch tip's
  SHA at the moment `bossy update` was last run.

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
