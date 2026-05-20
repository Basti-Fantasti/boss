// Package cli provides command-line interface implementation for Boss.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/basti-fantasti/bossy/pkg/env"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/pkg/pkgmanager"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var reFolderName = regexp.MustCompile(`^.+` + regexp.QuoteMeta(string(filepath.Separator)) + `([^\\]+)$`)

// initCmdRegister registers the init command.
func initCmdRegister(root *cobra.Command) {
	var quiet bool

	var initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize a new project",
		Long:  "Initialize a new project and creates a bossy.json file",
		Example: `  Initialize a new project:
  bossy init

  Initialize a new project without having it ask any questions:
  bossy init --quiet`,
		Run: func(_ *cobra.Command, _ []string) {
			doInitialization(quiet)
		},
	}

	initCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "without asking questions")

	root.AddCommand(initCmd)
}

// doInitialization initializes the project.
func doInitialization(quiet bool) {
	if !quiet {
		printHead()
	}

	packageData, err := pkgmanager.LoadPackage()
	if err != nil && !os.IsNotExist(err) {
		msg.Die("Fail on open dependencies file: %s", err)
	}

	allString := reFolderName.FindAllStringSubmatch(env.GetCurrentDir(), -1)
	folderName := allString[0][1]

	if quiet {
		packageData.Name = folderName
		packageData.Version = "1.0.0"
		packageData.MainSrc = "./src"
	} else {
		packageData.Name = getParamOrDef("Package name ("+folderName+")", folderName)
		packageData.Homepage = getParamOrDef("Homepage", "")
		packageData.Version = getParamOrDef("Version (1.0.0)", "1.0.0")
		packageData.Description = getParamOrDef("Description", "")
		packageData.MainSrc = getParamOrDef("Source folder (./src)", "./src")
	}

	if err := pkgmanager.SavePackageCurrent(packageData); err != nil {
		msg.Die("Failed to save package: %v", err)
	}

	jsonData, errMarshal := json.MarshalIndent(packageData, "", "  ")
	if errMarshal != nil {
		msg.Die("Failed to marshal package: %v", errMarshal)
	}
	msg.Info("\n" + string(jsonData))
}

// getParamOrDef gets the parameter or default value.
func getParamOrDef(msg string, def ...string) string {
	input := &pterm.DefaultInteractiveTextInput

	if len(def) > 0 {
		input = input.WithDefaultValue(def[0])
	}

	result, _ := input.Show(msg)

	return result
}

// printHead prints the head message.
func printHead() {
	msg.Info(`
This utility will walk you through creating a bossy.json file.
It only covers the most common items, and tries to guess sensible defaults.

Use 'bossy install <pkg>' afterwards to install a package and
save it as a dependency in the bossy.json file.

Press ^C at any time to quit.
`)
}
