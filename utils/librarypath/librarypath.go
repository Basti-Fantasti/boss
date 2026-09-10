// Package librarypath provides utilities for managing Delphi library paths.
// It updates .dproj files with dependency paths and manages global browsing paths.
package librarypath

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basti-fantasti/bossy/pkg/pkgmanager"

	"slices"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/utils"
)

// UpdateLibraryPath updates the library path for the project or globally.
func UpdateLibraryPath(pkg *domain.Package) {
	msg.Info("♻️ Updating library path...")
	if env.GetGlobal() {
		updateGlobalLibraryPath(pkg)
	} else {
		updateDprojLibraryPath(pkg)
		updateGlobalBrowsingPath(pkg)
	}
}

// cleanPath removes duplicate paths and paths that are already in the modules directory.
func cleanPath(paths []string, fullPath bool) []string {
	prefix := env.GetModulesDir()
	var processedPaths []string
	if !fullPath {
		prefix, _ = filepath.Rel(env.GetCurrentDir(), prefix)
	}

	for key := range paths {
		if strings.HasPrefix(paths[key], prefix) {
			continue
		}
		if !utils.Contains(processedPaths, paths[key]) {
			processedPaths = append(processedPaths, paths[key])
		}
	}
	return processedPaths
}

// GetNewBrowsingPaths returns a list of new browsing paths.
func GetNewBrowsingPaths(paths []string, fullPath bool, rootPath string, setReadOnly bool) []string {
	paths = cleanPath(paths, fullPath)
	var path = env.GetModulesDir()

	matches, _ := os.ReadDir(path)

	for _, value := range matches {
		paths = processBrowsingPath(value, paths, path, fullPath, rootPath, setReadOnly)
	}
	return paths
}

// processBrowsingPath processes a browsing path for a package.
func processBrowsingPath(
	value os.DirEntry,
	paths []string,
	basePath string,
	fullPath bool,
	rootPath string,
	setReadOnly bool,
) []string {
	var packagePath = filepath.Join(basePath, value.Name(), consts.FilePackage)
	if _, err := os.Stat(packagePath); !os.IsNotExist(err) {
		other, _ := pkgmanager.LoadPackageOther(packagePath)
		if other.BrowsingPath != "" {
			dir := filepath.Join(basePath, value.Name(), other.BrowsingPath)
			paths = getNewBrowsingPathsFromDir(dir, paths, fullPath, rootPath)
			if setReadOnly {
				setReadOnlyProperty(dir)
			}
		}
	}
	return paths
}

// setReadOnlyProperty sets the read-only property for a directory.
func setReadOnlyProperty(dir string) {
	readonlybat := filepath.Join(dir, "readonly.bat")
	readFileStr := fmt.Sprintf(`attrib +r "%s" /s /d`, filepath.Join(dir, "*"))
	err := os.WriteFile(readonlybat, []byte(readFileStr), 0600)
	if err != nil {
		msg.Warn("  ⚠️ Error on create build file")
	}

	cmd := exec.Command(readonlybat) // #nosec G204 -- Executing controlled batch file with readonly attributes

	_, err = cmd.Output()
	if err != nil {
		msg.Err("  ❌ Failed to set readonly property to folder", dir, " - ", err)
	} else {
		os.Remove(readonlybat) // #nosec G104 -- Ignoring error on removing temporary file
	}
}

// GetNewPaths returns a list of new paths.
func GetNewPaths(pkg *domain.Package, paths []string, fullPath bool, rootPath string) []string {
	paths = cleanPath(paths, fullPath)
	var path = env.GetModulesDir()

	matches, _ := os.ReadDir(path)

	for _, value := range matches {
		for _, root := range moduleScanRoots(pkg, path, value.Name()) {
			paths = getNewPathsFromDir(root, paths, fullPath, rootPath)
		}
	}
	return paths
}

// moduleScanRoots returns the directories inside one installed module that are
// walked for compilable source, most specific source of truth first: a
// searchpaths restriction in the consuming bossy.json, then the dependency's
// own mainsrc, then the whole module.
//
// The last of those is the historical behaviour and stays the default, because
// most Delphi libraries are a flat folder of units. It only misbehaves for a
// repository that ships samples and tests alongside the library, where it can
// contribute hundreds of directories.
func moduleScanRoots(pkg *domain.Package, basePath, moduleName string) []string {
	moduleDir := filepath.Join(basePath, moduleName)

	if declared, ok := pkg.ModuleSearchPaths(moduleName); ok {
		return resolveDeclaredRoots(moduleDir, moduleName, declared)
	}

	packagePath := filepath.Join(moduleDir, consts.FilePackage)
	if _, err := os.Stat(packagePath); !os.IsNotExist(err) {
		other, _ := pkgmanager.LoadPackageOther(packagePath)
		return []string{filepath.Join(moduleDir, other.MainSrc)}
	}
	return []string{moduleDir}
}

// resolveDeclaredRoots turns declared relative paths into directories under the
// module, dropping the ones that do not resolve. Both rejections are warned
// about rather than ignored: a restriction that silently matches nothing
// removes the library from the search path, and that surfaces much later as a
// missing-unit error pointing nowhere near the manifest.
func resolveDeclaredRoots(moduleDir, moduleName string, declared []string) []string {
	roots := make([]string, 0, len(declared))
	for _, rel := range declared {
		root := filepath.Join(moduleDir, filepath.FromSlash(rel))
		if !isInside(moduleDir, root) {
			msg.Warn("⚠️ searchpaths: %q escapes module %s and was ignored", rel, moduleName)
			continue
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			msg.Warn("⚠️ searchpaths: %q does not exist in module %s", rel, moduleName)
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

// isInside reports whether child is base itself or sits below it.
func isInside(base, child string) bool {
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// getDefaultPath returns the default library paths.
func getDefaultPath(fullPath bool, rootPath string) []string {
	var paths []string

	if !fullPath {
		fullPath := filepath.Join(env.GetCurrentDir(), consts.FolderDependencies, consts.DcpFolder)

		dir, err := filepath.Rel(rootPath, fullPath)
		if err == nil {
			paths = append(paths, dir)
		}

		fullPath = filepath.Join(env.GetCurrentDir(), consts.FolderDependencies, consts.DcuFolder)
		dir, err = filepath.Rel(rootPath, fullPath)
		if err == nil {
			paths = append(paths, dir)
		}
	} else {
		paths = append(paths, filepath.Join(env.GetCurrentDir(), consts.FolderDependencies, consts.DcpFolder))
		paths = append(paths, filepath.Join(env.GetCurrentDir(), consts.FolderDependencies, consts.DcuFolder))
	}

	if isLazarus() {
		return paths
	}

	return append(paths, "$(DCC_UnitSearchPath)")
}

// cleanEmpty removes empty strings from a slice.
func cleanEmpty(paths []string) []string {
	for index, value := range paths {
		if value == "" {
			paths = slices.Delete(paths, index, index+1)
		}
	}
	return paths
}

// getNewBrowsingPathsFromDir returns a list of new browsing paths from a directory.
func getNewBrowsingPathsFromDir(path string, paths []string, fullPath bool, rootPath string) []string {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return paths
	}

	_ = filepath.Walk(path, func(path string, info os.FileInfo, _ error) error {
		matched, _ := regexp.MatchString(consts.RegexArtifacts, info.Name())
		if matched {
			dir, _ := filepath.Split(path)

			if !fullPath {
				dir, _ = filepath.Rel(rootPath, dir)
			}
			if !utils.Contains(paths, dir) {
				paths = append(paths, dir)
			}
		}
		return nil
	})
	return cleanEmpty(paths)
}

// getNewPathsFromDir returns a list of new paths from a directory.
func getNewPathsFromDir(path string, paths []string, fullPath bool, rootPath string) []string {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return paths
	}

	_ = filepath.Walk(path, func(path string, info os.FileInfo, _ error) error {
		matched, _ := regexp.MatchString(consts.RegexArtifacts, info.Name())
		if matched {
			dir, _ := filepath.Split(path)
			if !fullPath {
				dir, _ = filepath.Rel(rootPath, dir)
			}
			if !utils.Contains(paths, dir) {
				paths = append(paths, dir)
			}
		}
		return nil
	})

	for _, path := range getDefaultPath(fullPath, rootPath) {
		if !strings.HasPrefix(path, "$") {
			if !utils.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}
	return cleanEmpty(paths)
}
