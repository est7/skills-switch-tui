package cli

import (
	"errors"
	"fmt"

	"github.com/est7/skills-switch-tui/internal/catalog"
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
	command.AddCommand(newShowCommand(options))
	command.AddCommand(newEnableCommand(options, true))
	command.AddCommand(newEnableCommand(options, false))
	command.AddCommand(newSkillCreateCommand(options))
	command.AddCommand(newSkillDeleteCommand(options))
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

func newSkillDeleteCommand(options *rootOptions) *cobra.Command {
	var assumeYes bool
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
			targetClients := runtime.catalog.Clients.IDs()
			if len(clients) > 0 {
				targetClients, err = parseClients(clients, runtime.catalog, runtime.translator)
				if err != nil {
					return err
				}
			}
			operations := make([]projection.Operation, 0, len(targetClients))
			for _, clientID := range targetClients {
				operations = append(operations, projection.Operation{Skills: plan.skills, Client: clientID, Enabled: false})
			}
			if err := runtime.projection.Apply(operations); err != nil {
				return err
			}
			if err := catalog.RemoveLocalResource(runtime.catalog.Root, plan.path); err != nil {
				return err
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
	command.Flags().StringSliceVar(&clients, "client", nil, "limit projection cleanup to these clients (default all)")
	return command
}
