// Package domain contains the core business entities for Boss dependency manager.
// It defines Package, Dependency, Lock file structures and their associated operations.
package domain

import (
	"strings"
)

// Package represents the bossy.json file structure.
// This is a pure domain entity containing only business data and logic.
// Use PackageRepository (ports.PackageRepository) for persistence operations.
type Package struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	Version      string              `json:"version"`
	Homepage     string              `json:"homepage"`
	MainSrc      string              `json:"mainsrc"`
	BrowsingPath string              `json:"browsingpath"`
	Projects     []string            `json:"projects"`
	SearchPaths  map[string][]string `json:"searchpaths,omitempty"`
	Scripts      map[string]string   `json:"scripts,omitempty"`
	Dependencies map[string]string   `json:"dependencies"`
	Engines      *PackageEngines     `json:"engines,omitempty"`
	Toolchain    *PackageToolchain   `json:"toolchain,omitempty"`
	Lock         PackageLock         `json:"-"`
}

// PackageEngines represents the engines configuration in bossy.json.
type PackageEngines struct {
	Compiler  string   `json:"compiler,omitempty"`
	Platforms []string `json:"platforms,omitempty"`
}

// PackageToolchain represents the toolchain configuration in bossy.json.
type PackageToolchain struct {
	Compiler string `json:"compiler,omitempty"`
	Platform string `json:"platform,omitempty"`
	Path     string `json:"path,omitempty"`
	Strict   bool   `json:"strict,omitempty"`
}

// NewPackage creates a new Package with initialized collections.
func NewPackage() *Package {
	return &Package{
		Dependencies: make(map[string]string),
		Projects:     []string{},
	}
}

// AddDependency adds or updates a dependency in the package.
func (p *Package) AddDependency(dep string, ver string) {
	for key := range p.Dependencies {
		if strings.EqualFold(key, dep) {
			p.Dependencies[key] = ver
			return
		}
	}

	p.Dependencies[dep] = ver
}

// ModuleSearchPaths returns the search-path restriction declared for a module
// directory, if the manifest declares one.
//
// Without a restriction bossy walks a dependency's whole tree and puts every
// directory holding compilable source on the Delphi search path. For a
// repository that ships its library next to samples, demos and unit tests —
// delphimvcframework being the obvious case — that is hundreds of entries,
// which both hides the real source directories and can push dcc's command
// line past the Windows length limit. Naming the directories that actually
// hold the library is the way out.
//
// Keys are matched on the module directory name, so a restriction may be
// written as the full dependency key ("github.com/danieleteti/delphimvcframework")
// or as the bare module name ("delphimvcframework"). Paths are relative to
// the module directory and are still walked recursively.
func (p *Package) ModuleSearchPaths(moduleName string) ([]string, bool) {
	if p == nil || len(p.SearchPaths) == 0 {
		return nil, false
	}
	want := NormalizeModuleKey(moduleName)
	for key, paths := range p.SearchPaths {
		if NormalizeModuleKey(key) == want {
			return paths, true
		}
	}
	return nil, false
}

// NormalizeModuleKey reduces a dependency reference to the lowercase module
// directory name bossy installs it under, so the several ways of writing the
// same dependency all resolve to one key.
func NormalizeModuleKey(key string) string {
	s := strings.TrimSuffix(strings.TrimSpace(key), "/")
	s = strings.TrimSuffix(s, ".git")
	name := reDepName.FindString(s)
	return strings.ToLower(strings.Trim(name, "/"))
}

// AddProject adds a project to the package.
func (p *Package) AddProject(project string) {
	p.Projects = append(p.Projects, project)
}

// GetParsedDependencies returns the dependencies parsed as Dependency objects.
func (p *Package) GetParsedDependencies() []Dependency {
	if p == nil || len(p.Dependencies) == 0 {
		return []Dependency{}
	}
	return GetDependencies(p.Dependencies)
}

// UninstallDependency removes a dependency from the package.
func (p *Package) UninstallDependency(dep string) {
	if p.Dependencies != nil {
		for key := range p.Dependencies {
			if strings.EqualFold(key, dep) {
				delete(p.Dependencies, key)
				return
			}
		}
	}
}
