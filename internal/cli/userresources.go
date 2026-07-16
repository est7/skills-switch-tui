package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/userresource"
	"github.com/spf13/cobra"
)

func newUserResourceCommand(options *rootOptions, descriptor userresource.Descriptor) *cobra.Command {
	command := &cobra.Command{Use: descriptor.Command, Short: descriptor.CommandSummary, Args: cobra.NoArgs}
	command.AddCommand(newUserResourceListCommand(options, descriptor))
	command.AddCommand(newUserResourceToggleCommand(options, descriptor, true))
	command.AddCommand(newUserResourceToggleCommand(options, descriptor, false))
	return command
}

type userResourceView struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Clients map[string]string `json:"clients"`
}

func newUserResourceListCommand(options *rootOptions, descriptor userresource.Descriptor) *cobra.Command {
	var outputJSON bool
	short := "List user-global resources and client state"
	if descriptor.TargetScope == userresource.TargetProject {
		short = "List project resources and client state"
	}
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"query"},
		Short:   short,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtime, err := loadUserResourceRuntime(options, descriptor.Kind)
			if err != nil {
				return err
			}
			clientIDs := runtime.catalog.Clients.IDsFor(descriptor.Capability)
			result := make([]userResourceView, 0, len(runtime.resources.Resources))
			for _, resource := range runtime.resources.Resources {
				item := userResourceView{ID: resource.ID, Kind: string(descriptor.Kind), Clients: make(map[string]string)}
				for _, clientID := range clientIDs {
					state, err := runtime.manager.State(resource, clientID)
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
			fmt.Fprintf(writer, "%s\t", runtime.translator.Text(i18n.ResourceHeader))
			for _, clientID := range clientIDs {
				fmt.Fprintf(writer, "%s\t", strings.ToUpper(string(clientID)))
			}
			fmt.Fprintln(writer)
			for _, item := range result {
				fmt.Fprintf(writer, "%s\t", item.ID)
				for _, clientID := range clientIDs {
					fmt.Fprintf(writer, "%s\t", item.Clients[string(clientID)])
				}
				fmt.Fprintln(writer)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit JSON")
	return command
}

func newUserResourceToggleCommand(options *rootOptions, descriptor userresource.Descriptor, enabled bool) *cobra.Command {
	var clients []string
	verb := "enable"
	short := "Enable a user-global resource for selected clients"
	if descriptor.TargetScope == userresource.TargetProject {
		short = "Enable a project resource for selected clients"
	}
	if !enabled {
		verb = "disable"
		short = "Disable a user-global resource for selected clients"
		if descriptor.TargetScope == userresource.TargetProject {
			short = "Disable a project resource for selected clients"
		}
	}
	command := &cobra.Command{
		Use:   verb + " <resource-id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			runtime, err := loadUserResourceRuntime(options, descriptor.Kind)
			if err != nil {
				return err
			}
			resource, ok := runtime.resources.Resource(args[0])
			if !ok {
				return fmt.Errorf("unknown %s %q", descriptor.Kind, args[0])
			}
			selected, err := parseClientsForCapability(clients, runtime.catalog, runtime.translator, descriptor.Capability)
			if err != nil {
				return err
			}
			operations := make([]userresource.Operation, 0, len(selected))
			for _, clientID := range selected {
				if !resource.Supports(clientID) {
					return errors.New(runtime.translator.Text(i18n.ResourceIncompatible, resource.ID, clientID))
				}
				operations = append(operations, userresource.Operation{Resource: resource, Client: clientID, Enabled: enabled})
			}
			if err := runtime.manager.Apply(operations); err != nil {
				return err
			}
			key := i18n.EnabledResource
			if !enabled {
				key = i18n.DisabledResource
			}
			clientNames := make([]string, len(selected))
			for index, clientID := range selected {
				clientNames[index] = string(clientID)
			}
			fmt.Fprintln(command.OutOrStdout(), runtime.translator.Text(key, resource.ID, strings.Join(clientNames, ",")))
			return nil
		},
	}
	command.Flags().StringSliceVar(&clients, "client", nil, "registered target client (repeatable)")
	return command
}
