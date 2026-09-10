//nolint:testpackage // Testing internal functions
package librarypath

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/consts"
)

// writeUnit creates dir under root and drops a .pas file in it, which is what
// makes the directory visible to the search-path walk.
func writeUnit(t *testing.T, root, dir string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(dir))
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(filepath.Join(full, "Unit1.pas"), []byte("unit Unit1;"), 0o600); err != nil {
		t.Fatalf("write unit in %s: %v", full, err)
	}
}

// newModulesDir lays out a project directory holding one dependency shaped
// like delphimvcframework: a real source folder next to sample and test trees.
// It chdirs into the project, because env.GetModulesDir resolves against the
// working directory.
func newModulesDir(t *testing.T) (string, string) {
	t.Helper()
	project := t.TempDir()
	t.Chdir(project)
	modules := filepath.Join(project, consts.FolderDependencies)
	moduleDir := filepath.Join(modules, "dmvc")
	writeUnit(t, moduleDir, "sources")
	writeUnit(t, moduleDir, "samples/basicdemo")
	writeUnit(t, moduleDir, "samples/jsonrpc")
	writeUnit(t, moduleDir, "unittests/general")
	return modules, moduleDir
}

func TestModuleScanRoots_NoRestrictionWalksWholeModule(t *testing.T) {
	modules, moduleDir := newModulesDir(t)

	roots := moduleScanRoots(domain.NewPackage(), modules, "dmvc")

	want := []string{moduleDir}
	if !slices.Equal(roots, want) {
		t.Errorf("moduleScanRoots() = %v, want %v", roots, want)
	}
}

func TestModuleScanRoots_RestrictionNarrowsToDeclaredPaths(t *testing.T) {
	modules, moduleDir := newModulesDir(t)

	pkg := domain.NewPackage()
	pkg.SearchPaths = map[string][]string{"dmvc": {"sources"}}

	roots := moduleScanRoots(pkg, modules, "dmvc")

	want := []string{filepath.Join(moduleDir, "sources")}
	if !slices.Equal(roots, want) {
		t.Errorf("moduleScanRoots() = %v, want %v", roots, want)
	}
}

// A restriction is written against the dependency as declared in
// "dependencies", which is a full repository reference, not the module
// directory name the walk iterates over.
func TestModuleScanRoots_RestrictionMatchesFullDependencyKey(t *testing.T) {
	modules, moduleDir := newModulesDir(t)

	pkg := domain.NewPackage()
	pkg.SearchPaths = map[string][]string{"github.com/danieleteti/DMVC": {"sources"}}

	roots := moduleScanRoots(pkg, modules, "dmvc")

	want := []string{filepath.Join(moduleDir, "sources")}
	if !slices.Equal(roots, want) {
		t.Errorf("moduleScanRoots() = %v, want %v", roots, want)
	}
}

func TestModuleScanRoots_DropsMissingAndEscapingPaths(t *testing.T) {
	modules, moduleDir := newModulesDir(t)

	pkg := domain.NewPackage()
	pkg.SearchPaths = map[string][]string{
		"dmvc": {"sources", "does-not-exist", "../../elsewhere"},
	}

	roots := moduleScanRoots(pkg, modules, "dmvc")

	want := []string{filepath.Join(moduleDir, "sources")}
	if !slices.Equal(roots, want) {
		t.Errorf("moduleScanRoots() = %v, want %v", roots, want)
	}
}

// The restriction has to survive the walk, not just moduleScanRoots: this is
// the behaviour that keeps dcc's command line inside the Windows length limit.
func TestGetNewPaths_RestrictionKeepsSamplesOffTheSearchPath(t *testing.T) {
	modules, _ := newModulesDir(t)

	pkg := domain.NewPackage()
	pkg.SearchPaths = map[string][]string{"dmvc": {"sources"}}

	got := GetNewPaths(pkg, nil, true, modules)

	for _, p := range got {
		if filepath.Base(filepath.Dir(filepath.Clean(p))) == "samples" {
			t.Errorf("GetNewPaths() kept a samples directory: %s", p)
		}
	}
	if !slices.ContainsFunc(got, func(p string) bool {
		return filepath.Base(filepath.Clean(p)) == "sources"
	}) {
		t.Errorf("GetNewPaths() dropped the declared sources directory, got %v", got)
	}
}

// Without a restriction the same layout contributes every sample directory,
// which is the behaviour the restriction exists to escape. Asserting it here
// keeps the test above honest: it proves the samples were reachable.
func TestGetNewPaths_NoRestrictionKeepsSamplesOnTheSearchPath(t *testing.T) {
	modules, _ := newModulesDir(t)

	got := GetNewPaths(domain.NewPackage(), nil, true, modules)

	found := false
	for _, p := range got {
		if filepath.Base(filepath.Dir(filepath.Clean(p))) == "samples" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GetNewPaths() found no samples directory to begin with, got %v", got)
	}
}
