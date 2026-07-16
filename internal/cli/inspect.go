package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
	"github.com/est7/skills-switch-tui/internal/tui"
	"github.com/est7/skills-switch-tui/internal/userresource"
	"github.com/spf13/cobra"
)

func newTUICommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive terminal UI",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTUI(options)
		},
	}
}

func runTUI(options *rootOptions) error {
	runtime, err := loadRuntime(options)
	if err != nil {
		return err
	}
	userResources := make(map[userresource.Kind]tui.UserResourceSet, len(runtime.userResources))
	for kind, managed := range runtime.userResources {
		userResources[kind] = tui.UserResourceSet{Catalog: managed.catalog, Manager: managed.manager}
	}
	model := tui.NewModel(runtime.catalog, runtime.projectRoot, runtime.projection, &runtime.manager, runtime.translator, tui.Resources{
		MCPCatalog:    runtime.mcpCatalog,
		MCPManager:    runtime.mcpManager,
		Prompts:       runtime.prompts,
		PromptManager: runtime.promptMgr,
		UserResources: userResources,
		UserHome:      runtime.userHome,
	})
	return tui.Run(model)
}

type clientProjectionView struct {
	Supported bool   `json:"supported"`
	State     string `json:"state"`
	Target    string `json:"target"`
}

type showOutput struct {
	ID            string                          `json:"id"`
	Name          string                          `json:"name"`
	Description   string                          `json:"description,omitempty"`
	MetadataIssue string                          `json:"metadataIssue,omitempty"`
	Source        string                          `json:"source"`
	Path          string                          `json:"path"`
	Archived      bool                            `json:"archived"`
	Clients       map[string]clientProjectionView `json:"clients"`
}

func newShowCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:     "show <skill-id>",
		Aliases: []string{"query"},
		Short:   "Show one skill and its project projections",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			skill, ok := runtime.catalog.Skill(args[0])
			if !ok {
				return errors.New(runtime.translator.Text(i18n.UnknownSkill, args[0]))
			}
			source, _ := runtime.catalog.Source(skill.SourceID)
			result := showOutput{
				ID:            skill.ID,
				Name:          skill.Name,
				Description:   skill.Description,
				MetadataIssue: skill.MetadataIssue,
				Source:        skill.SourceID,
				Path:          skill.Path,
				Archived:      source.IsArchived(),
				Clients:       make(map[string]clientProjectionView),
			}
			for _, client := range skillClientIDs(runtime.catalog.Clients, projection.ScopeProject) {
				state, stateErr := runtime.projection.State(skill, client)
				if stateErr != nil {
					return stateErr
				}
				target, targetErr := runtime.projection.TargetPath(skill, client)
				if targetErr != nil {
					return targetErr
				}
				result.Clients[string(client)] = clientProjectionView{
					Supported: skill.Supports(client),
					State:     string(state),
					Target:    target,
				}
			}
			if outputJSON {
				return writeJSON(command, result)
			}
			fmt.Fprintf(command.OutOrStdout(), "%s\n%s\n%s\n", result.ID, result.Description, result.Path)
			if result.MetadataIssue != "" {
				fmt.Fprintf(command.OutOrStdout(), "%s: %s\n", runtime.translator.Text(i18n.MetadataIssueLabel), result.MetadataIssue)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\n", runtime.translator.Text(i18n.ClientHeader), runtime.translator.Text(i18n.StateHeader), runtime.translator.Text(i18n.PathHeader))
			for _, client := range skillClientIDs(runtime.catalog.Clients, projection.ScopeProject) {
				view := result.Clients[string(client)]
				fmt.Fprintf(writer, "%s\t%s\t%s\n", client, view.State, view.Target)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

type clientStatus struct {
	Client       string `json:"client"`
	Enabled      int    `json:"enabled"`
	Disabled     int    `json:"disabled"`
	Issues       int    `json:"issues"`
	Incompatible int    `json:"incompatible"`
}

type statusOutput struct {
	Project string         `json:"project"`
	Clients []clientStatus `json:"clients"`
}

func newStatusCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Summarize project projection state by client",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			result, err := buildStatus(runtime)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(command, result)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
				runtime.translator.Text(i18n.ClientHeader),
				runtime.translator.Text(i18n.EnabledHeader),
				runtime.translator.Text(i18n.DisabledHeader),
				runtime.translator.Text(i18n.IssuesHeader),
				runtime.translator.Text(i18n.IncompatibleHeader),
			)
			for _, client := range result.Clients {
				fmt.Fprintf(writer, "%s\t%d\t%d\t%d\t%d\n", client.Client, client.Enabled, client.Disabled, client.Issues, client.Incompatible)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

func buildStatus(runtime runtime) (statusOutput, error) {
	result := statusOutput{Project: runtime.projectRoot}
	for _, client := range skillClientIDs(runtime.catalog.Clients, projection.ScopeProject) {
		summary := clientStatus{Client: string(client)}
		for _, source := range runtime.catalog.Sources {
			if source.IsArchived() {
				continue
			}
			for _, skill := range source.Skills {
				state, err := runtime.projection.State(skill, client)
				if err != nil {
					return statusOutput{}, err
				}
				switch state {
				case projection.StateEnabled, projection.StateGlobal:
					summary.Enabled++
				case projection.StateDisabled:
					summary.Disabled++
				case projection.StateIncompatible:
					summary.Incompatible++
				case projection.StateIncompatibleEnabled, projection.StateConflict, projection.StateBroken, projection.StateDuplicate:
					summary.Issues++
				}
			}
		}
		result.Clients = append(result.Clients, summary)
	}
	return result, nil
}

type doctorIssue struct {
	Kind     string `json:"kind"`
	Resource string `json:"resource"`
	Client   string `json:"client"`
	Scope    string `json:"scope,omitempty"`
	State    string `json:"state"`
	Path     string `json:"path"`
	Detail   string `json:"detail,omitempty"`
}

type doctorOutput struct {
	Healthy       bool          `json:"healthy"`
	Project       string        `json:"project"`
	ResourcesRoot string        `json:"resourcesRoot"`
	Issues        []doctorIssue `json:"issues"`
}

func newDoctorCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check catalog and project projection health",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			result, err := buildDoctor(runtime)
			if err != nil {
				return err
			}
			if outputJSON {
				if err := writeJSON(command, result); err != nil {
					return err
				}
			} else if result.Healthy {
				fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.DoctorHealthy))
			} else {
				writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", runtime.translator.Text(i18n.KindHeader), runtime.translator.Text(i18n.ResourceHeader), runtime.translator.Text(i18n.ClientHeader), runtime.translator.Text(i18n.ScopeHeader), runtime.translator.Text(i18n.StateHeader), runtime.translator.Text(i18n.PathHeader))
				for _, issue := range result.Issues {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", issue.Kind, issue.Resource, issue.Client, issue.Scope, issue.State, issue.Path)
				}
				if err := writer.Flush(); err != nil {
					return err
				}
			}
			if !result.Healthy {
				return errors.New(runtime.translator.Text(i18n.DoctorFoundIssues, len(result.Issues)))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

func buildDoctor(runtime runtime) (doctorOutput, error) {
	result := doctorOutput{
		Healthy:       true,
		Project:       runtime.projectRoot,
		ResourcesRoot: runtime.resources.Root,
	}
	for _, source := range runtime.catalog.Sources {
		if source.IsArchived() {
			continue
		}
		if source.IsCheckoutMissing() {
			result.Healthy = false
			result.Issues = append(result.Issues, doctorIssue{
				Kind: "source", Resource: source.ID, State: string(catalog.SourceCheckoutMissing), Path: source.Path,
				Detail: "configured vendor checkout is missing; run source update " + source.ID,
			})
		}
		for _, skill := range source.Skills {
			for _, client := range skillClientIDs(runtime.catalog.Clients, projection.ScopeProject) {
				state, err := runtime.projection.State(skill, client)
				if err != nil {
					return doctorOutput{}, err
				}
				if state == projection.StateIncompatibleEnabled || state == projection.StateConflict || state == projection.StateBroken || state == projection.StateDuplicate {
					path, pathErr := runtime.projection.TargetPath(skill, client)
					if pathErr != nil {
						return doctorOutput{}, pathErr
					}
					result.Healthy = false
					detail := projectionHealthDetail(runtime.projection, skill, client, projection.ScopeProject)
					result.Issues = append(result.Issues, doctorIssue{Kind: "skill", Resource: skill.ID, Client: string(client), Scope: string(projection.ScopeProject), State: string(state), Path: path, Detail: detail})
				}
				if runtime.projection.SupportsScope(client, projection.ScopeGlobal) {
					state, err := runtime.projection.StateAt(skill, client, projection.ScopeGlobal)
					if err != nil {
						return doctorOutput{}, err
					}
					if state == projection.StateIncompatibleEnabled || state == projection.StateConflict || state == projection.StateBroken {
						path, pathErr := runtime.projection.TargetPathAt(skill, client, projection.ScopeGlobal)
						if pathErr != nil {
							return doctorOutput{}, pathErr
						}
						result.Healthy = false
						detail := projectionHealthDetail(runtime.projection, skill, client, projection.ScopeGlobal)
						result.Issues = append(result.Issues, doctorIssue{Kind: "skill", Resource: skill.ID, Client: string(client), Scope: string(projection.ScopeGlobal), State: string(state), Path: path, Detail: detail})
					}
				}
			}
		}
	}
	orphans, err := runtime.projection.OrphanedProjections(activeSources(runtime.catalog))
	if err != nil {
		return doctorOutput{}, err
	}
	for _, orphan := range orphans {
		result.Healthy = false
		result.Issues = append(result.Issues, doctorIssue{Kind: "skill", Resource: orphan.Name, Client: string(orphan.Client), Scope: string(projection.ScopeProject), State: "orphaned", Path: orphan.Path})
	}
	globalOrphans, err := runtime.projection.OrphanedProjectionsAt(activeSources(runtime.catalog), projection.ScopeGlobal)
	if err != nil {
		return doctorOutput{}, err
	}
	for _, orphan := range globalOrphans {
		result.Healthy = false
		result.Issues = append(result.Issues, doctorIssue{Kind: "skill", Resource: orphan.Name, Client: string(orphan.Client), Scope: string(projection.ScopeGlobal), State: "orphaned", Path: orphan.Path})
	}
	for _, name := range runtime.mcpCatalog.Names() {
		server, _ := runtime.mcpCatalog.Server(name)
		if secretIssues := mcp.SecretIssues(server); len(secretIssues) > 0 {
			result.Healthy = false
			result.Issues = append(result.Issues, doctorIssue{
				Kind: "mcp-catalog", Resource: name, State: "plaintext-secret", Path: runtime.mcpCatalog.Path,
				Detail: "replace " + strings.Join(secretIssues, ", ") + " with ${ENV_VAR} references",
			})
		}
		for _, clientID := range runtime.catalog.Clients.IDsFor(client.CapabilityMCP) {
			state, err := runtime.mcpManager.State(name, clientID)
			if err != nil {
				return doctorOutput{}, err
			}
			if state != mcp.StateConflict {
				continue
			}
			path, _, err := runtime.catalog.Clients.MCPProjectFile(runtime.projectRoot, clientID)
			if err != nil {
				return doctorOutput{}, err
			}
			result.Healthy = false
			result.Issues = append(result.Issues, doctorIssue{Kind: "mcp", Resource: name, Client: string(clientID), State: string(state), Path: path})
		}
	}
	for _, group := range runtime.prompts.Groups {
		state, err := runtime.promptMgr.State(group)
		if err != nil {
			return doctorOutput{}, err
		}
		if state != systemprompt.StatePartial && state != systemprompt.StateConflict && state != systemprompt.StateBroken && state != systemprompt.StateStale {
			continue
		}
		path, err := runtime.catalog.Clients.UserPromptTargetDir(runtime.userHome, group.Client)
		if err != nil {
			return doctorOutput{}, err
		}
		result.Healthy = false
		result.Issues = append(result.Issues, doctorIssue{Kind: "system-prompt", Resource: group.ID, Client: string(group.Client), State: string(state), Path: path})
	}
	for _, descriptor := range userresource.Descriptors() {
		managed := runtime.userResources[descriptor.Kind]
		for _, resource := range managed.catalog.Resources {
			for _, clientID := range runtime.catalog.Clients.IDsFor(descriptor.Capability) {
				state, err := managed.manager.State(resource, clientID)
				if err != nil {
					return doctorOutput{}, err
				}
				if state != userresource.StateConflict && state != userresource.StateBroken {
					continue
				}
				path, err := managed.manager.TargetPath(resource, clientID)
				if err != nil {
					return doctorOutput{}, err
				}
				result.Healthy = false
				result.Issues = append(result.Issues, doctorIssue{Kind: string(descriptor.Kind), Resource: resource.ID, Client: string(clientID), State: string(state), Path: path})
			}
		}
	}
	return result, nil
}

func projectionHealthDetail(manager projection.Manager, skill catalog.Skill, clientID catalog.Client, scope projection.Scope) string {
	health, err := manager.HealthAt(skill, clientID, scope)
	if err != nil {
		return err.Error()
	}
	if health.Cause != nil {
		return health.Cause.Error()
	}
	return ""
}

func writeJSON(command *cobra.Command, value any) error {
	encoder := json.NewEncoder(command.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
