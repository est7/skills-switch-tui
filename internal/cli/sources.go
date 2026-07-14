package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/source"
	"github.com/spf13/cobra"
)

func newSourceCommand(options *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "source",
		Short: "Manage catalog source repositories",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newSourceListCommand(options))
	command.AddCommand(newSourceAddCommand(options))
	command.AddCommand(newUpdateCommand(options))
	command.AddCommand(newSourceRemoveCommand(options))
	return command
}

func newSourceRemoveCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <source-id>",
		Aliases: []string{"delete", "del", "rm"},
		Short:   "Remove a clean vendor submodule and its catalog policy",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadCatalogRuntime(options)
			if err != nil {
				return err
			}
			selected, err := selectVendorSources(runtime.catalog, args, runtime.translator)
			if err != nil {
				return err
			}
			if err := runtime.manager.Remove(command.Context(), selected[0]); err != nil {
				return err
			}
			fmt.Fprint(command.OutOrStdout(), runtime.translator.Text(i18n.SourceRemoved, selected[0].ID))
			return nil
		},
	}
}

type sourceView struct {
	ID                string                      `json:"id"`
	Kind              string                      `json:"kind"`
	Scope             string                      `json:"scope"`
	Path              string                      `json:"path"`
	Skills            int                         `json:"skills"`
	Branch            string                      `json:"branch,omitempty"`
	SkillPaths        []string                    `json:"skillPaths,omitempty"`
	SparsePaths       []string                    `json:"sparsePaths,omitempty"`
	DiscoveryStrategy catalog.DiscoveryStrategy   `json:"discoveryStrategy,omitempty"`
	DiscoveryPriority []catalog.DiscoveryStrategy `json:"discoveryPriority,omitempty"`
}

func newSourceListCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	var includeArchive bool
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"query"},
		Short:   "List catalog sources",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadCatalogRuntime(options)
			if err != nil {
				return err
			}
			result := make([]sourceView, 0, len(runtime.catalog.Sources))
			for _, catalogSource := range runtime.catalog.Sources {
				if catalogSource.IsArchived() && !includeArchive {
					continue
				}
				kind := string(catalogSource.Kind)
				result = append(result, sourceView{
					ID:                catalogSource.ID,
					Kind:              kind,
					Scope:             catalogSource.Scope,
					Path:              catalogSource.Path,
					Skills:            len(catalogSource.Skills),
					Branch:            catalogSource.Branch,
					SkillPaths:        catalogSource.SkillPaths,
					SparsePaths:       catalogSource.SparsePaths,
					DiscoveryStrategy: catalogSource.DiscoveryStrategy,
					DiscoveryPriority: catalogSource.DiscoveryPriority,
				})
			}
			if outputJSON {
				return writeJSON(command, result)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
				runtime.translator.Text(i18n.SourceHeader),
				runtime.translator.Text(i18n.KindHeader),
				runtime.translator.Text(i18n.SkillsHeader),
				runtime.translator.Text(i18n.BranchHeader),
				runtime.translator.Text(i18n.DiscoveryHeader),
				runtime.translator.Text(i18n.PathHeader),
			)
			for _, item := range result {
				fmt.Fprintf(writer, "%s\t%s\t%d\t%s\t%s\t%s\n",
					item.ID,
					item.Kind,
					item.Skills,
					item.Branch,
					item.DiscoveryStrategy,
					item.Path,
				)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	command.Flags().BoolVar(&includeArchive, "archive", false, "include archived sources")
	return command
}

func newSourceAddCommand(options *rootOptions) *cobra.Command {
	var name string
	var branch string
	var clientScope string
	var skillPaths []string
	var sparsePaths []string
	var discoveryPriority []string
	command := &cobra.Command{
		Use:     "add <git-url>",
		Aliases: []string{"create"},
		Short:   "Add a vendor repository as a git submodule",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadSourceMutationRuntime(options)
			if err != nil {
				return err
			}
			scope := "shared"
			if clientScope != "" {
				if !runtime.catalog.Clients.Has(catalog.Client(clientScope)) {
					return errors.New(runtime.translator.Text(i18n.UnknownClient, clientScope))
				}
				scope = clientScope
			}
			cleanSparsePaths := make([]string, 0, len(sparsePaths))
			for _, path := range sparsePaths {
				if path = strings.TrimSpace(path); path != "" {
					cleanSparsePaths = append(cleanSparsePaths, path)
				}
			}
			cleanSkillPaths := make([]string, 0, len(skillPaths))
			for _, path := range skillPaths {
				if path = strings.TrimSpace(path); path != "" {
					cleanSkillPaths = append(cleanSkillPaths, path)
				}
			}
			cleanDiscoveryPriority := make([]catalog.DiscoveryStrategy, 0, len(discoveryPriority))
			for _, strategy := range discoveryPriority {
				if strategy = strings.TrimSpace(strategy); strategy != "" {
					cleanDiscoveryPriority = append(cleanDiscoveryPriority, catalog.DiscoveryStrategy(strategy))
				}
			}
			// An owner/repo shorthand or a GitHub/GitLab link fills in the clone
			// URL, name, branch, and Skill subpath so the user can register a
			// source from just a reference. Explicit flags win.
			repositoryURL := args[0]
			if ref, parseErr := source.ParseSourceRef(args[0]); parseErr == nil {
				repositoryURL = ref.CloneURL
				if name == "" {
					name = ref.Name
				}
				if !command.Flags().Changed("branch") && ref.Branch != "" {
					branch = ref.Branch
				}
				if len(cleanSkillPaths) == 0 && len(cleanDiscoveryPriority) == 0 && ref.Subpath != "" {
					cleanSkillPaths = []string{ref.Subpath}
				}
			}
			if name == "" {
				return errors.New(runtime.translator.Text(i18n.SourceNameRequired))
			}
			if err := runtime.manager.Add(command.Context(), source.AddRequest{
				Name:              name,
				URL:               repositoryURL,
				Branch:            branch,
				Scope:             scope,
				SkillPaths:        cleanSkillPaths,
				SparsePaths:       cleanSparsePaths,
				DiscoveryPriority: cleanDiscoveryPriority,
			}); err != nil {
				return err
			}
			sourceID := catalog.ScopedSourceID(catalog.SourceVendor, scope, name)
			fmt.Fprint(command.OutOrStdout(), runtime.translator.Text(i18n.SourceAdded, sourceID))
			return nil
		},
	}
	command.Flags().StringVar(&name, "name", "", "source name")
	command.Flags().StringVar(&branch, "branch", "main", "tracked branch")
	command.Flags().StringVar(&clientScope, "client", "", "restrict the entire source to one registered client")
	command.Flags().StringSliceVar(&skillPaths, "skill-path", nil, "authoritative Skill directory path (repeatable)")
	command.Flags().StringSliceVar(&sparsePaths, "sparse", nil, "additional sparse-checkout path (repeatable)")
	command.Flags().StringSliceVar(&discoveryPriority, "discovery-priority", nil, "source discovery strategy priority (repeatable)")
	return command
}

func newUpdateCommand(options *rootOptions) *cobra.Command {
	var dryRun bool
	var outputJSON bool
	command := &cobra.Command{
		Use:     "update [source-id]",
		Aliases: []string{"up"},
		Short:   "Update vendor sources from their tracked branches",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadCatalogRuntime(options)
			if err != nil {
				return err
			}
			selected, err := selectVendorSources(runtime.catalog, args, runtime.translator)
			if err != nil {
				return err
			}
			results, err := runtime.manager.Update(command.Context(), selected, dryRun)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(command, results)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
				runtime.translator.Text(i18n.SourceHeader),
				runtime.translator.Text(i18n.BranchHeader),
				runtime.translator.Text(i18n.CurrentHeader),
				runtime.translator.Text(i18n.RemoteHeader),
				runtime.translator.Text(i18n.ChangedHeader),
			)
			for _, result := range results {
				changed := runtime.translator.Text(i18n.ChangedNo)
				if result.Changed {
					changed = runtime.translator.Text(i18n.ChangedYes)
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					result.SourceID,
					result.Branch,
					shortRevision(result.Current),
					shortRevision(result.Remote),
					changed,
				)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "inspect updates without changing submodules")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

func selectVendorSources(loaded catalog.Catalog, args []string, translator i18n.Translator) ([]catalog.Source, error) {
	if len(args) == 1 {
		selected, ok := loaded.Source(args[0])
		if !ok {
			return nil, errors.New(translator.Text(i18n.UnknownSource, args[0]))
		}
		if selected.IsArchived() || !selected.IsVendor() {
			return nil, errors.New(translator.Text(i18n.SourceNotVendor, selected.ID))
		}
		return []catalog.Source{selected}, nil
	}
	selected := make([]catalog.Source, 0)
	for _, candidate := range loaded.Sources {
		if candidate.IsVendor() {
			selected = append(selected, candidate)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New(translator.Text(i18n.NoVendorSources))
	}
	return selected, nil
}

func shortRevision(revision string) string {
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
}
