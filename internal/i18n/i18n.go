package i18n

import (
	"fmt"
	"os"
	"strings"
)

type Language string

const (
	English Language = "en"
	Chinese Language = "zh"
)

type Key string

const (
	Ready                       Key = "ready"
	ProductSubtitle             Key = "product_subtitle"
	TabSkills                   Key = "tab_skills"
	TabMCP                      Key = "tab_mcp"
	TabCommands                 Key = "tab_commands"
	TabHooks                    Key = "tab_hooks"
	TabAgents                   Key = "tab_agents"
	TabOutputStyles             Key = "tab_output_styles"
	TabSystemPrompts            Key = "tab_system_prompts"
	ProjectLabel                Key = "project_label"
	UserLabel                   Key = "user_label"
	FilterAll                   Key = "filter_all"
	FilterEnabled               Key = "filter_enabled"
	FilterIssues                Key = "filter_issues"
	FilterArchive               Key = "filter_archive"
	FilterSelected              Key = "filter_selected"
	SearchPlaceholder           Key = "search_placeholder"
	SourceSkillHeader           Key = "source_skill_header"
	ResourceHeader              Key = "resource_header"
	ScopeHeader                 Key = "scope_header"
	NoSkillsMatch               Key = "no_skills_match"
	NoResourcesMatch            Key = "no_resources_match"
	MoreRows                    Key = "more_rows"
	HelpNavigate                Key = "help_navigate"
	HelpResource                Key = "help_resource"
	HelpClient                  Key = "help_client"
	HelpToggle                  Key = "help_toggle"
	HelpToggleAll               Key = "help_toggle_all"
	HelpExpand                  Key = "help_expand"
	HelpSearch                  Key = "help_search"
	HelpFilter                  Key = "help_filter"
	HelpUpdate                  Key = "help_update"
	HelpUpdateAll               Key = "help_update_all"
	HelpBuild                   Key = "help_build"
	HelpScope                   Key = "help_scope"
	HelpLanguage                Key = "help_language"
	HelpMore                    Key = "help_more"
	HelpQuit                    Key = "help_quit"
	TargetSelected              Key = "target_selected"
	ScopeSelected               Key = "scope_selected"
	NoMatchingSkills            Key = "no_matching_skills"
	IncompatibleSkill           Key = "incompatible_skill"
	UnavailableForClient        Key = "unavailable_for_client"
	CatalogCompatibility        Key = "catalog_compatibility"
	NoCompatibleSkills          Key = "no_compatible_skills"
	InspectProjectFailed        Key = "inspect_project_failed"
	ArchiveCannotEnable         Key = "archive_cannot_enable"
	ArchiveReferenceError       Key = "archive_reference_error"
	NoChangesApplied            Key = "no_changes_applied"
	EnabledSkills               Key = "enabled_skills"
	DisabledSkills              Key = "disabled_skills"
	EnabledSkillAllClients      Key = "enabled_skill_all_clients"
	DisabledSkillAllClients     Key = "disabled_skill_all_clients"
	EnabledSourceAllClients     Key = "enabled_source_all_clients"
	DisabledSourceAllClients    Key = "disabled_source_all_clients"
	SelectSkillForAllClients    Key = "select_skill_for_all_clients"
	AllClientsNotForPrompts     Key = "all_clients_not_for_prompts"
	EnabledMCPAllClients        Key = "enabled_mcp_all_clients"
	DisabledMCPAllClients       Key = "disabled_mcp_all_clients"
	NoCompatibleClients         Key = "no_compatible_clients"
	EnabledResource             Key = "enabled_resource"
	DisabledResource            Key = "disabled_resource"
	ResourceIncompatible        Key = "resource_incompatible"
	UpdatesUnavailable          Key = "updates_unavailable"
	NoSourceSelected            Key = "no_source_selected"
	VendorOnlyUpdate            Key = "vendor_only_update"
	UpdatingSource              Key = "updating_source"
	UpdatingAllSources          Key = "updating_all_sources"
	UpdateFailed                Key = "update_failed"
	UpdatePartial               Key = "update_partial"
	UpdateReloadFailed          Key = "update_reload_failed"
	UpdatedSources              Key = "updated_sources"
	NoSelection                 Key = "no_selection"
	LocalSource                 Key = "local_source"
	VendorBranch                Key = "vendor_branch"
	DiscoveryLabel              Key = "discovery_label"
	ArchiveReference            Key = "archive_reference"
	TargetsLabel                Key = "targets_label"
	ArchivedLabel               Key = "archived_label"
	MetadataIssueLabel          Key = "metadata_issue_label"
	RootShort                   Key = "root_short"
	ResourcesFlag               Key = "resources_flag"
	ProjectFlag                 Key = "project_flag"
	LanguageFlag                Key = "language_flag"
	ListShort                   Key = "list_short"
	EmitJSONFlag                Key = "emit_json_flag"
	SkillHeader                 Key = "skill_header"
	SourceHeader                Key = "source_header"
	EnableShort                 Key = "enable_short"
	DisableShort                Key = "disable_short"
	ClientFlag                  Key = "client_flag"
	SourceFlag                  Key = "source_flag"
	SelectExactlyOne            Key = "select_exactly_one"
	AtLeastOneClient            Key = "at_least_one_client"
	UnknownClient               Key = "unknown_client"
	UnknownMCPServer            Key = "unknown_mcp_server"
	UnknownPromptGroup          Key = "unknown_prompt_group"
	SourceNoCompatibleSkills    Key = "source_no_compatible_skills"
	EnabledResult               Key = "enabled_result"
	DisabledResult              Key = "disabled_result"
	VersionShort                Key = "version_short"
	TUIShort                    Key = "tui_short"
	InitShort                   Key = "init_short"
	InitializedResources        Key = "initialized_resources"
	ResourcesAlreadyReady       Key = "resources_already_ready"
	ShowShort                   Key = "show_short"
	StatusShort                 Key = "status_short"
	SourceCommandShort          Key = "source_command_short"
	SourceListShort             Key = "source_list_short"
	SourceAddShort              Key = "source_add_short"
	SourceRemoveShort           Key = "source_remove_short"
	SourceRemoved               Key = "source_removed"
	MCPCommandShort             Key = "mcp_command_short"
	MCPListShort                Key = "mcp_list_short"
	MCPEnableShort              Key = "mcp_enable_short"
	MCPDisableShort             Key = "mcp_disable_short"
	PromptCommandShort          Key = "prompt_command_short"
	PromptListShort             Key = "prompt_list_short"
	PromptEnableShort           Key = "prompt_enable_short"
	PromptDisableShort          Key = "prompt_disable_short"
	PromptBuildShort            Key = "prompt_build_short"
	BuiltPrompt                 Key = "built_prompt"
	BuildingPrompt              Key = "building_prompt"
	PromptBuildFailed           Key = "prompt_build_failed"
	PromptBuildUnavailable      Key = "prompt_build_unavailable"
	CommandsCommandShort        Key = "commands_command_short"
	HooksCommandShort           Key = "hooks_command_short"
	AgentsCommandShort          Key = "agents_command_short"
	OutputStylesCommandShort    Key = "output_styles_command_short"
	UserResourceListShort       Key = "user_resource_list_short"
	UserResourceEnableShort     Key = "user_resource_enable_short"
	UserResourceDisableShort    Key = "user_resource_disable_short"
	PromptHeader                Key = "prompt_header"
	FilesHeader                 Key = "files_header"
	PromptFileSummary           Key = "prompt_file_summary"
	UpdateShort                 Key = "update_short"
	DoctorShort                 Key = "doctor_short"
	IncludeArchiveFlag          Key = "include_archive_flag"
	NameFlag                    Key = "name_flag"
	BranchFlag                  Key = "branch_flag"
	SourceScopeFlag             Key = "source_scope_flag"
	SkillPathFlag               Key = "skill_path_flag"
	SparseFlag                  Key = "sparse_flag"
	DiscoveryPriorityFlag       Key = "discovery_priority_flag"
	DryRunFlag                  Key = "dry_run_flag"
	SourceAdded                 Key = "source_added"
	SourceNameRequired          Key = "source_name_required"
	BranchHeader                Key = "branch_header"
	CurrentHeader               Key = "current_header"
	RemoteHeader                Key = "remote_header"
	ChangedHeader               Key = "changed_header"
	PathHeader                  Key = "path_header"
	SkillsHeader                Key = "skills_header"
	KindHeader                  Key = "kind_header"
	DiscoveryHeader             Key = "discovery_header"
	ClientHeader                Key = "client_header"
	StateHeader                 Key = "state_header"
	EnabledHeader               Key = "enabled_header"
	DisabledHeader              Key = "disabled_header"
	IssuesHeader                Key = "issues_header"
	IncompatibleHeader          Key = "incompatible_header"
	DoctorHealthy               Key = "doctor_healthy"
	DoctorFoundIssues           Key = "doctor_found_issues"
	UnknownSkill                Key = "unknown_skill"
	UnknownSource               Key = "unknown_source"
	ArchivedCannotCLIEnable     Key = "archived_cannot_cli_enable"
	SourceNotVendor             Key = "source_not_vendor"
	NoVendorSources             Key = "no_vendor_sources"
	ChangedYes                  Key = "changed_yes"
	ChangedNo                   Key = "changed_no"
	UpdatePrunedSummary         Key = "update_pruned_summary"
	UpdatePruneFailed           Key = "update_prune_failed"
	PruneNoOrphans              Key = "prune_no_orphans"
	PruneDryRunSummary          Key = "prune_dry_run_summary"
	UsageHeading                Key = "usage_heading"
	AliasesHeading              Key = "aliases_heading"
	ExamplesHeading             Key = "examples_heading"
	AvailableCommandsHeading    Key = "available_commands_heading"
	AdditionalCommandsHeading   Key = "additional_commands_heading"
	FlagsHeading                Key = "flags_heading"
	GlobalFlagsHeading          Key = "global_flags_heading"
	AdditionalHelpHeading       Key = "additional_help_heading"
	MoreInformationHint         Key = "more_information_hint"
	HelpFlag                    Key = "help_flag"
	HelpCommandShort            Key = "help_command_short"
	HelpDelete                  Key = "help_delete"
	DeleteConfirmTitle          Key = "delete_confirm_title"
	DeleteConfirmSource         Key = "delete_confirm_source"
	DeleteConfirmSkill          Key = "delete_confirm_skill"
	DeleteConfirmHint           Key = "delete_confirm_hint"
	Deleting                    Key = "deleting"
	DeletedSource               Key = "deleted_source"
	DeletedSkill                Key = "deleted_skill"
	DeleteFailed                Key = "delete_failed"
	DeleteReadOnlySkill         Key = "delete_read_only_skill"
	DeleteArchivedUnsupported   Key = "delete_archived_unsupported"
	DeleteUnavailable           Key = "delete_unavailable"
	DeleteConfirmMCP            Key = "delete_confirm_mcp"
	DeletedMCPServer            Key = "deleted_mcp_server"
	HelpAdd                     Key = "help_add"
	AddUnavailable              Key = "add_unavailable"
	AddMenuTitle                Key = "add_menu_title"
	AddMenuRepo                 Key = "add_menu_repo"
	AddMenuLocal                Key = "add_menu_local"
	AddRepoTitle                Key = "add_repo_title"
	AddRepoRequired             Key = "add_repo_required"
	AddRepoUnavailable          Key = "add_repo_unavailable"
	AddingRepo                  Key = "adding_repo"
	AddFailed                   Key = "add_failed"
	SourceAddedStatus           Key = "source_added_status"
	CreateSkillNameTitle        Key = "create_skill_name_title"
	CreateSkillNameInvalid      Key = "create_skill_name_invalid"
	CreateSkillDescTitle        Key = "create_skill_desc_title"
	AddMCPUnavailable           Key = "add_mcp_unavailable"
	MCPFormTitle                Key = "mcp_form_title"
	MCPNamePromptTitle          Key = "mcp_name_prompt_title"
	MCPNameRequired             Key = "mcp_name_required"
	MCPServerExists             Key = "mcp_server_exists"
	MCPServersAdded             Key = "mcp_servers_added"
	MCPServerAdded              Key = "mcp_server_added"
	MCPTransportAmbiguous       Key = "mcp_transport_ambiguous"
	DeleteNeedsConfirmation     Key = "delete_needs_confirmation"
	DeleteUnknownTarget         Key = "delete_unknown_target"
	DeleteVendorViaSourceRemove Key = "delete_vendor_via_source_remove"
	SkillsCommandShort          Key = "skills_command_short"
	SkillsCreateShort           Key = "skills_create_short"
	SkillCreated                Key = "skill_created"
	SkillsDeleteShort           Key = "skills_delete_short"
	MCPAddShort                 Key = "mcp_add_short"
	MCPImportShort              Key = "mcp_import_short"
	MCPImportFileFlag           Key = "mcp_import_file_flag"
	MCPImportNameFlag           Key = "mcp_import_name_flag"
	MCPImportNameRequired       Key = "mcp_import_name_required"
	MCPRemoveShort              Key = "mcp_remove_short"
)

type Translator struct {
	language Language
}

var messages = map[Language]map[Key]string{
	English: {
		Ready:                       "Ready",
		ProductSubtitle:             "project resources · user-global files",
		TabSkills:                   "Skills",
		TabMCP:                      "MCP",
		TabCommands:                 "Commands",
		TabHooks:                    "Hooks",
		TabAgents:                   "Agents",
		TabOutputStyles:             "Output Styles",
		TabSystemPrompts:            "System Prompts",
		ProjectLabel:                "project",
		UserLabel:                   "user",
		FilterAll:                   "all",
		FilterEnabled:               "enabled",
		FilterIssues:                "issues",
		FilterArchive:               "archive",
		FilterSelected:              "Filter: %s",
		SearchPlaceholder:           "filter sources and skills",
		SourceSkillHeader:           "NAME / SKILL",
		ResourceHeader:              "RESOURCE",
		ScopeHeader:                 "SCOPE",
		NoSkillsMatch:               "No skills match this view.",
		NoResourcesMatch:            "No resources match this view.",
		MoreRows:                    "↓ %d more",
		HelpNavigate:                "navigate",
		HelpResource:                "resource",
		HelpClient:                  "client",
		HelpToggle:                  "toggle",
		HelpToggleAll:               "all clients",
		HelpExpand:                  "expand",
		HelpSearch:                  "search",
		HelpFilter:                  "filter",
		HelpUpdate:                  "update",
		HelpUpdateAll:               "update all",
		HelpBuild:                   "build prompt",
		HelpScope:                   "scope",
		HelpLanguage:                "language",
		HelpMore:                    "more",
		HelpQuit:                    "quit",
		TargetSelected:              "Target: %s",
		ScopeSelected:               "Skill scope: %s",
		NoMatchingSkills:            "No matching skills",
		IncompatibleSkill:           "Incompatible skill",
		UnavailableForClient:        "%s is unavailable for %s: %s",
		CatalogCompatibility:        "catalog compatibility policy",
		NoCompatibleSkills:          "No compatible skills for %s",
		InspectProjectFailed:        "Could not inspect project state",
		ArchiveCannotEnable:         "Archived sources cannot be enabled",
		ArchiveReferenceError:       "%s is archived and reference-only",
		NoChangesApplied:            "No changes applied",
		EnabledSkills:               "Enabled %d skill(s) for %s",
		DisabledSkills:              "Disabled %d skill(s) for %s",
		EnabledSkillAllClients:      "Enabled %s for all %d compatible clients",
		DisabledSkillAllClients:     "Disabled %s for all %d compatible clients",
		EnabledSourceAllClients:     "Enabled %s across %d compatible projection(s)",
		DisabledSourceAllClients:    "Disabled %s across all clients (%d compatible projections)",
		SelectSkillForAllClients:    "Select a Skill or source to toggle all clients",
		AllClientsNotForPrompts:     "All-client toggle does not apply to system prompts",
		EnabledMCPAllClients:        "Enabled %s for all %d clients",
		DisabledMCPAllClients:       "Disabled %s for all %d clients",
		NoCompatibleClients:         "%s has no compatible clients",
		EnabledResource:             "Enabled %s for %s",
		DisabledResource:            "Disabled %s for %s",
		ResourceIncompatible:        "%s is unavailable for %s",
		UpdatesUnavailable:          "Source updates are unavailable",
		NoSourceSelected:            "No source selected",
		VendorOnlyUpdate:            "Only vendor sources can be updated",
		UpdatingSource:              "Updating %s…",
		UpdatingAllSources:          "Updating all %d vendor sources…",
		UpdateFailed:                "Update failed",
		UpdatePartial:               "Updated %d source(s); some sources failed",
		UpdateReloadFailed:          "Updated sources, but catalog reload failed",
		UpdatedSources:              "Updated %d source(s); %d changed",
		NoSelection:                 "No selection",
		LocalSource:                 "local",
		VendorBranch:                "vendor · branch %s",
		DiscoveryLabel:              "discovery %s",
		ArchiveReference:            "archive · reference-only",
		TargetsLabel:                "targets  %s",
		ArchivedLabel:               "archived",
		MetadataIssueLabel:          "metadata issue",
		RootShort:                   "Manage project resources and user-global agent files",
		ResourcesFlag:               "agent resources root (default ~/.agents/resources)",
		ProjectFlag:                 "project directory (default current directory)",
		LanguageFlag:                "interface language: auto, en, or zh",
		ListShort:                   "List catalog skills and their current project state",
		EmitJSONFlag:                "emit JSON",
		SkillHeader:                 "SKILL",
		SourceHeader:                "SOURCE",
		EnableShort:                 "Enable a skill or source for the current project",
		DisableShort:                "Disable a skill or source for the current project",
		ClientFlag:                  "registered target client (repeatable)",
		SourceFlag:                  "operate on every compatible skill in a source",
		SelectExactlyOne:            "select exactly one skill-id or --source",
		AtLeastOneClient:            "at least one --client is required",
		UnknownClient:               "unknown client %q",
		UnknownMCPServer:            "unknown MCP server %q",
		UnknownPromptGroup:          "unknown system prompt group %q",
		SourceNoCompatibleSkills:    "source %s has no skills compatible with %s",
		EnabledResult:               "enabled %d skill(s) for %s\n",
		DisabledResult:              "disabled %d skill(s) for %s\n",
		VersionShort:                "Print version information",
		TUIShort:                    "Open the interactive terminal UI",
		InitShort:                   "Initialize agent resources and the bundled operator Skill",
		InitializedResources:        "initialized %s and added %s\n",
		ResourcesAlreadyReady:       "%s is ready; %s is available\n",
		ShowShort:                   "Show one skill and its project projections",
		StatusShort:                 "Summarize project projection state by client",
		SourceCommandShort:          "Manage catalog source repositories",
		SourceListShort:             "List catalog sources",
		SourceAddShort:              "Add a vendor repository as a git submodule",
		SourceRemoveShort:           "Remove a clean vendor submodule and its catalog policy",
		SourceRemoved:               "removed source %s\n",
		MCPCommandShort:             "Manage project-level MCP servers",
		MCPListShort:                "List MCP servers and project state",
		MCPEnableShort:              "Enable an MCP server for project clients",
		MCPDisableShort:             "Disable an MCP server for project clients",
		PromptCommandShort:          "Manage user-global system prompt files",
		PromptListShort:             "List system prompt groups and user-global state",
		PromptEnableShort:           "Enable a system prompt group for its user-global client",
		PromptDisableShort:          "Disable a system prompt group for its user-global client",
		PromptBuildShort:            "Build a generated system prompt from its source files",
		BuiltPrompt:                 "Built %s at %s (%d bytes, changed: %t)",
		BuildingPrompt:              "Building %s…",
		PromptBuildFailed:           "Prompt build failed",
		PromptBuildUnavailable:      "%s uses tree projection and does not need a build",
		CommandsCommandShort:        "Manage project command files",
		HooksCommandShort:           "Manage project hook files",
		AgentsCommandShort:          "Manage user-global agent files",
		OutputStylesCommandShort:    "Manage user-global output style files",
		UserResourceListShort:       "List user-global resources and client state",
		UserResourceEnableShort:     "Enable a user-global resource for selected clients",
		UserResourceDisableShort:    "Disable a user-global resource for selected clients",
		PromptHeader:                "PROMPT",
		FilesHeader:                 "FILES",
		PromptFileSummary:           "%d Markdown file(s) · %s",
		UpdateShort:                 "Update vendor sources from their tracked branches",
		DoctorShort:                 "Check project resources and user-global agent files",
		IncludeArchiveFlag:          "include archived sources",
		NameFlag:                    "source name",
		BranchFlag:                  "tracked branch",
		SourceScopeFlag:             "restrict the entire source to one registered client",
		SkillPathFlag:               "authoritative Skill directory path (repeatable)",
		SparseFlag:                  "additional sparse-checkout path (repeatable)",
		DiscoveryPriorityFlag:       "source discovery strategy priority (repeatable)",
		DryRunFlag:                  "inspect updates without changing submodules",
		SourceAdded:                 "added source %s\n",
		SourceNameRequired:          "source name is required (could not derive it from the URL); pass --name",
		BranchHeader:                "BRANCH",
		CurrentHeader:               "CURRENT",
		RemoteHeader:                "REMOTE",
		ChangedHeader:               "CHANGED",
		PathHeader:                  "PATH",
		SkillsHeader:                "SKILLS",
		KindHeader:                  "KIND",
		DiscoveryHeader:             "DISCOVERY",
		ClientHeader:                "CLIENT",
		StateHeader:                 "STATE",
		EnabledHeader:               "ENABLED",
		DisabledHeader:              "DISABLED",
		IssuesHeader:                "ISSUES",
		IncompatibleHeader:          "INCOMPATIBLE",
		DoctorHealthy:               "healthy",
		DoctorFoundIssues:           "doctor found %d projection issue(s)",
		UnknownSkill:                "unknown skill %q",
		UnknownSource:               "unknown source %q",
		ArchivedCannotCLIEnable:     "archived source %s is reference-only and cannot be enabled",
		SourceNotVendor:             "source %s is not an updateable vendor source",
		NoVendorSources:             "no vendor sources selected",
		UpdatePrunedSummary:         "removed %d orphaned projection(s) whose skill left the source:",
		UpdatePruneFailed:           "some orphaned projections could not be removed: %v",
		PruneNoOrphans:              "no orphaned projections found",
		PruneDryRunSummary:          "%d orphaned projection(s) would be removed; re-run with --yes:",
		ChangedYes:                  "yes",
		ChangedNo:                   "no",
		UsageHeading:                "Usage",
		AliasesHeading:              "Aliases",
		ExamplesHeading:             "Examples",
		AvailableCommandsHeading:    "Available Commands",
		AdditionalCommandsHeading:   "Additional Commands",
		FlagsHeading:                "Flags",
		GlobalFlagsHeading:          "Global Flags",
		AdditionalHelpHeading:       "Additional help topics",
		MoreInformationHint:         "Use \"%s [command] --help\" for more information about a command.",
		HelpFlag:                    "help for this command",
		HelpCommandShort:            "Help about any command",
		HelpDelete:                  "delete",
		DeleteConfirmTitle:          "Confirm deletion",
		DeleteConfirmSource:         "Remove source %s and all %d skills from disk. This cannot be undone.",
		DeleteConfirmSkill:          "Remove skill %s from disk. This cannot be undone.",
		DeleteConfirmHint:           "[y] delete   [n/esc] cancel",
		Deleting:                    "Deleting %s…",
		DeletedSource:               "Removed source %s",
		DeletedSkill:                "Removed skill %s",
		DeleteFailed:                "Deletion failed",
		DeleteReadOnlySkill:         "Read-only source: delete the whole source or disable the skill; individual skills cannot be removed",
		DeleteArchivedUnsupported:   "Archived references cannot be deleted here",
		DeleteUnavailable:           "Deletion is only available for skill sources",
		DeleteConfirmMCP:            "Remove MCP server %s from the catalog. This cannot be undone.",
		DeletedMCPServer:            "Removed MCP server %s",
		HelpAdd:                     "new",
		AddMCPUnavailable:           "Adding servers is only available on the MCP tab",
		AddUnavailable:              "adding is available on the Skills and MCP tabs",
		AddMenuTitle:                "Add to the Skills catalog",
		AddMenuRepo:                 "Add a remote repo source",
		AddMenuLocal:                "Create a local Skill",
		AddRepoTitle:                "Repository URL (GitHub or GitLab)",
		AddRepoRequired:             "a repository URL is required",
		AddRepoUnavailable:          "adding a remote source is unavailable here",
		AddingRepo:                  "adding source %s…",
		AddFailed:                   "add failed",
		SourceAddedStatus:           "added source %s",
		CreateSkillNameTitle:        "Skill name",
		CreateSkillNameInvalid:      "use letters, digits, dot, dash, or underscore",
		CreateSkillDescTitle:        "Description (optional)",
		MCPFormTitle:                "Paste MCP JSON",
		MCPNamePromptTitle:          "Server name for the pasted object",
		MCPNameRequired:             "server name is required",
		MCPServerExists:             "MCP server already exists: %s",
		MCPServersAdded:             "added %d MCP servers",
		MCPServerAdded:              "Added MCP server %s",
		MCPTransportAmbiguous:       "cannot infer transport; pass --command for stdio or --url for http",
		DeleteNeedsConfirmation:     "%s will be permanently deleted from disk; re-run with --yes to confirm",
		DeleteUnknownTarget:         "unknown Skill or source: %s",
		DeleteVendorViaSourceRemove: "%s is a vendor source; use `source remove` instead",
		SkillsCommandShort:          "List, show, enable, disable, create, or delete catalog skills",
		SkillsCreateShort:           "Scaffold a new local Skill skeleton",
		SkillCreated:                "created local Skill at %s",
		SkillsDeleteShort:           "Delete a local Skill or group directory from the resource SSOT",
		MCPAddShort:                 "Register a new MCP server in the catalog",
		MCPImportShort:              "Add MCP servers from a pasted JSON definition",
		MCPImportFileFlag:           "read the JSON definition from a file",
		MCPImportNameFlag:           "server name for a bare (unkeyed) object",
		MCPImportNameRequired:       "a bare MCP object needs --name",
		MCPRemoveShort:              "Remove an MCP server from the catalog",
	},
	Chinese: {
		Ready:                       "就绪",
		ProductSubtitle:             "项目资源 · 用户级 Agent 文件",
		TabSkills:                   "Skills",
		TabMCP:                      "MCP",
		TabCommands:                 "命令",
		TabHooks:                    "Hooks",
		TabAgents:                   "Agents",
		TabOutputStyles:             "输出样式",
		TabSystemPrompts:            "系统提示词",
		ProjectLabel:                "项目",
		UserLabel:                   "用户",
		FilterAll:                   "全部",
		FilterEnabled:               "已启用",
		FilterIssues:                "问题",
		FilterArchive:               "归档",
		FilterSelected:              "筛选：%s",
		SearchPlaceholder:           "筛选来源与 Skills",
		SourceSkillHeader:           "名称 / SKILL",
		ResourceHeader:              "资源",
		ScopeHeader:                 "作用域",
		NoSkillsMatch:               "当前视图没有匹配的 Skills。",
		NoResourcesMatch:            "当前视图没有匹配的资源。",
		MoreRows:                    "↓ 还有 %d 项",
		HelpNavigate:                "导航",
		HelpResource:                "资源",
		HelpClient:                  "客户端",
		HelpToggle:                  "开关",
		HelpToggleAll:               "全部客户端",
		HelpExpand:                  "展开",
		HelpSearch:                  "搜索",
		HelpFilter:                  "筛选",
		HelpUpdate:                  "更新",
		HelpUpdateAll:               "全部更新",
		HelpBuild:                   "构建提示词",
		HelpScope:                   "作用域",
		HelpLanguage:                "语言",
		HelpMore:                    "更多",
		HelpQuit:                    "退出",
		TargetSelected:              "目标：%s",
		ScopeSelected:               "Skill 作用域：%s",
		NoMatchingSkills:            "没有匹配的 Skills",
		IncompatibleSkill:           "Skill 不兼容",
		UnavailableForClient:        "%s 不适用于 %s：%s",
		CatalogCompatibility:        "目录兼容性策略",
		NoCompatibleSkills:          "%s 没有兼容的 Skills",
		InspectProjectFailed:        "无法检查项目状态",
		ArchiveCannotEnable:         "归档来源不能启用",
		ArchiveReferenceError:       "%s 已归档，仅供参考",
		NoChangesApplied:            "未应用任何变更",
		EnabledSkills:               "已启用 %d 个 Skill（%s）",
		DisabledSkills:              "已停用 %d 个 Skill（%s）",
		EnabledSkillAllClients:      "已在全部 %[2]d 个兼容客户端启用 %[1]s",
		DisabledSkillAllClients:     "已在全部 %[2]d 个兼容客户端停用 %[1]s",
		EnabledSourceAllClients:     "已为 %[1]s 启用全部 %[2]d 个兼容投影",
		DisabledSourceAllClients:    "已在全部客户端停用 %[1]s（%[2]d 个兼容投影）",
		SelectSkillForAllClients:    "请选择一个 Skill 或来源，再切换全部客户端",
		AllClientsNotForPrompts:     "全部客户端切换不适用于系统提示词",
		EnabledMCPAllClients:        "已在全部 %[2]d 个客户端启用 %[1]s",
		DisabledMCPAllClients:       "已在全部 %[2]d 个客户端停用 %[1]s",
		NoCompatibleClients:         "%s 没有兼容的客户端",
		EnabledResource:             "已为 %[2]s 启用 %[1]s",
		DisabledResource:            "已为 %[2]s 停用 %[1]s",
		ResourceIncompatible:        "%s 不适用于 %s",
		UpdatesUnavailable:          "当前无法更新来源",
		NoSourceSelected:            "未选择来源",
		VendorOnlyUpdate:            "只有 vendor 来源可以更新",
		UpdatingSource:              "正在更新 %s…",
		UpdatingAllSources:          "正在更新全部 %d 个 vendor 来源…",
		UpdateFailed:                "更新失败",
		UpdatePartial:               "已更新 %d 个来源；部分来源失败",
		UpdateReloadFailed:          "来源已更新，但重新加载目录失败",
		UpdatedSources:              "已更新 %d 个来源；%d 个发生变化",
		NoSelection:                 "未选择",
		LocalSource:                 "本地",
		VendorBranch:                "vendor · 分支 %s",
		DiscoveryLabel:              "发现策略 %s",
		ArchiveReference:            "归档 · 仅供参考",
		TargetsLabel:                "适用客户端  %s",
		ArchivedLabel:               "已归档",
		MetadataIssueLabel:          "元数据问题",
		RootShort:                   "管理项目资源和用户级 Agent 文件",
		ResourcesFlag:               "Agent 资源根目录（默认 ~/.agents/resources）",
		ProjectFlag:                 "项目目录（默认当前目录）",
		LanguageFlag:                "界面语言：auto、en 或 zh",
		ListShort:                   "列出目录 Skills 及当前项目状态",
		EmitJSONFlag:                "输出 JSON",
		SkillHeader:                 "SKILL",
		SourceHeader:                "来源",
		EnableShort:                 "为当前项目启用一个 Skill 或来源",
		DisableShort:                "为当前项目停用一个 Skill 或来源",
		ClientFlag:                  "已注册的目标客户端（可重复）",
		SourceFlag:                  "操作来源内所有兼容的 Skills",
		SelectExactlyOne:            "必须且只能选择一个 skill-id 或 --source",
		AtLeastOneClient:            "至少需要一个 --client",
		UnknownClient:               "未知客户端 %q",
		UnknownMCPServer:            "未知 MCP server %q",
		UnknownPromptGroup:          "未知系统提示词组 %q",
		SourceNoCompatibleSkills:    "来源 %s 没有适用于 %s 的 Skills",
		EnabledResult:               "已启用 %d 个 Skill（%s）\n",
		DisabledResult:              "已停用 %d 个 Skill（%s）\n",
		VersionShort:                "输出版本信息",
		TUIShort:                    "打开交互式终端界面",
		InitShort:                   "初始化 Agent 资源和内置操作 Skill",
		InitializedResources:        "已初始化 %s，并添加 %s\n",
		ResourcesAlreadyReady:       "%s 已就绪；%s 可用\n",
		ShowShort:                   "查看一个 Skill 及其项目投影",
		StatusShort:                 "按客户端汇总项目投影状态",
		SourceCommandShort:          "管理目录来源仓库",
		SourceListShort:             "列出目录来源",
		SourceAddShort:              "将 vendor 仓库添加为 Git submodule",
		SourceRemoveShort:           "移除干净的 vendor submodule 及其目录策略",
		SourceRemoved:               "已移除来源 %s\n",
		MCPCommandShort:             "管理项目级 MCP servers",
		MCPListShort:                "列出 MCP servers 及项目状态",
		MCPEnableShort:              "为项目客户端启用一个 MCP server",
		MCPDisableShort:             "为项目客户端停用一个 MCP server",
		PromptCommandShort:          "管理用户级系统提示词文件",
		PromptListShort:             "列出系统提示词组及用户级状态",
		PromptEnableShort:           "为对应用户级客户端启用系统提示词组",
		PromptDisableShort:          "为对应用户级客户端停用系统提示词组",
		PromptBuildShort:            "从源文件构建生成式系统提示词",
		BuiltPrompt:                 "已构建 %[1]s：%[2]s（%[3]d bytes，发生变化：%[4]t）",
		BuildingPrompt:              "正在构建 %s…",
		PromptBuildFailed:           "提示词构建失败",
		PromptBuildUnavailable:      "%s 使用 tree 投影，不需要构建",
		CommandsCommandShort:        "管理项目级命令文件",
		HooksCommandShort:           "管理项目级 Hook 文件",
		AgentsCommandShort:          "管理用户级 Agent 文件",
		OutputStylesCommandShort:    "管理用户级输出样式文件",
		UserResourceListShort:       "列出用户级资源及客户端状态",
		UserResourceEnableShort:     "为选定客户端启用用户级资源",
		UserResourceDisableShort:    "为选定客户端停用用户级资源",
		PromptHeader:                "提示词",
		FilesHeader:                 "文件",
		PromptFileSummary:           "%d 个 Markdown 文件 · %s",
		UpdateShort:                 "从跟踪分支更新 vendor 来源",
		DoctorShort:                 "检查项目资源和用户级 Agent 文件",
		IncludeArchiveFlag:          "包含归档来源",
		NameFlag:                    "来源名称",
		BranchFlag:                  "跟踪分支",
		SourceScopeFlag:             "将整个来源限制为一个已注册客户端",
		SkillPathFlag:               "权威 Skill 目录路径（可重复）",
		SparseFlag:                  "附加稀疏检出路径（可重复）",
		DiscoveryPriorityFlag:       "来源发现策略优先级（可重复）",
		DryRunFlag:                  "只检查更新，不修改 submodule",
		SourceAdded:                 "已添加来源 %s\n",
		SourceNameRequired:          "需要来源名称（无法从 URL 推导）；请传 --name",
		BranchHeader:                "分支",
		CurrentHeader:               "当前版本",
		RemoteHeader:                "远端版本",
		ChangedHeader:               "有更新",
		PathHeader:                  "路径",
		SkillsHeader:                "SKILLS",
		KindHeader:                  "类型",
		DiscoveryHeader:             "发现策略",
		ClientHeader:                "客户端",
		StateHeader:                 "状态",
		EnabledHeader:               "已启用",
		DisabledHeader:              "已停用",
		IssuesHeader:                "问题",
		IncompatibleHeader:          "不兼容",
		DoctorHealthy:               "健康",
		DoctorFoundIssues:           "doctor 发现 %d 个投影问题",
		UnknownSkill:                "未知 Skill %q",
		UnknownSource:               "未知来源 %q",
		ArchivedCannotCLIEnable:     "归档来源 %s 仅供参考，不能启用",
		SourceNotVendor:             "来源 %s 不是可更新的 vendor 来源",
		NoVendorSources:             "未选择 vendor 来源",
		UpdatePrunedSummary:         "更新后清理了 %d 条已从来源移除的失效投影：",
		UpdatePruneFailed:           "部分失效投影未能清理：%v",
		PruneNoOrphans:              "未发现失效投影",
		PruneDryRunSummary:          "发现 %d 条失效投影，加 --yes 执行清理：",
		ChangedYes:                  "是",
		ChangedNo:                   "否",
		UsageHeading:                "用法",
		AliasesHeading:              "别名",
		ExamplesHeading:             "示例",
		AvailableCommandsHeading:    "可用命令",
		AdditionalCommandsHeading:   "其他命令",
		FlagsHeading:                "选项",
		GlobalFlagsHeading:          "全局选项",
		AdditionalHelpHeading:       "其他帮助主题",
		MoreInformationHint:         "使用 \"%s [command] --help\" 查看命令详情。",
		HelpFlag:                    "显示当前命令帮助",
		HelpCommandShort:            "查看任意命令的帮助",
		HelpDelete:                  "删除",
		DeleteConfirmTitle:          "确认删除",
		DeleteConfirmSource:         "将从磁盘删除来源 %s 及其全部 %d 个技能,不可撤销。",
		DeleteConfirmSkill:          "将从磁盘删除技能 %s,不可撤销。",
		DeleteConfirmHint:           "[y] 删除   [n/esc] 取消",
		Deleting:                    "正在删除 %s…",
		DeletedSource:               "已删除来源 %s",
		DeletedSkill:                "已删除技能 %s",
		DeleteFailed:                "删除失败",
		DeleteReadOnlySkill:         "只读来源:请删除整个来源或 disable 技能,不能单独删除其中的技能",
		DeleteArchivedUnsupported:   "归档引用不能在此删除",
		DeleteUnavailable:           "仅技能来源支持删除",
		DeleteConfirmMCP:            "将从目录删除 MCP 服务器 %s,不可撤销。",
		DeletedMCPServer:            "已删除 MCP 服务器 %s",
		HelpAdd:                     "新建",
		AddMCPUnavailable:           "仅 MCP 标签页支持添加服务器",
		AddUnavailable:              "新建仅在 Skills 和 MCP 标签页可用",
		AddMenuTitle:                "添加到 Skills 目录",
		AddMenuRepo:                 "添加远程 repo 来源",
		AddMenuLocal:                "创建本地 Skill",
		AddRepoTitle:                "仓库 URL(GitHub 或 GitLab)",
		AddRepoRequired:             "需要仓库 URL",
		AddRepoUnavailable:          "此处无法添加远程来源",
		AddingRepo:                  "正在添加来源 %s…",
		AddFailed:                   "添加失败",
		SourceAddedStatus:           "已添加来源 %s",
		CreateSkillNameTitle:        "Skill 名称",
		CreateSkillNameInvalid:      "只能用字母、数字、点、连字符或下划线",
		CreateSkillDescTitle:        "描述(可选)",
		MCPFormTitle:                "粘贴 MCP JSON",
		MCPNamePromptTitle:          "为粘贴的对象指定服务器名称",
		MCPNameRequired:             "需要服务器名称",
		MCPServerExists:             "MCP 服务器已存在:%s",
		MCPServersAdded:             "已添加 %d 个 MCP 服务器",
		MCPServerAdded:              "已添加 MCP 服务器 %s",
		MCPTransportAmbiguous:       "无法推断 transport;stdio 用 --command,http 用 --url",
		DeleteNeedsConfirmation:     "%s 将从磁盘永久删除;请加 --yes 确认",
		DeleteUnknownTarget:         "未知的技能或来源:%s",
		DeleteVendorViaSourceRemove: "%s 是 vendor 来源,请改用 `source remove`",
		SkillsCommandShort:          "列出、查看、启用、停用、创建或删除目录技能",
		SkillsCreateShort:           "生成一个新的本地 Skill 骨架",
		SkillCreated:                "已在 %s 创建本地 Skill",
		SkillsDeleteShort:           "从资源 SSOT 删除本地技能或组目录",
		MCPAddShort:                 "在目录中注册新的 MCP 服务器",
		MCPImportShort:              "从粘贴的 JSON 定义添加 MCP 服务器",
		MCPImportFileFlag:           "从文件读取 JSON 定义",
		MCPImportNameFlag:           "为裸(无键)对象指定服务器名称",
		MCPImportNameRequired:       "裸 MCP 对象需要 --name",
		MCPRemoveShort:              "从目录移除 MCP 服务器",
	},
}

func New(language Language) Translator {
	if language != Chinese {
		language = English
	}
	return Translator{language: language}
}

func Resolve(configured string, environment map[string]string) (Translator, error) {
	value := strings.ToLower(strings.TrimSpace(configured))
	if value == "" || value == "auto" {
		value = detectedLocale(environment)
	}
	language := English
	switch {
	case strings.HasPrefix(value, "zh"):
		language = Chinese
	case value == "", value == "c", strings.HasPrefix(value, "c."), value == "posix", strings.HasPrefix(value, "en"):
		language = English
	default:
		return Translator{}, fmt.Errorf("unsupported language %q; use auto, en, or zh", configured)
	}
	return Translator{language: language}, nil
}

func FromEnvironment(configured string) (Translator, error) {
	environment := map[string]string{
		"LC_ALL":      os.Getenv("LC_ALL"),
		"LC_MESSAGES": os.Getenv("LC_MESSAGES"),
		"LANG":        os.Getenv("LANG"),
	}
	return Resolve(configured, environment)
}

func (t Translator) Language() Language {
	return t.language
}

func (t Translator) Text(key Key, arguments ...any) string {
	message, ok := messages[t.language][key]
	if !ok {
		message = messages[English][key]
	}
	if message == "" {
		message = string(key)
	}
	if len(arguments) == 0 {
		return message
	}
	return fmt.Sprintf(message, arguments...)
}

func detectedLocale(environment map[string]string) string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := strings.TrimSpace(environment[key]); value != "" {
			return strings.ToLower(value)
		}
	}
	return "en"
}
