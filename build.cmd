@echo off
REM Build the bossy CLI binary for Windows.
REM Mirrors the ldflags in the Makefile so the binary reports the same
REM version metadata as a `make build`-produced binary on Unix.
REM
REM Usage:
REM   build.cmd            (release-stamped if a git tag points at HEAD, else "unreleased")
REM   build.cmd <version>  (stamp a custom version string)
REM
REM Output: bin\bossy.exe

setlocal enableextensions

cd /d "%~dp0"

if not exist bin mkdir bin

for /f %%i in ('git rev-parse HEAD') do set GIT_COMMIT=%%i
if errorlevel 1 (
    echo failed to read git commit
    exit /b 1
)

set VERSION_METADATA=unreleased
if not "%~1"=="" set VERSION_METADATA=%~1

set LDFLAGS=-w -s
set LDFLAGS=%LDFLAGS% -X github.com/basti-fantasti/bossy/internal/version.metadata=%VERSION_METADATA%
set LDFLAGS=%LDFLAGS% -X github.com/basti-fantasti/bossy/internal/version.gitCommit=%GIT_COMMIT%

set GO111MODULE=on
go build -ldflags "%LDFLAGS%" -o bin\bossy.exe .
if errorlevel 1 (
    echo go build failed
    exit /b 1
)

echo built bin\bossy.exe (commit %GIT_COMMIT%, version metadata %VERSION_METADATA%)

endlocal
