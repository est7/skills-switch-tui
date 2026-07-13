package cli

import (
	"encoding/json"
	"fmt"

	"github.com/est7/skills-switch-tui/internal/bootstrap"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/spf13/cobra"
)

func newInitCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize agent resources and the bundled operator Skill",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resourcesRoot, err := resolveResourcesRoot(options.resourcesRoot)
			if err != nil {
				return err
			}
			result, err := (bootstrap.Manager{ResourcesRoot: resourcesRoot}).Initialize(command.Context())
			if err != nil {
				return err
			}
			if outputJSON {
				encoder := json.NewEncoder(command.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			translator, err := i18n.FromEnvironment(options.language)
			if err != nil {
				return err
			}
			key := i18n.ResourcesAlreadyReady
			if result.SourceAdded {
				key = i18n.InitializedResources
			}
			fmt.Fprint(command.OutOrStdout(), translator.Text(key, result.ResourcesRoot, result.SkillID))
			return nil
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}
