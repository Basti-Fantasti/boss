//nolint:testpackage // Testing internal functions
package librarypath

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
)

// The Delphi IDE writes .dproj files with CRLF. etree normalises CRLF to LF
// while parsing, because the XML spec requires it, so writing the document
// back rewrites every line of the file unless the original ending is restored.
//
// On a build server that matters beyond noise: with core.autocrlf=true the
// runner checks the project file out as CRLF, bossy writes it back as LF, and
// delphi-build-worker's verify_dproj — which hashes the raw bytes — reports the
// committed project file as out of date and fails the build before compiling.
// `git diff` cannot see it, because git normalises line endings before
// comparing.

const dprojWithLibraryPath = `<Project xmlns="http://schemas.microsoft.com/developer/msbuild/2003">
    <PropertyGroup Condition="'$(Base)'!=''">
        <DCC_UnitSearchPath>src;$(DCC_UnitSearchPath)</DCC_UnitSearchPath>
    </PropertyGroup>
</Project>
`

// writeProject drops a .dproj with the given line ending into a temp project
// directory and chdirs into it, because the path helpers resolve against the
// working directory.
func writeProject(t *testing.T, lineEnding string) string {
	t.Helper()
	project := t.TempDir()
	t.Chdir(project)

	content := dprojWithLibraryPath
	if lineEnding == "\r\n" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}

	name := filepath.Join(project, "Sample.dproj")
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("write dproj: %v", err)
	}
	return name
}

func readProject(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read dproj: %v", err)
	}
	return content
}

func TestUpdateLibraryPathProject_KeepsCRLF(t *testing.T) {
	name := writeProject(t, "\r\n")

	updateLibraryPathProject(domain.NewPackage(), name)

	content := readProject(t, name)
	if !bytes.Contains(content, []byte("\r\n")) {
		t.Errorf("CRLF project file was rewritten with LF endings:\n%q", content)
	}
	if bytes.Contains(bytes.ReplaceAll(content, []byte("\r\n"), nil), []byte("\n")) {
		t.Errorf("CRLF project file came back with mixed endings:\n%q", content)
	}
}

func TestUpdateLibraryPathProject_KeepsLF(t *testing.T) {
	name := writeProject(t, "\n")

	updateLibraryPathProject(domain.NewPackage(), name)

	content := readProject(t, name)
	if bytes.Contains(content, []byte("\r")) {
		t.Errorf("LF project file gained CR characters:\n%q", content)
	}
}

// Nothing in the file needs changing here — the dependency list is empty — so
// a second run must leave the bytes alone. That is the property the build
// server checks, and it is stronger than either test above on its own.
func TestUpdateLibraryPathProject_IsByteStableOnRerun(t *testing.T) {
	for _, ending := range []struct {
		name  string
		value string
	}{
		{"CRLF", "\r\n"},
		{"LF", "\n"},
	} {
		t.Run(ending.name, func(t *testing.T) {
			name := writeProject(t, ending.value)

			updateLibraryPathProject(domain.NewPackage(), name)
			first := readProject(t, name)

			updateLibraryPathProject(domain.NewPackage(), name)
			second := readProject(t, name)

			if !bytes.Equal(first, second) {
				t.Errorf("second run changed the file:\nfirst:  %q\nsecond: %q", first, second)
			}
		})
	}
}

func TestDetectLineEnding(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"crlf", "<a>\r\n</a>\r\n", "\r\n"},
		{"lf", "<a>\n</a>\n", "\n"},
		{"no line break at all", "<a/>", "\n"},
		{"mixed counts as crlf", "<a>\n</a>\r\n", "\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectLineEnding([]byte(tc.content)); got != tc.want {
				t.Errorf("detectLineEnding(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}
