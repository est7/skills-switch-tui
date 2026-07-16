package userresource

import (
	"fmt"

	"github.com/est7/skills-switch-tui/internal/client"
)

type TargetScope string

const (
	TargetProject TargetScope = "project"
	TargetUser    TargetScope = "user"
)

// Descriptor is the single source of truth for a managed file-resource kind.
// Presentation layers may add labels, but must not duplicate capability,
// storage directory, or target-scope decisions.
type Descriptor struct {
	Kind           Kind
	Directory      string
	Command        string
	CommandSummary string
	Capability     client.Capability
	TargetScope    TargetScope
	BootstrapScope string
	targetDir      func(client.Registry, string, client.ID) (string, error)
}

var descriptors = []Descriptor{
	{
		Kind:           KindCommand,
		Directory:      "commands",
		Command:        "commands",
		CommandSummary: "Manage project command files",
		Capability:     client.CapabilityCommands,
		TargetScope:    TargetProject,
		BootstrapScope: "shared",
		targetDir:      client.Registry.ProjectCommandsTargetDir,
	},
	{
		Kind:           KindHook,
		Directory:      "hooks",
		Command:        "hooks",
		CommandSummary: "Manage project hook files",
		Capability:     client.CapabilityHooks,
		TargetScope:    TargetProject,
		BootstrapScope: "shared",
		targetDir:      client.Registry.ProjectHooksTargetDir,
	},
	{
		Kind:           KindAgent,
		Directory:      "agents",
		Command:        "agents",
		CommandSummary: "Manage user-global agent files",
		Capability:     client.CapabilityAgents,
		TargetScope:    TargetUser,
		BootstrapScope: "shared",
		targetDir:      client.Registry.UserAgentsTargetDir,
	},
	{
		Kind:           KindOutputStyle,
		Directory:      "output-styles",
		Command:        "output-styles",
		CommandSummary: "Manage user-global output style files",
		Capability:     client.CapabilityOutputStyles,
		TargetScope:    TargetUser,
		BootstrapScope: "claude-only",
		targetDir:      client.Registry.UserOutputStylesTargetDir,
	},
}

func Descriptors() []Descriptor {
	return append([]Descriptor(nil), descriptors...)
}

func Describe(kind Kind) (Descriptor, error) {
	for _, descriptor := range descriptors {
		if descriptor.Kind == kind {
			return descriptor, nil
		}
	}
	return Descriptor{}, fmt.Errorf("unknown user resource kind %q", kind)
}

func (d Descriptor) TargetDir(registry client.Registry, base string, clientID client.ID) (string, error) {
	if d.targetDir == nil {
		return "", fmt.Errorf("user resource kind %q has no target directory resolver", d.Kind)
	}
	return d.targetDir(registry, base, clientID)
}
