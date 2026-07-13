package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
	"github.com/spf13/cobra"
)

func newMCPCommand(options *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "mcp",
		Short: "Manage project-level MCP servers",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newMCPListCommand(options))
	command.AddCommand(newMCPToggleCommand(options, true))
	command.AddCommand(newMCPToggleCommand(options, false))
	command.AddCommand(newMCPAddCommand(options))
	command.AddCommand(newMCPRemoveCommand(options))
	return command
}

func newMCPAddCommand(options *rootOptions) *cobra.Command {
	var commandLine, url, cwd, transport string
	var commandArgs, env, headers []string
	command := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a new MCP server in the catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			envMap, err := parseKeyValues(env)
			if err != nil {
				return err
			}
			headerMap, err := parseKeyValues(headers)
			if err != nil {
				return err
			}
			server := mcp.Server{
				Name:      args[0],
				Transport: mcp.Transport(transport),
				Command:   commandLine,
				Args:      commandArgs,
				Env:       envMap,
				CWD:       cwd,
				URL:       url,
				Headers:   headerMap,
			}
			if server.Transport == "" {
				switch {
				case commandLine != "" && url == "":
					server.Transport = mcp.TransportStdio
				case url != "" && commandLine == "":
					server.Transport = mcp.TransportHTTP
				default:
					return errors.New(runtime.translator.Text(i18n.MCPTransportAmbiguous))
				}
			}
			if err := mcp.AddServer(runtime.mcpCatalog.Path, server); err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.MCPServerAdded, args[0]))
			return nil
		},
	}
	command.Flags().StringVar(&commandLine, "command", "", "stdio command executable")
	command.Flags().StringSliceVar(&commandArgs, "arg", nil, "stdio command argument (repeatable)")
	command.Flags().StringSliceVar(&env, "env", nil, "stdio environment KEY=VALUE (repeatable)")
	command.Flags().StringVar(&cwd, "cwd", "", "stdio working directory")
	command.Flags().StringVar(&url, "url", "", "http(s) endpoint URL")
	command.Flags().StringSliceVar(&headers, "header", nil, "http header KEY=VALUE (repeatable)")
	command.Flags().StringVar(&transport, "transport", "", "stdio or http (inferred when omitted)")
	return command
}

func newMCPRemoveCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"delete", "del", "rm"},
		Short:   "Remove an MCP server from the catalog",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := runtime.mcpCatalog.Server(name); !ok {
				return errors.New(runtime.translator.Text(i18n.UnknownMCPServer, name))
			}
			operations := make([]mcp.Operation, 0)
			for _, clientID := range runtime.catalog.Clients.IDs() {
				state, err := runtime.mcpManager.State(name, clientID)
				if err != nil {
					return err
				}
				if state == mcp.StateEnabled {
					operations = append(operations, mcp.Operation{Server: name, Client: clientID, Enabled: false})
				}
			}
			if len(operations) > 0 {
				if err := runtime.mcpManager.Apply(operations); err != nil {
					return err
				}
			}
			if err := mcp.RemoveServer(runtime.mcpCatalog.Path, name); err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(i18n.DeletedMCPServer, name))
			return nil
		},
	}
}

func parseKeyValues(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, val, found := strings.Cut(value, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("invalid KEY=VALUE %q", value)
		}
		result[key] = val
	}
	return result, nil
}

type mcpView struct {
	Name    string            `json:"name"`
	Clients map[string]string `json:"clients"`
}

func newMCPListCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"query"},
		Short:   "List MCP servers and project state",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			result := make([]mcpView, 0, len(runtime.mcpCatalog.Servers))
			for _, name := range runtime.mcpCatalog.Names() {
				item := mcpView{Name: name, Clients: make(map[string]string)}
				for _, clientID := range runtime.catalog.Clients.IDs() {
					state, err := runtime.mcpManager.State(name, clientID)
					if err != nil {
						return err
					}
					item.Clients[string(clientID)] = string(state)
				}
				result = append(result, item)
			}
			if outputJSON {
				return writeJSON(command, result)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprint(writer, "MCP")
			for _, clientID := range runtime.catalog.Clients.IDs() {
				fmt.Fprintf(writer, "\t%s", clientID)
			}
			fmt.Fprintln(writer)
			for _, item := range result {
				fmt.Fprint(writer, item.Name)
				for _, clientID := range runtime.catalog.Clients.IDs() {
					fmt.Fprintf(writer, "\t%s", item.Clients[string(clientID)])
				}
				fmt.Fprintln(writer)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

func newMCPToggleCommand(options *rootOptions, enabled bool) *cobra.Command {
	verb := "enable"
	short := "Enable an MCP server for project clients"
	if !enabled {
		verb = "disable"
		short = "Disable an MCP server for project clients"
	}
	var clients []string
	command := &cobra.Command{
		Use:   verb + " <server>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadRuntime(options)
			if err != nil {
				return err
			}
			if _, ok := runtime.mcpCatalog.Server(args[0]); !ok {
				return errors.New(runtime.translator.Text(i18n.UnknownMCPServer, args[0]))
			}
			parsed, err := parseClients(clients, runtime.catalog, runtime.translator)
			if err != nil {
				return err
			}
			operations := make([]mcp.Operation, 0, len(parsed))
			for _, clientID := range parsed {
				operations = append(operations, mcp.Operation{Server: args[0], Client: clientID, Enabled: enabled})
			}
			if err := runtime.mcpManager.Apply(operations); err != nil {
				return err
			}
			key := i18n.EnabledResource
			if !enabled {
				key = i18n.DisabledResource
			}
			clientNames := make([]string, len(parsed))
			for index, clientID := range parsed {
				clientNames[index] = string(clientID)
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(key, args[0], strings.Join(clientNames, ",")))
			return nil
		},
	}
	command.Flags().StringSliceVar(&clients, "client", nil, "registered target client (repeatable)")
	return command
}

func newPromptCommand(options *rootOptions) *cobra.Command {
	command := &cobra.Command{
		Use:     "prompt",
		Aliases: []string{"system-prompt"},
		Short:   "Manage user-global system prompt files",
		Args:    cobra.NoArgs,
	}
	command.AddCommand(newPromptListCommand(options))
	command.AddCommand(newPromptToggleCommand(options, true))
	command.AddCommand(newPromptToggleCommand(options, false))
	return command
}

type promptView struct {
	ID     string `json:"id"`
	Client string `json:"client"`
	Files  int    `json:"files"`
	State  string `json:"state"`
}

func newPromptListCommand(options *rootOptions) *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"query"},
		Short:   "List system prompt groups and user-global state",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadPromptRuntime(options)
			if err != nil {
				return err
			}
			result := make([]promptView, 0, len(runtime.prompts.Groups))
			for _, group := range runtime.prompts.Groups {
				state, err := runtime.promptMgr.State(group)
				if err != nil {
					return err
				}
				result = append(result, promptView{ID: group.ID, Client: string(group.Client), Files: len(group.Files), State: string(state)})
			}
			if outputJSON {
				return writeJSON(command, result)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
				runtime.translator.Text(i18n.PromptHeader),
				runtime.translator.Text(i18n.ClientHeader),
				runtime.translator.Text(i18n.FilesHeader),
				runtime.translator.Text(i18n.StateHeader),
			)
			for _, item := range result {
				fmt.Fprintf(writer, "%s\t%s\t%d\t%s\n", item.ID, item.Client, item.Files, item.State)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

func newPromptToggleCommand(options *rootOptions, enabled bool) *cobra.Command {
	verb := "enable"
	short := "Enable a system prompt group for its user-global client"
	if !enabled {
		verb = "disable"
		short = "Disable a system prompt group for its user-global client"
	}
	return &cobra.Command{
		Use:   verb + " <group>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadPromptRuntime(options)
			if err != nil {
				return err
			}
			group, ok := runtime.prompts.Group(args[0])
			if !ok {
				return errors.New(runtime.translator.Text(i18n.UnknownPromptGroup, args[0]))
			}
			if err := runtime.promptMgr.SetEnabled([]systemprompt.Group{group}, enabled); err != nil {
				return err
			}
			key := i18n.EnabledResource
			if !enabled {
				key = i18n.DisabledResource
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(key, group.ID, group.Client))
			return nil
		},
	}
}
