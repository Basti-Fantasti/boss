package tui

import (
	"errors"
	"fmt"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	tea "github.com/charmbracelet/bubbletea"
)

// ErrCancelled is returned when the user aborted the picker.
var ErrCancelled = errors.New("selection cancelled")

// Run opens the dependency picker and returns the user's selections.
//
// The caller is responsible for having established that this is an interactive
// terminal; a picker started without one would block on a keystroke that never
// arrives.
func Run(presets []domain.Preset, lister RefLister) ([]Selection, error) {
	program := tea.NewProgram(newModel(presets, lister))

	finished, err := program.Run()
	if err != nil {
		return nil, fmt.Errorf("run dependency picker: %w", err)
	}

	model, ok := finished.(rootModel)
	if !ok {
		return nil, errors.New("dependency picker returned an unexpected model")
	}
	if model.cancelled {
		return nil, ErrCancelled
	}

	return model.Selections(), nil
}
