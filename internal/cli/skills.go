package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/spf13/cobra"
)

func newSkillsCommand(options *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   "List catalog skills and delete local skills or groups",
		Args:    cobra.NoArgs,
	}
	command.AddCommand(newListCommand(options))
	command.AddCommand(newSkillDiscoverCommand(options))
	command.AddCommand(newSkillAdoptCommand(options))
	command.AddCommand(newShowCommand(options))
	command.AddCommand(newEnableCommand(options, true))
	command.AddCommand(newEnableCommand(options, false))
	command.AddCommand(newSkillCreateCommand(options))
	command.AddCommand(newSkillDeleteCommand(options))
	command.AddCommand(newSkillPruneCommand(options))
	return command
}

type adoptedSkillOutput struct {
	Path     string                    `json:"path"`
	SkillID  string                    `json:"skillId"`
	SSOTPath string                    `json:"ssotPath"`
	Status   projection.AdoptionStatus `json:"status"`
	Reason   string                    `json:"reason"`
}

func adoptionStatusKey(status projection.AdoptionStatus) i18n.Key {
	switch status {
	case projection.AdoptionAdopted:
		return i18n.AdoptAdopted
	case projection.AdoptionFailed:
		return i18n.AdoptFailed
	case projection.AdoptionStranded:
		return i18n.AdoptStranded
	default:
		return i18n.AdoptRefused
	}
}

func newSkillAdoptCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	var scope, group string
	command := &cobra.Command{
		Use:   "adopt <path>...",
		Short: "Adopt unmanaged Skills into the local catalog",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			adoptions, adoptErr := runtime.projection.AdoptSkills(args, scope, group)
			result := make([]adoptedSkillOutput, 0, len(adoptions))
			for _, adoption := range adoptions {
				result = append(result, adoptedSkillOutput{
					Path:     adoption.Path,
					SkillID:  adoption.SkillID,
					SSOTPath: adoption.SSOTPath,
					Status:   adoption.Status,
					Reason:   adoption.Reason,
				})
			}
			if outputJSON {
				return errors.Join(adoptErr, writeJSON(command, result))
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
				runtime.translator.Text(i18n.PathHeader),
				runtime.translator.Text(i18n.SkillHeader),
				runtime.translator.Text(i18n.TargetHeader),
				runtime.translator.Text(i18n.StateHeader),
				runtime.translator.Text(i18n.ReasonHeader),
			)
			for _, adoption := range result {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					adoption.Path,
					adoption.SkillID,
					adoption.SSOTPath,
					runtime.translator.Text(adoptionStatusKey(adoption.Status)),
					adoption.Reason,
				)
			}
			return errors.Join(adoptErr, writer.Flush())
		},
	}
	command.Flags().StringVar(&scope, "scope", "shared", "local scope: shared or a registered client id")
	command.Flags().StringVar(&group, "group", "", "group directory (default: a standalone group named after the Skill)")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

type discoveredSkillOutput struct {
	Client string `json:"client"`
	Scope  string `json:"scope"`
	Name   string `json:"name"`
	Path   string `json:"path"`
}

func newSkillDiscoverCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	var rawScope string
	command := &cobra.Command{
		Use:   "discover",
		Short: "Discover unmanaged Skills in registered client target directories",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			scopes, err := parseSkillDiscoveryScopes(rawScope)
			if err != nil {
				return err
			}
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			discovered, err := runtime.projection.DiscoverUnmanagedSkills(scopes...)
			if err != nil {
				return err
			}
			result := make([]discoveredSkillOutput, 0, len(discovered))
			for _, skill := range discovered {
				result = append(result, discoveredSkillOutput{
					Client: string(skill.Client),
					Scope:  string(skill.Scope),
					Name:   skill.Name,
					Path:   skill.Path,
				})
			}
			if outputJSON {
				return writeJSON(command, result)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
				runtime.translator.Text(i18n.ClientHeader),
				runtime.translator.Text(i18n.ScopeHeader),
				runtime.translator.Text(i18n.NameHeader),
				runtime.translator.Text(i18n.PathHeader),
			)
			for _, skill := range result {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", skill.Client, skill.Scope, skill.Name, skill.Path)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	command.Flags().StringVar(&rawScope, "scope", "all", "discovery scope: project, global, or all")
	return command
}

func parseSkillDiscoveryScopes(raw string) ([]projection.Scope, error) {
	switch strings.TrimSpace(raw) {
	case "all":
		return []projection.Scope{projection.ScopeProject, projection.ScopeGlobal}, nil
	case string(projection.ScopeProject):
		return []projection.Scope{projection.ScopeProject}, nil
	case string(projection.ScopeGlobal):
		return []projection.Scope{projection.ScopeGlobal}, nil
	default:
		return nil, fmt.Errorf("unknown discovery scope %q: expected project, global, or all", raw)
	}
}

type pruneOutput struct {
	Applied  bool         `json:"applied"`
	Orphaned []prunedLink `json:"orphaned"`
	Pruned   []prunedLink `json:"pruned"`
}

// newSkillPruneCommand removes projections in the selected scope whose skill
// disappeared from its source — the residue an upstream `source update` leaves
// when a skill is dropped. It lists candidates by default and only removes them
// with --yes, mirroring `skills delete`. Detection reuses the same guard as the
// automatic post-update cleanup: an empty (unavailable) source is never treated
// as a wholesale removal.
func newSkillPruneCommand(options *rootOptions) *cobra.Command {
	var assumeYes bool
	var outputJSON bool
	var rawScope string
	command := &cobra.Command{
		Use:   "prune",
		Short: "Remove projections whose Skill left its source",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			scope, scopeErr := parseSkillScope(rawScope)
			if scopeErr != nil {
				return scopeErr
			}
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			orphans, err := runtime.projection.OrphanedProjectionsAt(activeSources(runtime.catalog), scope)
			if err != nil {
				return err
			}
			if !assumeYes {
				if outputJSON {
					return writeJSON(command, pruneOutput{Applied: false, Orphaned: toPrunedLinks(orphans), Pruned: []prunedLink{}})
				}
				if len(orphans) == 0 {
					fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.PruneNoOrphans))
					return nil
				}
				fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.PruneDryRunSummary, len(orphans)))
				return writeOrphanTable(command, runtime.translator, orphans)
			}
			pruned, pruneErr := runtime.projection.PruneOrphans(orphans)
			if outputJSON {
				if err := writeJSON(command, pruneOutput{Applied: true, Orphaned: toPrunedLinks(orphans), Pruned: toPrunedLinks(pruned)}); err != nil {
					return err
				}
				return pruneErr
			}
			if len(pruned) == 0 && pruneErr == nil {
				fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.PruneNoOrphans))
				return nil
			}
			if len(pruned) > 0 {
				fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.UpdatePrunedSummary, len(pruned)))
				if err := writeOrphanTable(command, runtime.translator, pruned); err != nil {
					return err
				}
			}
			return pruneErr
		},
	}
	command.Flags().BoolVar(&assumeYes, "yes", false, "remove the orphaned projections (default: list only)")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	command.Flags().StringVar(&rawScope, "scope", string(projection.ScopeProject), "projection scope: project or global")
	return command
}

func newSkillCreateCommand(options *rootOptions) *cobra.Command {
	var group, scope, description string
	var outputJSON bool
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Scaffold a new local Skill skeleton",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadSourceMutationRuntime(options)
			if err != nil {
				return err
			}
			if scope != "shared" {
				if err := runtime.catalog.Clients.Require(client.ID(scope), client.CapabilitySkills); err != nil {
					return err
				}
			}
			skillDir, err := catalog.ScaffoldLocalSkill(runtime.resources.SkillsRoot(), scope, group, args[0], description)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(command, map[string]string{"path": skillDir})
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.SkillCreated, skillDir))
			return nil
		},
	}
	command.Flags().StringVar(&group, "group", "", "group directory (default: a standalone group named after the Skill)")
	command.Flags().StringVar(&scope, "scope", "shared", "local scope (shared or a registered client id)")
	command.Flags().StringVar(&description, "description", "", "Skill description for the frontmatter")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

type localDeletion struct {
	path    string
	skills  []catalog.Skill
	isGroup bool
}

// resolveLocalDeletion maps a Skill or source id to a removable local target.
// Vendor sources are removed with `source remove`; vendor skills are read-only;
// archived references are never removable.
func resolveLocalDeletion(loaded catalog.Catalog, id string, translator i18n.Translator) (localDeletion, error) {
	if skill, ok := loaded.Skill(id); ok {
		source, _ := loaded.Source(skill.SourceID)
		if source.IsVendor() {
			return localDeletion{}, errors.New(translator.Text(i18n.DeleteReadOnlySkill))
		}
		if source.IsArchived() {
			return localDeletion{}, errors.New(translator.Text(i18n.DeleteArchivedUnsupported))
		}
		return localDeletion{path: skill.Path, skills: []catalog.Skill{skill}}, nil
	}
	if source, ok := loaded.Source(id); ok {
		if source.IsVendor() {
			return localDeletion{}, errors.New(translator.Text(i18n.DeleteVendorViaSourceRemove, id))
		}
		if source.IsArchived() {
			return localDeletion{}, errors.New(translator.Text(i18n.DeleteArchivedUnsupported))
		}
		return localDeletion{path: source.Path, skills: source.Skills, isGroup: true}, nil
	}
	return localDeletion{}, errors.New(translator.Text(i18n.DeleteUnknownTarget, id))
}

type skillDeleteOutput struct {
	ID    string `json:"id"`
	Group bool   `json:"group"`
	Path  string `json:"path"`
}

func newSkillDeleteCommand(options *rootOptions) *cobra.Command {
	var assumeYes bool
	var outputJSON bool
	var clients []string
	command := &cobra.Command{
		Use:     "delete <skill-or-group-id>",
		Aliases: []string{"remove", "rm", "del"},
		Short:   "Delete a local Skill or group directory from the resource SSOT",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			id := args[0]
			plan, err := resolveLocalDeletion(runtime.catalog, id, runtime.translator)
			if err != nil {
				return err
			}
			if !assumeYes {
				return errors.New(runtime.translator.Text(i18n.DeleteNeedsConfirmation, id))
			}
			if len(clients) > 0 {
				return errors.New("--client cannot limit cleanup when deleting a shared provider; omit it to retire every projection")
			}
			targetClients := runtime.catalog.Clients.IDsFor(client.CapabilitySkills)
			operations := make([]projection.Operation, 0, len(targetClients)*2)
			for _, clientID := range targetClients {
				operations = append(operations, projection.Operation{Skills: plan.skills, Client: clientID, Enabled: false, Scope: projection.ScopeProject})
				if runtime.projection.SupportsScope(clientID, projection.ScopeGlobal) {
					operations = append(operations, projection.Operation{Skills: plan.skills, Client: clientID, Enabled: false, Scope: projection.ScopeGlobal})
				}
			}
			if err := runtime.projection.Apply(operations); err != nil {
				return err
			}
			if err := catalog.RemoveLocalResource(runtime.catalog.Root, plan.path); err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(command, skillDeleteOutput{ID: id, Group: plan.isGroup, Path: plan.path})
			}
			resultKey := i18n.DeletedSkill
			if plan.isGroup {
				resultKey = i18n.DeletedSource
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(resultKey, id))
			return nil
		},
	}
	command.Flags().BoolVar(&assumeYes, "yes", false, "confirm deletion without an interactive prompt")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	command.Flags().StringSliceVar(&clients, "client", nil, "limit projection cleanup to these clients (default all)")
	return command
}
