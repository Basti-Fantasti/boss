// Package librarypath provides utilities for manipulating Delphi .dproj files.
// This file contains XML manipulation functions for updating library paths.
package librarypath

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/beevik/etree"
)

const (
	crlf = "\r\n"
	lf   = "\n"
)

var (
	//nolint:lll // Regex pattern readability is important
	reProjectFile = regexp.MustCompile(`.*` + regexp.QuoteMeta(consts.FileExtensionDproj) + `|.*` + regexp.QuoteMeta(consts.FileExtensionLpi) + `$`)
	reLazarusFile = regexp.MustCompile(`.*` + regexp.QuoteMeta(consts.FileExtensionLpi) + `$`)
)

// updateDprojLibraryPath updates the library path in the project file.
func updateDprojLibraryPath(pkg *domain.Package) {
	var isLazarus = isLazarus()
	var projectNames = GetProjectNames(pkg)
	for _, projectName := range projectNames {
		if isLazarus {
			updateOtherUnitFilesProject(pkg, projectName)
		} else {
			updateLibraryPathProject(pkg, projectName)
		}
	}
}

// updateOtherUnitFilesProject updates the other unit files in the project file.
func updateOtherUnitFilesProject(pkg *domain.Package, lpiName string) {
	doc := etree.NewDocument()
	info, err := os.Stat(lpiName)
	if os.IsNotExist(err) || info.IsDir() {
		msg.Err("❌ .lpi not found.")
		return
	}
	original, err := os.ReadFile(lpiName)
	if err != nil {
		msg.Err("❌ Error on read lpi: %s", err)
		return
	}
	if err = doc.ReadFromBytes(original); err != nil {
		msg.Err("❌ Error on read lpi: %s", err)
		return
	}

	root := doc.Root()

	compilerOptions := root.SelectElement(consts.XMLTagNameCompilerOptions)
	processCompilerOptions(pkg, compilerOptions)

	projectOptions := root.SelectElement(consts.XMLTagNameProjectOptions)

	buildModes := projectOptions.SelectElement(consts.XMLTagNameBuildModes)
	for _, item := range buildModes.SelectElements(consts.XMLTagNameItem) {
		attribute := item.SelectAttr(consts.XMLNameAttribute)
		compilerOptions = item.SelectElement(consts.XMLTagNameCompilerOptions)
		if compilerOptions != nil {
			msg.Info("  🔁 Updating %s mode", attribute.Value)
			processCompilerOptions(pkg, compilerOptions)
		}
	}

	doc.WriteSettings.CanonicalAttrVal = true
	doc.WriteSettings.CanonicalEndTags = false
	doc.WriteSettings.CanonicalText = true

	if err = writeKeepingLineEndings(doc, lpiName, original, info.Mode()); err != nil {
		msg.Err("❌ Failed to write .lpi file: %v", err)
	}
}

// processCompilerOptions processes the compiler options.
func processCompilerOptions(pkg *domain.Package, compilerOptions *etree.Element) {
	searchPaths := compilerOptions.SelectElement(consts.XMLTagNameSearchPaths)
	if searchPaths == nil {
		return
	}
	otherUnitFiles := searchPaths.SelectElement(consts.XMLTagNameOtherUnitFiles)
	if otherUnitFiles == nil {
		otherUnitFiles = createTagOtherUnitFiles(searchPaths)
	}
	value := otherUnitFiles.SelectAttr("Value")
	currentPaths := strings.Split(value.Value, ";")
	currentPaths = GetNewPaths(pkg, currentPaths, false, env.GetCurrentDir())
	value.Value = strings.Join(currentPaths, ";")
}

// createTagOtherUnitFiles creates the other unit files tag.
func createTagOtherUnitFiles(node *etree.Element) *etree.Element {
	child := node.CreateElement(consts.XMLTagNameOtherUnitFiles)
	child.CreateAttr("Value", "")
	return child
}

// updateGlobalBrowsingPath updates the global browsing path.
func updateGlobalBrowsingPath(pkg *domain.Package) {
	var isLazarus = isLazarus()
	var projectNames = GetProjectNames(pkg)
	for i, projectName := range projectNames {
		if !isLazarus {
			updateGlobalBrowsingByProject(projectName, i == 0)
		}
	}
}

// updateLibraryPathProject updates the library path in the project file.
func updateLibraryPathProject(pkg *domain.Package, dprojName string) {
	doc := etree.NewDocument()
	info, err := os.Stat(dprojName)
	if os.IsNotExist(err) || info.IsDir() {
		msg.Err("❌ .dproj not found.")
		return
	}
	original, err := os.ReadFile(dprojName)
	if err != nil {
		msg.Err("❌ Error on read dproj: %s", err)
		return
	}
	if err = doc.ReadFromBytes(original); err != nil {
		msg.Err("❌ Error on read dproj: %s", err)
		return
	}
	root := doc.Root()

	childrens := root.FindElements(consts.XMLTagNameProperty)
	for _, children := range childrens {
		attribute := children.SelectAttr(consts.XMLTagNamePropertyAttribute)
		if attribute != nil && attribute.Value == consts.XMLTagNamePropertyAttributeValue {
			child := children.SelectElement(consts.XMLTagNameLibraryPath)
			if child == nil {
				child = createTagLibraryPath(children)
			}
			rootPath := filepath.Join(env.GetCurrentDir(), path.Dir(dprojName))
			if _, err = os.Stat(rootPath); os.IsNotExist(err) {
				rootPath = env.GetCurrentDir()
			}
			processCurrentPath(pkg, child, rootPath)
		}
	}

	doc.WriteSettings.CanonicalAttrVal = true
	doc.WriteSettings.CanonicalEndTags = false
	doc.WriteSettings.CanonicalText = true

	if err = writeKeepingLineEndings(doc, dprojName, original, info.Mode()); err != nil {
		msg.Err("❌ Failed to write .dproj file: %v", err)
	}
}

// detectLineEnding reports the line ending a file already uses. A file holding
// no line break at all is treated as LF, which is what etree writes.
func detectLineEnding(content []byte) string {
	if bytes.Contains(content, []byte(crlf)) {
		return crlf
	}
	return lf
}

// writeKeepingLineEndings writes doc to name with the line ending original
// already used.
//
// The XML spec requires a parser to normalise CRLF to LF, so a project file
// saved by the Delphi IDE — which writes CRLF — comes back from etree as LF and
// would be rewritten line for line. Beyond the noise in an IDE user's `git
// status`, that breaks a build server: with core.autocrlf=true the runner
// checks the file out as CRLF, and a CI step comparing the bytes before and
// after `bossy install` sees a project file that no longer matches the one in
// the repository. `git diff` cannot see it, because git normalises line endings
// before comparing.
func writeKeepingLineEndings(doc *etree.Document, name string, original []byte, mode os.FileMode) error {
	out, err := doc.WriteToBytes()
	if err != nil {
		return err
	}
	if detectLineEnding(original) == crlf {
		// etree writes LF only, so this cannot produce CRCRLF.
		out = bytes.ReplaceAll(out, []byte(lf), []byte(crlf))
	}
	return os.WriteFile(name, out, mode.Perm())
}

// createTagLibraryPath creates the library path tag.
func createTagLibraryPath(node *etree.Element) *etree.Element {
	child := node.CreateElement(consts.XMLTagNameLibraryPath)
	return child
}

// GetProjectNames returns the project names.
func GetProjectNames(pkg *domain.Package) []string {
	var result []string

	if len(pkg.Projects) > 0 {
		result = pkg.Projects
	} else {
		files, err := os.ReadDir(env.GetCurrentDir())
		if err != nil {
			msg.Err("❌ Failed to read directory: %v", err)
			return result
		}

		for _, file := range files {
			if reProjectFile.MatchString(file.Name()) {
				result = append(result, filepath.Join(env.GetCurrentDir(), file.Name()))
			}
		}
	}

	return result
}

// isLazarus checks if the project is a Lazarus project.
func isLazarus() bool {
	files, err := os.ReadDir(env.GetCurrentDir())
	if err != nil {
		msg.Debug("⚠️ Failed to check for Lazarus project: %v", err)
		return false
	}

	for _, file := range files {
		matched := reLazarusFile.MatchString(file.Name())
		if matched {
			return true
		}
	}
	return false
}

// processCurrentPath processes the current path.
func processCurrentPath(pkg *domain.Package, node *etree.Element, rootPath string) {
	currentPaths := strings.Split(node.Text(), ";")

	currentPaths = GetNewPaths(pkg, currentPaths, false, rootPath)

	node.SetText(strings.Join(currentPaths, ";"))
}
