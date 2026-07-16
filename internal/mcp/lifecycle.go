package mcp

import (
	"errors"
	"fmt"

	"github.com/est7/skills-switch-tui/internal/catalog"
)

// RemoveWithProjections retires every enabled project projection before
// deleting the shared catalog provider, and restores those projections if the
// catalog mutation fails.
func RemoveWithProjections(manager Manager, catalogPath, name string, clients []catalog.Client) error {
	disable := make([]Operation, 0, len(clients))
	restore := make([]Operation, 0, len(clients))
	for _, clientID := range clients {
		state, err := manager.State(name, clientID)
		if err != nil {
			return fmt.Errorf("inspect MCP projection for %s: %w", clientID, err)
		}
		if state == StateEnabled {
			disable = append(disable, Operation{Server: name, Client: clientID, Enabled: false})
			restore = append(restore, Operation{Server: name, Client: clientID, Enabled: true})
		}
	}
	if err := manager.Apply(disable); err != nil {
		return fmt.Errorf("retire MCP projections: %w", err)
	}
	if err := RemoveServer(catalogPath, name); err != nil {
		if restoreErr := manager.Apply(restore); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore MCP projections after failed removal: %w", restoreErr))
		}
		return err
	}
	return nil
}
