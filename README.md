# Bossy

![Boss][bossLogo]

[![Go Report Card][goReportBadge]][goReportLink]
[![GitHub release (latest by date)][latestReleaseBadge]](https://github.com/Basti-Fantasti/bossy/releases/latest)
[![GitHub Release Date][releaseDateBadge]](https://github.com/Basti-Fantasti/bossy/releases)
[![GitHub repo size][repoSizeBadge]](https://github.com/Basti-Fantasti/bossy/archive/refs/heads/main.zip)
[![GitHub All Releases][totalDownloadsBadge]](https://github.com/Basti-Fantasti/bossy/releases)
[![GitHub][githubLicenseBadge]](https://github.com/Basti-Fantasti/bossy/blob/main/LICENSE)
[![GitHub issues][githubIssuesBadge]](https://github.com/Basti-Fantasti/bossy/issues)
[![GitHub pull requests][githubPullRequestsBadge]](https://github.com/Basti-Fantasti/bossy/pulls)
[![GitHub contributors][githubContributorsBadge]](https://github.com/Basti-Fantasti/bossy?tab=readme-ov-file#-code-contributors)
![Github Stars][repoStarsBadge]

_Bossy_ is a fork of [Boss](https://github.com/HashLoad/boss) — an open source dependency manager for Delphi and Lazarus projects developed by HashLoad.

<!-- getting start with emoji -->

## 🚀 Getting started

Install bossy (see [Installation](#-installation)), then run this in a Delphi or
Lazarus project directory:

```sh
bossy init                  # writes bossy.json; add -q to take defaults
bossy install horse@3.3.5   # adds the dependency and resolves it
git add bossy.json bossy-lock.json
git commit -m "Add horse 3.3.5"
```

`bossy.json` is the manifest you edit. `bossy-lock.json` is generated and
records the commit each dependency was resolved to, which is what makes a later
`bossy install` reproduce the same sources. Commit both; ignore `modules/`.

Joining a project that already uses bossy takes one command:

```sh
bossy install
```

Task-oriented recipes — branch pins, updating a single dependency, switching a
dependency between a branch and a tag — are in
[`docs/workflows.md`](docs/workflows.md).

> Aside: the upstream Boss project has a Portuguese
> [Getting Started](https://medium.com/@matheusarendthunsche/come%C3%A7ando-com-o-boss-72aad9bcc13)
> article covering the same concepts. It predates this fork and uses the `boss`
> command name throughout.

## 📦 Installation

- Download [setup](https://github.com/Basti-Fantasti/bossy/releases)
- Just type `bossy` in the terminal
- (Optional) Install a [Boss Delphi IDE complement](https://github.com/hashload/boss-ide)

Or you can use the following the steps below:

- Download the latest version of [Bossy](https://github.com/Basti-Fantasti/bossy/releases)
- Extract the files to a folder
- Add the folder to the system path
- Run the command `bossy` in the terminal

## 📚 Available Commands

### > Init

Initialize a new project and create a `bossy.json` file. Add `-q` or `--quiet` to skip interactive prompts and use default values.

```shell
bossy init
bossy init -q
bossy init --quiet
```

### > Install

Install one or more dependencies with real-time progress tracking:

```shell
bossy install <dependency>
```

**Progress Tracking:** Bossy displays progress for each dependency being installed:

```
⏳ horse                          Waiting...
🧬 dataset-serialize              Cloning...
🔍 jhonson                        Checking...
🔥 redis-client                   Installing...
📦 boss-core                      Installed
```

The dependency name is case insensitive. For example, `bossy install horse` is the same as `bossy install HORSE`.

```shell
bossy install horse                        # HashLoad organization on GitHub
bossy install fake/horse                   # Fake organization on GitHub
bossy install gitlab.com/fake/horse        # Fake organization on GitLab
bossy install https://gitlab.com/fake/horse # Full URL
```

A version can be appended with `@`. It may be a semver constraint, a tag, a
branch name, or a commit SHA:

```shell
bossy install horse@3.3.5
bossy install "horse@^3.0.0"               # quote: ^ is special in some shells
bossy install horse@dev                    # branch
```

Called with no arguments, `bossy install` installs everything in `bossy.json` at
the commits recorded in `bossy-lock.json`. To move a dependency that is already
locked, use [`bossy update`](#-update) — `install` replays the lock rather than
re-resolving. See [`docs/workflows.md`](docs/workflows.md).

You can also specify the compiler version and platform:

```sh
bossy install --compiler=37.0 --platform=Win64
```

> Aliases: i, add

### > Uninstall

Remove a dependency from the project:

```sh
bossy uninstall <dependency>
```

> Aliases: remove, rm, r, un, unlink

### > Update

Re-resolve dependencies against their remotes, ignoring `bossy-lock.json`, and
rewrite the lock with the commits that come back.

```sh
bossy update                    # every dependency in bossy.json
bossy update horse              # one dependency, keeping its declared version
bossy update horse jhonson      # several
bossy update horse@dev          # redeclare the version, then resolve it
```

`bossy update <dep>` leaves the version in `bossy.json` untouched and only moves
the lock, so it is the way to follow a branch you are already pinned to.
Appending `@<version>` rewrites the manifest entry as well, which is how a
dependency is switched between a branch and a tag.

`bossy update --select` (or `-s`) opens an interactive checklist of the
dependencies in `bossy.json` and updates the ones you tick, leaving the rest
untouched. Its annotations compare version strings rather than commits, so a
branch pin always displays as `up to date`.

Recipes for the common cases are in [`docs/workflows.md`](docs/workflows.md).

> Aliases: up

### > Upgrade

Upgrade the Bossy CLI to the latest version. Add `--dev` to upgrade to the latest pre-release:

```sh
bossy upgrade
bossy upgrade --dev
```

### > Dependencies

List all project dependencies in a tree format. Add `-v` to show version information:

```shell
bossy dependencies
bossy dependencies -v
bossy dependencies <package>
bossy dependencies <package> -v
```

> Aliases: dep, ls, list, ll, la, dependency

### > Run

Execute a custom script defined in your `bossy.json` file. Scripts are defined in the `scripts` section:

```json
{
  "name": "my-project",
  "scripts": {
    "build": "msbuild MyProject.dproj",
    "test": "MyProject.exe --test",
    "clean": "del /s *.dcu"
  }
}
```

```sh
bossy run build
bossy run test
bossy run clean
```

### > Authentication

Bossy uses different authentication mechanisms depending on transport and
context:

- **Public repositories** (e.g. `github.com/HashLoad/horse`): no setup needed.
- **Private SSH repositories** (`git@gitlab.mydomain.com:foo/bar.git`): bossy invokes
  the system `git` binary. Configure `ssh-agent`, `~/.ssh/config`, and your
  SSH key the way you would for any other git workflow. No `bossy auth`
  command is required for SSH.
- **Private HTTPS repositories**: store basic-auth credentials per host:
  ```sh
  bossy auth set gitlab.mydomain.com              # interactive
  bossy auth set gitlab.mydomain.com -u user -p pat
  bossy auth list
  bossy auth rm gitlab.mydomain.com
  ```
- **CI/CD environments**: bossy auto-detects GitLab CI via `GITLAB_CI`,
  `CI_SERVER_HOST`, and `CI_JOB_TOKEN`. No setup needed in the runner.
  See [`docs/ci.md`](docs/ci.md).

For non-GitLab CI runners or one-off overrides:
```sh
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=https-token:gitlab-ci-token:$CI_JOB_TOKEN
# or, force SSH for a host:
export BOSSY_AUTH_GITLAB_MYDOMAIN_COM=ssh
```

To pin a default protocol per host:
```sh
bossy config git protocol gitlab.mydomain.com ssh
bossy config git protocol github.com https
```

### > Dependency URL forms

In `bossy.json` and on the `bossy install` command line, you can use any of:

- Bare name → defaults to `github.com/hashload/<name>` (e.g. `horse`).
- `host/owner/repo`
- `git@host:owner/repo[.git]` — pins the dep to SSH.
- `https://host/owner/repo[.git]` — pins the dep to HTTPS.

### > Version

Show the Bossy CLI version:

```shell
bossy version
bossy v
bossy -v
bossy --version
```

> Aliases: v

## Global Flags

### > Global (-g)

Use global environment for installation. Packages installed globally are available system-wide:

```sh
bossy install -g <dependency>
bossy --global install <dependency>
```

### > Debug (-d)

Enable debug mode to see detailed output:

```sh
bossy install --debug
bossy -d install
```

### > Help (-h)

Show help for any command:

```sh
bossy --help
bossy <command> --help
```

## Configuration

### > Cache

Manage the Bossy cache. Remove all cached modules to free up disk space:

```sh
bossy config cache rm
```

> Aliases: purge, clean

### > Delphi Version

You can configure which Delphi version Bossy should use for compilation. This is useful when you have multiple Delphi versions installed.

#### List available versions

Lists all detected Delphi installations (32-bit and 64-bit) with their indexes.

```sh
bossy config delphi list
```

#### Select a version

Selects a specific Delphi version to use globally. You can use the index from the list command, the version number, or the version with architecture.

```sh
bossy config delphi use <index>
# or
bossy config delphi use <version>
# or
bossy config delphi use <version>-<arch>
```

Example:
```sh
bossy config delphi use 0
bossy config delphi use 37.0
bossy config delphi use 37.0-Win64
```

### > Shallow Clone

You can enable shallow cloning to significantly speed up dependency downloads. Shallow clones only fetch the latest commit of each branch without the full git history, reducing download size dramatically (e.g., from 127 MB to <1 MB for large repositories).

All branch tips are still fetched, so branch-pinned dependencies keep resolving
correctly. If a dependency is pinned to a commit SHA that predates the shallow
cut-off, bossy deepens the clone automatically on first use.

```sh
# Enable shallow clone (faster, recommended for CI/CD)
bossy config git shallow true

# Disable shallow clone (full history)
bossy config git shallow false
```

**Note:** Shallow clone is disabled by default to maintain compatibility. When enabled, you won't have access to the full git history of dependencies.

You can also temporarily enable shallow clone using an environment variable:

```sh
# Windows
set BOSS_GIT_SHALLOW=1
bossy install

# Linux/macOS
BOSS_GIT_SHALLOW=1 bossy install
```

### > SSH Behaviour

SSH dependencies are cloned and listed with the system `git` binary, so
`~/.ssh/config` aliases, custom keys and non-standard ports all work as they
do on the command line. Two details are worth knowing:

- **`GIT_SSH_COMMAND` defaults to `ssh -o BatchMode=yes`** for bossy's own git
  invocations when the variable is not already set. A passphrase-protected key
  with no `ssh-agent` loaded therefore fails immediately instead of prompting.
  That is deliberate: an invisible prompt hangs a CI job forever. Load an agent
  if you need the key unlocked interactively.
- **The environment variable outranks git's `core.sshCommand`.** If you
  configured a custom ssh wrapper (custom key, proxy command) in
  `~/.gitconfig` rather than in the environment, bossy will not pick it up and
  will use plain `ssh -o BatchMode=yes` instead. Export `GIT_SSH_COMMAND` with
  your wrapper to keep it in effect:

  ```sh
  export GIT_SSH_COMMAND='ssh -i ~/.ssh/id_work -o BatchMode=yes'
  ```

### > Host Aliases

Define short aliases for git hosts so dependencies can be referenced as
`<alias>:<path>` on the install command line. Useful when a long internal
hostname clutters `bossy install` invocations.

```sh
# Set an alias
bossy config alias mygit gitlab.mydomain.com

# List configured aliases
bossy config alias --list

# Remove an alias
bossy config alias --unset mygit
```

With the alias `mygit → gitlab.mydomain.com` configured, the following are equivalent:

```sh
bossy install mygit:devops/some-lib
bossy install gitlab.mydomain.com/devops/some-lib
```

The expansion respects the host's configured protocol
(`bossy config git protocol <host> ssh|https`):

- If the host is pinned to `ssh`, `mygit:devops/some-lib` expands to
  `git@gitlab.mydomain.com:devops/some-lib`.
- Otherwise it expands to `gitlab.mydomain.com/devops/some-lib` (cloned over HTTPS).

Aliases are a CLI convenience only — they are never written into `bossy.json`.
Only the canonical `git@host:path` or `host/path` form is stored, so the
manifest stays portable across machines that may not share the same alias
config. The names `git`, `http`, `https`, and `ssh` are reserved.

### > Project Toolchain

You can also specify the required compiler version and platform in your project's `bossy.json` file. This ensures that everyone working on the project uses the correct toolchain.

Add a `toolchain` section to your `bossy.json`:

```json
{
  "name": "my-project",
  "version": "1.0.0",
  "toolchain": {
    "compiler": "37.0",
    "platform": "Win64"
  }
}
```

Supported fields in `toolchain`:
- `compiler`: The compiler version (e.g., "37.0").
- `platform`: The target platform ("Win32" or "Win64").
- `path`: Explicit path to the compiler (optional).
- `strict`: If true, fails if the exact version is not found (optional).

## Samples

```sh
bossy install horse
bossy install horse@1.0.0
bossy install -g delphi-docker
bossy install -g boss-ide
```

The version separator is `@`. A `name:version` argument is read as
`<alias>:<path>` (see [Host Aliases](#-host-aliases)); when the prefix is not a
configured alias the argument is discarded, and the run reports
`📄 No dependencies to install` and exits 0.

## Using [semantic versioning](https://semver.org/) to specify update types your package can accept

You can specify which update types your package can accept from dependencies in your package's bossy.json file.

For example, to specify acceptable version ranges up to 1.0.4, use the following syntax:

- Patch releases: 1.0 or 1.0.x or ~1.0.4
- Minor releases: 1 or 1.x or ^1.0.4
- Major releases: \* or x

### Pinning to a branch

A version string that is not a semver constraint is matched against the
repository's refs by exact name, and that ref list contains branches as well as
tags. A branch name therefore works as a version:

```json
{
  "dependencies": {
    "github.com/HashLoad/horse": "dev"
  }
}
```

The failed semver parse is reported as a warning even though the install
succeeds. See [`docs/workflows.md`](docs/workflows.md) for the full workflow,
including why a branch pin still gives reproducible builds.

### Pinning to a specific commit

In addition to tag-based constraints, a dependency version can be a raw git
commit SHA (7-40 hex characters). The dependency is then checked out at exactly
that commit:

```json
{
  "dependencies": {
    "git@gitlab.example.com:foo/bar": "a1b2c3d",
    "github.com/HashLoad/horse": "3f9e2c1a4b5d6e7f8091a2b3c4d5e6f70a1b2c3d"
  }
}
```

After every install, the resolved commit SHA is recorded in `bossy-lock.json`
under a `commit` field per dependency. On subsequent runs:

- `bossy install` checks the dependency out at the SHA from the lock, in
  detached HEAD, and skips the pull. Builds stay reproducible even if upstream
  tags move or branches advance.
- `bossy update` ignores the lock, re-resolves each dependency's version
  constraint against the remote, and rewrites the lock with the new SHAs.

Because `install` replays the lock, `bossy install <dep>@<version>` on a
dependency that is already locked rewrites the `bossy.json` entry without
touching the lock or the checkout. Use `bossy update <dep>[@<version>]` to move
a pin.

Commit `bossy-lock.json` to source control alongside `bossy.json` if you want
reproducible CI builds. See [`docs/ci.md`](docs/ci.md) and
[`docs/workflows.md`](docs/workflows.md).

## bossy.json File Format

The `bossy.json` file is the manifest for your Delphi/Lazarus project. It contains metadata, dependencies, build configuration, and custom scripts.

### Complete Structure

Here's a comprehensive example showing all available fields:

```json
{
  "name": "my-project",
  "description": "A sample Delphi project using Bossy",
  "version": "1.0.0",
  "homepage": "https://github.com/myuser/my-project",
  "mainsrc": "src/",
  "browsingpath": "src/;libs/",
  "projects": [
    "MyProject.dproj",
    "MyPackage.dproj"
  ],
  "searchpaths": {
    "github.com/danieleteti/delphimvcframework": ["sources", "lib/loggerpro"]
  },
  "dependencies": {
    "github.com/HashLoad/horse": "^3.0.0",
    "github.com/HashLoad/jhonson": "~2.1.0",
    "dataset-serialize": "*"
  },
  "scripts": {
    "build": "msbuild MyProject.dproj /p:Config=Release",
    "test": "MyProject.exe --test",
    "clean": "del /s *.dcu"
  },
  "engines": {
    "compiler": ">=35.0",
    "platforms": ["Win32", "Win64"]
  },
  "toolchain": {
    "compiler": "37.0",
    "platform": "Win64",
    "path": "C:\\Program Files\\Embarcadero\\Studio\\37.0",
    "strict": false
  }
}
```

### Field Descriptions

#### Core Fields

- **`name`** (required): Package name. Must be unique if publishing.
  ```json
  "name": "my-awesome-library"
  ```

- **`description`** (optional): A brief description of your project.
  ```json
  "description": "REST API framework for Delphi"
  ```

- **`version`** (required): Package version following [semantic versioning](https://semver.org/).
  ```json
  "version": "1.2.3"
  ```

- **`homepage`** (optional): Project website or repository URL.
  ```json
  "homepage": "https://github.com/myuser/my-project"
  ```

#### Source Configuration

- **`mainsrc`** (optional): Main source directory path.
  ```json
  "mainsrc": "src/"
  ```

- **`browsingpath`** (optional): Additional paths for IDE browsing (semicolon-separated).
  ```json
  "browsingpath": "src/;src/controllers/;src/models/"
  ```

- **`searchpaths`** (optional): Restricts which directories of a dependency
  reach the compiler search path.

  By default bossy walks a dependency's whole tree and adds every directory
  that holds source. That is right for a flat library, and wrong for a
  repository that ships samples and unit tests next to its library —
  delphimvcframework contributes around 190 directories that way, most of
  them demo projects. Naming the directories that hold the library fixes it:

  ```json
  "searchpaths": {
    "github.com/danieleteti/delphimvcframework": ["sources", "lib/loggerpro"]
  }
  ```

  Paths are relative to the module directory and are still walked
  recursively, so a nested layout needs only its root listed. Keys may be
  the full dependency reference as written in `dependencies`, or the bare
  module name (`delphimvcframework`). A path that does not exist is skipped
  with a warning.

  Beyond tidiness this can decide whether a project builds at all: MSBuild
  passes the search path to `dcc` on the command line, and Windows caps that
  at 32767 characters. Overrun it and the build fails with `MSB6003 ... The
  filename or extension is too long`, which names nothing useful.

#### Build Configuration

- **`projects`** (optional): List of Delphi project files (`.dproj`) to compile.
  ```json
  "projects": [
    "MyProject.dproj",
    "MyLibrary.dproj"
  ]
  ```

  **Note:** If not specified, Bossy won't compile the package but will still manage dependencies.

#### Dependencies

- **`dependencies`** (optional): Map of package dependencies with version constraints.
  ```json
  "dependencies": {
    "github.com/HashLoad/horse": "^3.0.0",
    "dataset-serialize": "~2.1.0",
    "jhonson": "*"
  }
  ```

  Supported version formats:
  - Exact version: `"1.0.0"`
  - Caret (minor updates): `"^1.0.0"` (allows 1.x.x, but not 2.x.x)
  - Tilde (patch updates): `"~1.0.0"` (allows 1.0.x, but not 1.1.x)
  - Wildcard (any): `"*"` or `"x"`
  - Range: `">=1.0.0 <2.0.0"`
  - Branch name: `"dev"` (see [Pinning to a branch](#pinning-to-a-branch))
  - Commit SHA: `"4e99beb"` (see [Pinning to a specific commit](#pinning-to-a-specific-commit))

#### Custom Scripts

- **`scripts`** (optional): Custom commands you can run with `bossy run <script-name>`.
  ```json
  "scripts": {
    "build": "msbuild MyProject.dproj /p:Config=Release",
    "test": "dunitx-console.exe MyProject.exe",
    "clean": "del /s *.dcu *.exe",
    "deploy": "xcopy /s /y bin\\*.exe deploy\\"
  }
  ```

  Execute with:
  ```sh
  bossy run build
  bossy run test
  ```

#### Engine Requirements

- **`engines`** (optional): Specify minimum compiler/platform requirements.
  ```json
  "engines": {
    "compiler": ">=35.0",
    "platforms": ["Win32", "Win64", "Linux64"]
  }
  ```

  - `compiler`: Minimum compiler version
  - `platforms`: Supported target platforms

#### Toolchain Configuration

- **`toolchain`** (optional): Specify the exact toolchain to use for this project.
  ```json
  "toolchain": {
    "compiler": "37.0",
    "platform": "Win64",
    "path": "C:\\Program Files\\Embarcadero\\Studio\\37.0",
    "strict": true
  }
  ```

  - `compiler`: Required compiler version
  - `platform`: Target platform ("Win32", "Win64", "Linux64", etc.)
  - `path`: Explicit path to the compiler (optional)
  - `strict`: If `true`, fails if the exact version is not found (default: `false`)

### Minimal bossy.json

The minimal valid `bossy.json` file:

```json
{
  "name": "my-project",
  "version": "1.0.0"
}
```

### Creating a new bossy.json

Use `bossy init` to create a new `bossy.json` interactively:

```sh
bossy init
```

Or use quiet mode for defaults:

```sh
bossy init -q
```

### Example: Library Package

```json
{
  "name": "my-delphi-library",
  "description": "Utilities for Delphi applications",
  "version": "2.1.0",
  "homepage": "https://github.com/myuser/my-library",
  "mainsrc": "src/",
  "projects": [
    "MyLibrary.dproj"
  ],
  "dependencies": {
    "github.com/HashLoad/horse": "^3.0.0"
  }
}
```

### Example: Application Package

```json
{
  "name": "my-app",
  "description": "My awesome Delphi application",
  "version": "1.0.0",
  "projects": [
    "MyApp.dproj"
  ],
  "dependencies": {
    "github.com/HashLoad/horse": "^3.0.0"
  },
  "scripts": {
    "build": "msbuild MyApp.dproj /p:Config=Release",
    "run": "bin\\MyApp.exe",
    "test": "dunitx-console.exe bin\\MyAppTests.exe"
  },
  "toolchain": {
    "compiler": "37.0",
    "platform": "Win32"
  }
}
```


## 💻 Code Contributors

![GitHub Contributors Image](https://contrib.rocks/image?repo=Basti-Fantasti/bossy)

[githubContributorsBadge]: https://img.shields.io/github/contributors/Basti-Fantasti/bossy
[goReportBadge]: https://goreportcard.com/badge/github.com/basti-fantasti/bossy
[goReportLink]: https://goreportcard.com/report/github.com/basti-fantasti/bossy
[bossLogo]: ./assets/png/sized/boss-logo-128px.png
[latestReleaseBadge]: https://img.shields.io/github/v/release/Basti-Fantasti/bossy
[releaseDateBadge]: https://img.shields.io/github/release-date/Basti-Fantasti/bossy
[repoSizeBadge]: https://img.shields.io/github/repo-size/Basti-Fantasti/bossy
[totalDownloadsBadge]: https://img.shields.io/github/downloads/Basti-Fantasti/bossy/total
[githubLicenseBadge]: https://img.shields.io/github/license/Basti-Fantasti/bossy
[githubIssuesBadge]: https://img.shields.io/github/issues/Basti-Fantasti/bossy
[githubPullRequestsBadge]: https://img.shields.io/github/issues-pr/Basti-Fantasti/bossy
[repoStarsBadge]: https://img.shields.io/github/stars/Basti-Fantasti/bossy?style=social
