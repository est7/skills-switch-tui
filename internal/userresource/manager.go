package userresource

import (
	"fmt"
	"path/filepath"

	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/linkprojection"
)

type State string

const (
	StateDisabled     State = "disabled"
	StateEnabled      State = "enabled"
	StateConflict     State = "conflict"
	StateBroken       State = "broken"
	StateIncompatible State = "incompatible"
)

type Operation struct {
	Resource Resource
	Client   client.ID
	Enabled  bool
}

type Manager struct {
	userHome string
	clients  client.Registry
}

func NewManager(userHome string, clients client.Registry) Manager {
	return Manager{userHome: userHome, clients: clients}
}

func (m Manager) State(resource Resource, clientID client.ID) (State, error) {
	file, supported, err := m.file(resource, clientID)
	if err != nil {
		return "", err
	}
	if !supported {
		return StateIncompatible, nil
	}
	state, err := (linkprojection.Manager{Label: string(resource.Kind)}).State([]linkprojection.File{file})
	return State(state), err
}

func (m Manager) TargetPath(resource Resource, clientID client.ID) (string, error) {
	file, supported, err := m.file(resource, clientID)
	if err != nil {
		return "", err
	}
	if !supported {
		return "", fmt.Errorf("%s %s is incompatible with client %s", resource.Kind, resource.ID, clientID)
	}
	return file.TargetPath, nil
}

func (m Manager) Apply(operations []Operation) error {
	filesByState := map[bool][]linkprojection.File{true: {}, false: {}}
	for _, operation := range operations {
		file, supported, err := m.file(operation.Resource, operation.Client)
		if err != nil {
			return err
		}
		if !supported {
			return fmt.Errorf("%s %s is incompatible with client %s", operation.Resource.Kind, operation.Resource.ID, operation.Client)
		}
		filesByState[operation.Enabled] = append(filesByState[operation.Enabled], file)
	}
	label := "user resource"
	if len(operations) > 0 {
		label = string(operations[0].Resource.Kind)
	}
	links := linkprojection.Manager{Label: label}
	// Disable first so an atomic provider switch can release a target before a
	// different source claims it. Each phase still preflights and rolls back as
	// one file-projection transaction.
	if err := links.SetEnabled(filesByState[false], false); err != nil {
		return err
	}
	if err := links.SetEnabled(filesByState[true], true); err != nil {
		if rollbackErr := links.SetEnabled(filesByState[false], true); rollbackErr != nil {
			return fmt.Errorf("%w; restore disabled projections: %v", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (m Manager) file(resource Resource, clientID client.ID) (linkprojection.File, bool, error) {
	if !resource.Supports(clientID) {
		return linkprojection.File{}, false, nil
	}
	target, err := targetRoot(m.clients, m.userHome, resource.Kind, clientID)
	if err != nil {
		return linkprojection.File{}, false, nil
	}
	return linkprojection.File{
		SourcePath: resource.SourcePath,
		TargetPath: filepath.Join(target, resource.RelativePath),
	}, true, nil
}
