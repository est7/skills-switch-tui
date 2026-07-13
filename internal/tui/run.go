package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func Run(model Model) error {
	defer model.cancel()
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}
