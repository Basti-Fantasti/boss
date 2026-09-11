# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Boss is a Go-based dependency manager CLI for Delphi/Lazarus projects (npm-inspired). It resolves packages from Git repos (GitHub, GitLab, arbitrary URLs), manages a per-user cache, wires sources into Delphi's library/browsing paths, optionally compiles `.dproj` files, and self-updates via GitHub releases.

## Common commands

Toolchain is pinned via `mise.toml` (Go 1.24, pre-commit 4.0.1). `golangci-lint` is a `go tool` dependency — invoke it via `go tool golangci-lint`, not a system install.

- Build: `make build` → `./bin/bossy`. Cross-compile via `make build-cross` (uses `gox`).
- Run from source: `make run <args>` (wraps `go run`).
- Tests: `make test` (style + unit, with `-race -v`). Single test: `go test ./internal/core/domain -run TestPackage_Foo -v`. Single package: `go test ./utils/parser`.
- Coverage: `make test-coverage` (script in `scripts/coverage.sh`).
- Lint: `make test-style` or `mise run golint` (both call `golangci-lint run`). Full pre-commit: `mise run lint`.
- Format: `make format` (goimports with `-local .`).
- Version metadata is injected via `-ldflags` into `internal/version` (`version`, `metadata`, `gitCommit`). Local builds without a tag are stamped `unreleased`.

## Architecture

The codebase follows hexagonal / ports-and-adapters layout. Treat the boundary strictly — domain and services must not import adapters.

```
app.go                                 thin main → cmd.Execute
cmd/cmd.go                             delegates to primary CLI adapter
internal/
  core/
    domain/        pure types: Package (bossy.json), Dependency, Lock (bossy-lock.json),
                   Constraint (semver), Graph (resolution tree), CacheInfo
    ports/         interfaces the core depends on: Git, Registry, Compiler,
                   Installer, Repositories (lock + package persistence)
    services/      use cases composed against ports:
                   installer, lock, packages, cache, gc, compiler,
                   compilerselector, paths (Delphi library/browsing path),
                   scripts, tracker (per-dep progress UI)
  adapters/
    primary/cli/   cobra commands (root, install, uninstall, update, upgrade,
                   init, login, run, dependencies, version, config/*).
                   This is where DI happens — wire services to secondary adapters.
    secondary/
      git/         go-git ("embedded") and shell git ("native") implementations
                   selectable via `boss config git mode`; plus shallow-clone
                   handling and a custom storage layer
      registry/    Windows registry + unix stub for Delphi install discovery
      repository/  on-disk lock + package (bossy.json / bossy-lock.json) persistence
      filesystem/  fs abstraction over real disk
  infra/           cross-cutting filesystem helpers / typed errors
  upgrade/         self-update flow (uses minio/selfupdate against GitHub releases)
  version/         build-time stamped version info
pkg/
  consts/          well-known filenames, env vars, paths
  env/             global runtime config (debug, global flag, paths, git mode)
  msg/             user-facing logging (pterm) + Die helper used by main
  pkgmanager/      higher-level facade some callers use instead of services directly
utils/
  dcc32/           Delphi command-line compiler invocation
  dcp/             .dcp/.bpl artifact handling
  librarypath/     parsing and editing Delphi IDE library path entries
  parser/          bossy.json parsing helpers
  crypto/, hash.go, arrays.go: small shared helpers
setup/             first-run / migration logic for the user's boss home dir
```

Key invariants:

- Two manifest formats live in `internal/core/domain`: **`bossy.json`** (`package.go`, user-authored) and **`bossy-lock.json`** (`lock.go`, generated). Treat the lock as derivable; never hand-edit-style logic should leak into domain types.
- Git access always goes through `core/ports.Git`. The runtime implementation is chosen at startup based on `boss config git mode` (embedded = go-git, native = `git` binary). When adding git operations, extend the port and implement in **both** `git_embedded.go` and `git_native.go`.
- Shallow clone is opt-in (`bossy config git shallow true` or `BOSSY_GIT_SHALLOW=1`). Code that walks history must tolerate a shallow clone or explicitly request a deep one.
- Delphi compiler discovery lives in `adapters/secondary/registry` (Windows-only via `registry_win.go`; `registry_unix.go` is a stub). `services/compilerselector` chooses the active toolchain honoring `bossy.json`'s `toolchain` block.
- The `paths` service is what mutates Delphi IDE library/browsing paths — be careful, it edits user IDE state. `librarypath` is its parsing primitive.
- CLI commands should be thin: parse flags, build the service, call it, render via `pkg/msg`. Business logic belongs in `core/services`.

## Conventions

- `pkg/msg.Die` is the canonical fatal exit; `main` already wraps `cmd.Execute` with it. Don't call `os.Exit` directly from services.
- Global runtime state (debug flag, `-g` global mode, resolved paths) lives in `pkg/env`. Read it; don't replicate it.
- Tests sit next to code (`*_test.go`). The `cmd/cmd_test.go` of the CLI adapter is the closest thing to an end-to-end harness.
- Lint config is strict (`.golangci.yml`, based on maratori's golden config, cyclop max 30). Run `make test-style` before pushing.
