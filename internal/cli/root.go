package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/project"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/resource"
	"github.com/est7/skills-switch-tui/internal/source"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
	"github.com/est7/skills-switch-tui/internal/userresource"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	resourcesRoot string
	projectRoot   string
	language      string
}

type runtime struct {
	catalog        catalog.Catalog
	projectRoot    string
	userHome       string
	projection     projection.Manager
	translator     i18n.Translator
	resources      resource.Layout
	manager        source.Manager
	mcpCatalog     mcp.Catalog
	mcpManager     mcp.Manager
	prompts        systemprompt.Catalog
	promptMgr      systemprompt.Manager
	commands       userresource.Catalog
	commandMgr     userresource.Manager
	hooks          userresource.Catalog
	hookMgr        userresource.Manager
	agents         userresource.Catalog
	agentMgr       userresource.Manager
	outputStyles   userresource.Catalog
	outputStyleMgr userresource.Manager
}

type promptRuntime struct {
	translator i18n.Translator
	prompts    systemprompt.Catalog
	promptMgr  systemprompt.Manager
}

type userResourceRuntime struct {
	catalog    catalog.Catalog
	translator i18n.Translator
	resources  userresource.Catalog
	manager    userresource.Manager
}

type catalogRuntime struct {
	catalog    catalog.Catalog
	translator i18n.Translator
	resources  resource.Layout
	manager    source.Manager
}

func NewRootCommand(version string) *cobra.Command {
	defaultLanguage := os.Getenv("SKILLS_SWITCH_LANG")
	if defaultLanguage == "" {
		defaultLanguage = "auto"
	}
	options := &rootOptions{language: defaultLanguage}
	translator, _ := i18n.FromEnvironment(defaultLanguage)
	command := &cobra.Command{
		Use:           "skills-switch",
		Short:         translator.Text(i18n.RootShort),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return runTUI(options)
		},
	}
	command.CompletionOptions.DisableDefaultCmd = true
	command.PersistentFlags().StringVar(&options.resourcesRoot, "resources", "", translator.Text(i18n.ResourcesFlag))
	command.PersistentFlags().StringVar(&options.projectRoot, "project", "", translator.Text(i18n.ProjectFlag))
	command.PersistentFlags().StringVar(&options.language, "lang", defaultLanguage, translator.Text(i18n.LanguageFlag))
	command.AddCommand(newSkillsCommand(options))
	command.AddCommand(newStatusCommand(options))
	command.AddCommand(newInitCommand(options))
	command.AddCommand(newSourceCommand(options))
	command.AddCommand(newMCPCommand(options))
	command.AddCommand(newPromptCommand(options))
	command.AddCommand(newUserResourceCommand(options, userresource.KindCommand))
	command.AddCommand(newUserResourceCommand(options, userresource.KindHook))
	command.AddCommand(newUserResourceCommand(options, userresource.KindAgent))
	command.AddCommand(newUserResourceCommand(options, userresource.KindOutputStyle))
	command.AddCommand(newDoctorCommand(options))
	command.AddCommand(newTUICommand(options))
	command.AddCommand(newVersionCommand(version))
	localizeCommandTree(command, translator)
	defaultHelp := command.HelpFunc()
	command.SetHelpFunc(func(target *cobra.Command, arguments []string) {
		selected, err := i18n.FromEnvironment(options.language)
		if err != nil {
			target.PrintErrln(err)
			selected = i18n.New(i18n.English)
		}
		localizeCommandTree(target.Root(), selected)
		if flag := target.Flags().Lookup("help"); flag != nil {
			flag.Usage = selected.Text(i18n.HelpFlag)
		}
		defaultHelp(target, arguments)
	})
	return command
}

func newListCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	var includeArchive bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List catalog skills and their current project state",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			result, err := buildListOutput(runtime, includeArchive)
			if err != nil {
				return err
			}
			if outputJSON {
				encoder := json.NewEncoder(command.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s", runtime.translator.Text(i18n.SkillHeader), runtime.translator.Text(i18n.SourceHeader))
			for _, client := range runtime.catalog.Clients.IDs() {
				fmt.Fprintf(writer, "\t%s", strings.ToUpper(string(client)))
			}
			fmt.Fprintln(writer)
			for _, skill := range result.Skills {
				fmt.Fprintf(writer, "%s\t%s", skill.ID, skill.Source)
				for _, client := range runtime.catalog.Clients.IDs() {
					fmt.Fprintf(writer, "\t%s", skill.Clients[string(client)])
				}
				fmt.Fprintln(writer)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	command.Flags().BoolVar(&includeArchive, "archive", false, "include archived sources")
	return command
}

func newEnableCommand(options *rootOptions, enabled bool) *cobra.Command {
	verb := "enable"
	short := "Enable a skill or source for the current project"
	if !enabled {
		verb = "disable"
		short = "Disable a skill or source for the current project"
	}
	var clients []string
	var sourceID string
	command := &cobra.Command{
		Use:   verb + " [skill-id]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if (len(args) == 0) == (sourceID == "") {
				translator, translatorErr := i18n.FromEnvironment(options.language)
				if translatorErr != nil {
					return translatorErr
				}
				return errors.New(translator.Text(i18n.SelectExactlyOne))
			}
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			parsedClients, err := parseClients(clients, runtime.catalog, runtime.translator)
			if err != nil {
				return err
			}
			skills, err := selectedSkills(runtime.catalog, args, sourceID, parsedClients, enabled, runtime.translator)
			if err != nil {
				return err
			}
			operations := make([]projection.Operation, 0, len(parsedClients))
			for _, client := range parsedClients {
				clientSkills := skills
				if sourceID != "" && enabled {
					clientSkills = compatibleSkills(skills, client)
					if len(clientSkills) == 0 {
						return errors.New(runtime.translator.Text(i18n.SourceNoCompatibleSkills, sourceID, client))
					}
				}
				operations = append(operations, projection.Operation{Skills: clientSkills, Client: client, Enabled: enabled})
			}
			if err := runtime.projection.Apply(operations); err != nil {
				return err
			}
			resultKey := i18n.EnabledResult
			if !enabled {
				resultKey = i18n.DisabledResult
			}
			clientNames := make([]string, 0, len(parsedClients))
			for _, client := range parsedClients {
				clientNames = append(clientNames, string(client))
			}
			fmt.Fprint(command.OutOrStdout(), runtime.translator.Text(resultKey, len(skills), strings.Join(clientNames, ",")))
			return nil
		},
	}
	command.Flags().StringSliceVar(&clients, "client", nil, "registered target client (repeatable)")
	command.Flags().StringVar(&sourceID, "source", "", "operate on every compatible skill in a source")
	return command
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			fmt.Fprintln(command.OutOrStdout(), version)
		},
	}
}

type listOutput struct {
	Project string      `json:"project"`
	Skills  []skillView `json:"skills"`
}

type skillView struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Source      string            `json:"source"`
	Description string            `json:"description,omitempty"`
	Clients     map[string]string `json:"clients"`
}

func buildListOutput(runtime runtime, includeArchive bool) (listOutput, error) {
	result := listOutput{Project: runtime.projectRoot}
	for _, source := range runtime.catalog.Sources {
		if source.IsArchived() && !includeArchive {
			continue
		}
		for _, skill := range source.Skills {
			view := skillView{
				ID:          skill.ID,
				Name:        skill.Name,
				Source:      skill.SourceID,
				Description: skill.Description,
				Clients:     make(map[string]string, len(runtime.catalog.Clients.IDs())),
			}
			for _, client := range runtime.catalog.Clients.IDs() {
				state, err := runtime.projection.State(skill, client)
				if err != nil {
					return listOutput{}, err
				}
				view.Clients[string(client)] = string(state)
			}
			result.Skills = append(result.Skills, view)
		}
	}
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].ID < result.Skills[j].ID })
	return result, nil
}

func loadRuntime(options *rootOptions) (runtime, error) {
	base, err := loadCatalogRuntime(options)
	if err != nil {
		return runtime{}, err
	}
	start := options.projectRoot
	if start == "" {
		start, err = os.Getwd()
		if err != nil {
			return runtime{}, fmt.Errorf("get current directory: %w", err)
		}
	}
	projectRoot, err := project.FindRoot(start)
	if err != nil {
		return runtime{}, err
	}
	userHome, err := resolveUserHome()
	if err != nil {
		return runtime{}, err
	}
	mcpCatalogPath := base.resources.MCPCatalogFile()
	mcpCatalog := mcp.Catalog{Path: mcpCatalogPath, Servers: make(map[string]mcp.Server)}
	if _, statErr := os.Stat(mcpCatalogPath); statErr == nil {
		mcpCatalog, err = mcp.LoadCatalog(mcpCatalogPath)
		if err != nil {
			return runtime{}, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return runtime{}, fmt.Errorf("stat MCP catalog: %w", statErr)
	}
	prompts, err := systemprompt.Discover(base.resources.SystemPromptsRoot(), base.catalog.Clients)
	if err != nil {
		return runtime{}, err
	}
	commands, err := userresource.Discover(base.resources.CommandsRoot(), userresource.KindCommand, base.catalog.Clients)
	if err != nil {
		return runtime{}, err
	}
	hooks, err := userresource.Discover(base.resources.HooksRoot(), userresource.KindHook, base.catalog.Clients)
	if err != nil {
		return runtime{}, err
	}
	agents, err := userresource.Discover(base.resources.AgentsRoot(), userresource.KindAgent, base.catalog.Clients)
	if err != nil {
		return runtime{}, err
	}
	outputStyles, err := userresource.Discover(base.resources.OutputStylesRoot(), userresource.KindOutputStyle, base.catalog.Clients)
	if err != nil {
		return runtime{}, err
	}
	return runtime{
		catalog:        base.catalog,
		projectRoot:    projectRoot,
		userHome:       userHome,
		projection:     projection.New(projectRoot, base.catalog),
		translator:     base.translator,
		resources:      base.resources,
		manager:        base.manager,
		mcpCatalog:     mcpCatalog,
		mcpManager:     mcp.NewManager(projectRoot, mcpCatalog, base.catalog.Clients),
		prompts:        prompts,
		promptMgr:      systemprompt.NewManager(userHome, base.catalog.Clients),
		commands:       commands,
		commandMgr:     userresource.NewManager(userHome, base.catalog.Clients),
		hooks:          hooks,
		hookMgr:        userresource.NewManager(userHome, base.catalog.Clients),
		agents:         agents,
		agentMgr:       userresource.NewManager(userHome, base.catalog.Clients),
		outputStyles:   outputStyles,
		outputStyleMgr: userresource.NewManager(userHome, base.catalog.Clients),
	}, nil
}

func loadUserResourceRuntime(options *rootOptions, kind userresource.Kind) (userResourceRuntime, error) {
	base, err := loadCatalogRuntime(options)
	if err != nil {
		return userResourceRuntime{}, err
	}
	userHome, err := resolveUserHome()
	if err != nil {
		return userResourceRuntime{}, err
	}
	root := base.resources.CommandsRoot()
	if kind == userresource.KindHook {
		root = base.resources.HooksRoot()
	} else if kind == userresource.KindAgent {
		root = base.resources.AgentsRoot()
	} else if kind == userresource.KindOutputStyle {
		root = base.resources.OutputStylesRoot()
	}
	resources, err := userresource.Discover(root, kind, base.catalog.Clients)
	if err != nil {
		return userResourceRuntime{}, err
	}
	return userResourceRuntime{
		catalog:    base.catalog,
		translator: base.translator,
		resources:  resources,
		manager:    userresource.NewManager(userHome, base.catalog.Clients),
	}, nil
}

func loadPromptRuntime(options *rootOptions) (promptRuntime, error) {
	base, err := loadCatalogRuntime(options)
	if err != nil {
		return promptRuntime{}, err
	}
	userHome, err := resolveUserHome()
	if err != nil {
		return promptRuntime{}, err
	}
	prompts, err := systemprompt.Discover(base.resources.SystemPromptsRoot(), base.catalog.Clients)
	if err != nil {
		return promptRuntime{}, err
	}
	return promptRuntime{
		translator: base.translator,
		prompts:    prompts,
		promptMgr:  systemprompt.NewManager(userHome, base.catalog.Clients),
	}, nil
}

func loadCatalogRuntime(options *rootOptions) (catalogRuntime, error) {
	resourcesRoot, err := resolveResourcesRoot(options.resourcesRoot)
	if err != nil {
		return catalogRuntime{}, err
	}
	resources, err := resource.NewLayout(resourcesRoot)
	if err != nil {
		return catalogRuntime{}, err
	}
	clients, err := client.LoadRegistry(resources.RegistryFile())
	if err != nil {
		return catalogRuntime{}, err
	}
	loadedCatalog, err := catalog.Load(resources.SkillsRoot(), clients)
	if err != nil {
		return catalogRuntime{}, err
	}
	translator, err := i18n.FromEnvironment(options.language)
	if err != nil {
		return catalogRuntime{}, err
	}
	return catalogRuntime{
		catalog:    loadedCatalog,
		translator: translator,
		resources:  resources,
		manager: source.Manager{
			SkillsRoot: resources.SkillsRoot(),
			Clients:    loadedCatalog.Clients,
		},
	}, nil
}

func loadSourceMutationRuntime(options *rootOptions) (catalogRuntime, error) {
	resourcesRoot, err := resolveResourcesRoot(options.resourcesRoot)
	if err != nil {
		return catalogRuntime{}, err
	}
	resources, err := resource.NewLayout(resourcesRoot)
	if err != nil {
		return catalogRuntime{}, err
	}
	if err := os.MkdirAll(resources.SkillsRoot(), 0o755); err != nil {
		return catalogRuntime{}, fmt.Errorf("create skills root: %w", err)
	}
	return loadCatalogRuntime(options)
}

// activeSources returns the catalog's non-archived sources: the set whose
// projections the tool manages and can reconcile. Archived sources are excluded
// because a link into an intentionally archived collection is preserved, not
// orphaned.
func activeSources(loaded catalog.Catalog) []catalog.Source {
	sources := make([]catalog.Source, 0, len(loaded.Sources))
	for _, candidate := range loaded.Sources {
		if candidate.IsArchived() {
			continue
		}
		sources = append(sources, candidate)
	}
	return sources
}

// autoPruneAfterUpdate removes projections in the current project whose skill
// disappeared from one of the just-updated sources. It reloads the catalog so
// detection sees the post-update skill set, scopes cleanup to the sources that
// actually changed, and only ever removes managed symlinks that lost their
// provider (see projection.OrphanedProjections for the empty-source guard).
//
// It is a best-effort side effect of `source update`: when the command is not
// run inside a project, or the project root cannot be resolved, it cleans
// nothing and returns no error, because updating a source mutates the resource
// catalog, not any particular consuming project. Cleanup failures are returned
// as a non-fatal warning so the update itself still reports success.
func autoPruneAfterUpdate(options *rootOptions, results []source.UpdateResult) ([]projection.Orphan, error) {
	changed := make(map[string]bool)
	for _, result := range results {
		if result.Changed {
			changed[result.SourceID] = true
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	start := options.projectRoot
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil
		}
		start = cwd
	}
	projectRoot, err := project.FindRoot(start)
	if err != nil {
		return nil, nil
	}
	base, err := loadCatalogRuntime(options)
	if err != nil {
		return nil, fmt.Errorf("reload catalog for cleanup: %w", err)
	}
	scoped := make([]catalog.Source, 0, len(changed))
	for _, candidate := range base.catalog.Sources {
		if changed[candidate.ID] {
			scoped = append(scoped, candidate)
		}
	}
	manager := projection.New(projectRoot, base.catalog)
	orphans, err := manager.OrphanedProjections(scoped)
	if err != nil {
		return nil, err
	}
	return manager.PruneOrphans(orphans)
}

func resolveResourcesRoot(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}
	if fromEnvironment := os.Getenv("SKILLS_SWITCH_RESOURCES"); fromEnvironment != "" {
		return filepath.Abs(fromEnvironment)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".agents", "resources"), nil
}

func resolveUserHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Abs(home)
}

func parseClients(values []string, loaded catalog.Catalog, translator i18n.Translator) ([]catalog.Client, error) {
	if len(values) == 0 {
		return nil, errors.New(translator.Text(i18n.AtLeastOneClient))
	}
	seen := make(map[catalog.Client]bool)
	clients := make([]catalog.Client, 0, len(values))
	for _, value := range values {
		client := catalog.Client(value)
		if !loaded.Clients.Has(client) {
			return nil, errors.New(translator.Text(i18n.UnknownClient, value))
		}
		if !seen[client] {
			seen[client] = true
			clients = append(clients, client)
		}
	}
	return clients, nil
}

func localizeCommandTree(root *cobra.Command, translator i18n.Translator) {
	root.Short = translator.Text(i18n.RootShort)
	root.SetUsageTemplate(localizedUsageTemplate(translator))
	if flag := root.PersistentFlags().Lookup("resources"); flag != nil {
		flag.Usage = translator.Text(i18n.ResourcesFlag)
	}
	if flag := root.PersistentFlags().Lookup("project"); flag != nil {
		flag.Usage = translator.Text(i18n.ProjectFlag)
	}
	if flag := root.PersistentFlags().Lookup("lang"); flag != nil {
		flag.Usage = translator.Text(i18n.LanguageFlag)
	}
	for _, command := range root.Commands() {
		switch command.Name() {
		case "version":
			command.Short = translator.Text(i18n.VersionShort)
		case "status":
			command.Short = translator.Text(i18n.StatusShort)
			localizeOutputFlags(command, translator)
		case "init":
			command.Short = translator.Text(i18n.InitShort)
			localizeOutputFlags(command, translator)
		case "doctor":
			command.Short = translator.Text(i18n.DoctorShort)
			localizeOutputFlags(command, translator)
		case "tui":
			command.Short = translator.Text(i18n.TUIShort)
		case "source":
			command.Short = translator.Text(i18n.SourceCommandShort)
			localizeSourceCommands(command, translator)
		case "skills":
			command.Short = translator.Text(i18n.SkillsCommandShort)
			localizeSkillsCommands(command, translator)
		case "mcp":
			command.Short = translator.Text(i18n.MCPCommandShort)
			localizeResourceCommands(command, translator)
		case "prompt":
			command.Short = translator.Text(i18n.PromptCommandShort)
			localizeResourceCommands(command, translator)
		case "commands":
			command.Short = translator.Text(i18n.CommandsCommandShort)
			localizeResourceCommands(command, translator)
		case "hooks":
			command.Short = translator.Text(i18n.HooksCommandShort)
			localizeResourceCommands(command, translator)
		case "agents":
			command.Short = translator.Text(i18n.AgentsCommandShort)
			localizeResourceCommands(command, translator)
		case "output-styles":
			command.Short = translator.Text(i18n.OutputStylesCommandShort)
			localizeResourceCommands(command, translator)
		case "help":
			command.Short = translator.Text(i18n.HelpCommandShort)
		}
	}
}

func localizeSkillsCommands(command *cobra.Command, translator i18n.Translator) {
	for _, child := range command.Commands() {
		switch child.Name() {
		case "list":
			child.Short = translator.Text(i18n.ListShort)
			localizeOutputFlags(child, translator)
		case "show":
			child.Short = translator.Text(i18n.ShowShort)
			localizeOutputFlags(child, translator)
		case "enable":
			child.Short = translator.Text(i18n.EnableShort)
			localizeToggleFlags(child, translator)
		case "disable":
			child.Short = translator.Text(i18n.DisableShort)
			localizeToggleFlags(child, translator)
		case "create":
			child.Short = translator.Text(i18n.SkillsCreateShort)
			localizeOutputFlags(child, translator)
		case "delete":
			child.Short = translator.Text(i18n.SkillsDeleteShort)
			if flag := child.Flags().Lookup("client"); flag != nil {
				flag.Usage = translator.Text(i18n.ClientFlag)
			}
		}
	}
}

func localizeResourceCommands(command *cobra.Command, translator i18n.Translator) {
	for _, child := range command.Commands() {
		switch command.Name() + "/" + child.Name() {
		case "mcp/list":
			child.Short = translator.Text(i18n.MCPListShort)
		case "mcp/enable":
			child.Short = translator.Text(i18n.MCPEnableShort)
		case "mcp/disable":
			child.Short = translator.Text(i18n.MCPDisableShort)
		case "mcp/add":
			child.Short = translator.Text(i18n.MCPAddShort)
		case "mcp/import":
			child.Short = translator.Text(i18n.MCPImportShort)
			if flag := child.Flags().Lookup("file"); flag != nil {
				flag.Usage = translator.Text(i18n.MCPImportFileFlag)
			}
			if flag := child.Flags().Lookup("name"); flag != nil {
				flag.Usage = translator.Text(i18n.MCPImportNameFlag)
			}
		case "mcp/remove":
			child.Short = translator.Text(i18n.MCPRemoveShort)
		case "prompt/list":
			child.Short = translator.Text(i18n.PromptListShort)
		case "prompt/enable":
			child.Short = translator.Text(i18n.PromptEnableShort)
		case "prompt/disable":
			child.Short = translator.Text(i18n.PromptDisableShort)
		case "prompt/build":
			child.Short = translator.Text(i18n.PromptBuildShort)
		case "commands/list", "hooks/list":
			child.Short = translator.Text(i18n.UserResourceListShort)
		case "commands/enable", "hooks/enable":
			child.Short = translator.Text(i18n.UserResourceEnableShort)
		case "commands/disable", "hooks/disable":
			child.Short = translator.Text(i18n.UserResourceDisableShort)
		}
		localizeOutputFlags(child, translator)
		if flag := child.Flags().Lookup("client"); flag != nil {
			flag.Usage = translator.Text(i18n.ClientFlag)
		}
	}
}

func localizedUsageTemplate(translator i18n.Translator) string {
	return translator.Text(i18n.UsageHeading) + `:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

` + translator.Text(i18n.AliasesHeading) + `:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

` + translator.Text(i18n.ExamplesHeading) + `:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

` + translator.Text(i18n.AvailableCommandsHeading) + `:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

` + translator.Text(i18n.AdditionalCommandsHeading) + `:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + translator.Text(i18n.FlagsHeading) + `:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

` + translator.Text(i18n.GlobalFlagsHeading) + `:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

` + translator.Text(i18n.AdditionalHelpHeading) + `:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

` + translator.Text(i18n.MoreInformationHint, "{{.CommandPath}}") + `{{end}}
`
}

func localizeOutputFlags(command *cobra.Command, translator i18n.Translator) {
	if flag := command.Flags().Lookup("json"); flag != nil {
		flag.Usage = translator.Text(i18n.EmitJSONFlag)
	}
	if flag := command.Flags().Lookup("archive"); flag != nil {
		flag.Usage = translator.Text(i18n.IncludeArchiveFlag)
	}
}

func localizeSourceCommands(command *cobra.Command, translator i18n.Translator) {
	for _, child := range command.Commands() {
		switch child.Name() {
		case "list":
			child.Short = translator.Text(i18n.SourceListShort)
			localizeOutputFlags(child, translator)
		case "add":
			child.Short = translator.Text(i18n.SourceAddShort)
			if flag := child.Flags().Lookup("name"); flag != nil {
				flag.Usage = translator.Text(i18n.NameFlag)
			}
			if flag := child.Flags().Lookup("branch"); flag != nil {
				flag.Usage = translator.Text(i18n.BranchFlag)
			}
			if flag := child.Flags().Lookup("client"); flag != nil {
				flag.Usage = translator.Text(i18n.SourceScopeFlag)
			}
			if flag := child.Flags().Lookup("skill-path"); flag != nil {
				flag.Usage = translator.Text(i18n.SkillPathFlag)
			}
			if flag := child.Flags().Lookup("sparse"); flag != nil {
				flag.Usage = translator.Text(i18n.SparseFlag)
			}
			if flag := child.Flags().Lookup("discovery-priority"); flag != nil {
				flag.Usage = translator.Text(i18n.DiscoveryPriorityFlag)
			}
		case "update":
			child.Short = translator.Text(i18n.UpdateShort)
			localizeOutputFlags(child, translator)
			if flag := child.Flags().Lookup("dry-run"); flag != nil {
				flag.Usage = translator.Text(i18n.DryRunFlag)
			}
		case "remove":
			child.Short = translator.Text(i18n.SourceRemoveShort)
		}
	}
}

func localizeToggleFlags(command *cobra.Command, translator i18n.Translator) {
	if flag := command.Flags().Lookup("client"); flag != nil {
		flag.Usage = translator.Text(i18n.ClientFlag)
	}
	if flag := command.Flags().Lookup("source"); flag != nil {
		flag.Usage = translator.Text(i18n.SourceFlag)
	}
}

func selectedSkills(loaded catalog.Catalog, args []string, sourceID string, clients []catalog.Client, enabled bool, translator i18n.Translator) ([]catalog.Skill, error) {
	if sourceID != "" {
		source, ok := loaded.Source(sourceID)
		if !ok {
			return nil, errors.New(translator.Text(i18n.UnknownSource, sourceID))
		}
		if enabled && source.IsArchived() {
			return nil, errors.New(translator.Text(i18n.ArchivedCannotCLIEnable, source.ID))
		}
		return source.Skills, nil
	}
	skill, ok := loaded.Skill(args[0])
	if !ok {
		return nil, errors.New(translator.Text(i18n.UnknownSkill, args[0]))
	}
	if source, ok := loaded.Source(skill.SourceID); enabled && ok && source.IsArchived() {
		return nil, errors.New(translator.Text(i18n.ArchivedCannotCLIEnable, source.ID))
	}
	if enabled {
		for _, client := range clients {
			if !skill.Supports(client) {
				reason := skill.CompatibilityReason
				if reason == "" {
					reason = translator.Text(i18n.CatalogCompatibility)
				}
				return nil, errors.New(translator.Text(i18n.UnavailableForClient, skill.ID, client, reason))
			}
		}
	}
	return []catalog.Skill{skill}, nil
}

func compatibleSkills(skills []catalog.Skill, client catalog.Client) []catalog.Skill {
	compatible := make([]catalog.Skill, 0, len(skills))
	for _, skill := range skills {
		if skill.Supports(client) {
			compatible = append(compatible, skill)
		}
	}
	return compatible
}
