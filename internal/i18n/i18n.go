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
	Ready                     Key = "ready"
	ProductSubtitle           Key = "product_subtitle"
	TabSkills                 Key = "tab_skills"
	TabMCP                    Key = "tab_mcp"
	TabSystemPrompts          Key = "tab_system_prompts"
	ProjectLabel              Key = "project_label"
	UserLabel                 Key = "user_label"
	FilterAll                 Key = "filter_all"
	FilterEnabled             Key = "filter_enabled"
	FilterIssues              Key = "filter_issues"
	FilterArchive             Key = "filter_archive"
	FilterSelected            Key = "filter_selected"
	SearchPlaceholder         Key = "search_placeholder"
	SourceSkillHeader         Key = "source_skill_header"
	ResourceHeader            Key = "resource_header"
	NoSkillsMatch             Key = "no_skills_match"
	NoResourcesMatch          Key = "no_resources_match"
	MoreRows                  Key = "more_rows"
	HelpNavigate              Key = "help_navigate"
	HelpResource              Key = "help_resource"
	HelpClient                Key = "help_client"
	HelpToggle                Key = "help_toggle"
	HelpExpand                Key = "help_expand"
	HelpSearch                Key = "help_search"
	HelpFilter                Key = "help_filter"
	HelpUpdate                Key = "help_update"
	HelpMore                  Key = "help_more"
	HelpQuit                  Key = "help_quit"
	TargetSelected            Key = "target_selected"
	NoMatchingSkills          Key = "no_matching_skills"
	IncompatibleSkill         Key = "incompatible_skill"
	UnavailableForClient      Key = "unavailable_for_client"
	CatalogCompatibility      Key = "catalog_compatibility"
	NoCompatibleSkills        Key = "no_compatible_skills"
	InspectProjectFailed      Key = "inspect_project_failed"
	ArchiveCannotEnable       Key = "archive_cannot_enable"
	ArchiveReferenceError     Key = "archive_reference_error"
	NoChangesApplied          Key = "no_changes_applied"
	EnabledSkills             Key = "enabled_skills"
	DisabledSkills            Key = "disabled_skills"
	EnabledResource           Key = "enabled_resource"
	DisabledResource          Key = "disabled_resource"
	ResourceIncompatible      Key = "resource_incompatible"
	UpdatesUnavailable        Key = "updates_unavailable"
	NoSourceSelected          Key = "no_source_selected"
	VendorOnlyUpdate          Key = "vendor_only_update"
	UpdatingSource            Key = "updating_source"
	UpdateFailed              Key = "update_failed"
	UpdateReloadFailed        Key = "update_reload_failed"
	UpdatedSources            Key = "updated_sources"
	NoSelection               Key = "no_selection"
	LocalSource               Key = "local_source"
	VendorBranch              Key = "vendor_branch"
	DiscoveryLabel            Key = "discovery_label"
	ArchiveReference          Key = "archive_reference"
	TargetsLabel              Key = "targets_label"
	ArchivedLabel             Key = "archived_label"
	MetadataIssueLabel        Key = "metadata_issue_label"
	RootShort                 Key = "root_short"
	ResourcesFlag             Key = "resources_flag"
	ProjectFlag               Key = "project_flag"
	LanguageFlag              Key = "language_flag"
	ListShort                 Key = "list_short"
	EmitJSONFlag              Key = "emit_json_flag"
	SkillHeader               Key = "skill_header"
	SourceHeader              Key = "source_header"
	EnableShort               Key = "enable_short"
	DisableShort              Key = "disable_short"
	ClientFlag                Key = "client_flag"
	SourceFlag                Key = "source_flag"
	SelectExactlyOne          Key = "select_exactly_one"
	AtLeastOneClient          Key = "at_least_one_client"
	UnknownClient             Key = "unknown_client"
	UnknownMCPServer          Key = "unknown_mcp_server"
	UnknownPromptGroup        Key = "unknown_prompt_group"
	SourceNoCompatibleSkills  Key = "source_no_compatible_skills"
	EnabledResult             Key = "enabled_result"
	DisabledResult            Key = "disabled_result"
	VersionShort              Key = "version_short"
	TUIShort                  Key = "tui_short"
	ShowShort                 Key = "show_short"
	StatusShort               Key = "status_short"
	SourceCommandShort        Key = "source_command_short"
	SourceListShort           Key = "source_list_short"
	SourceAddShort            Key = "source_add_short"
	SourceRemoveShort         Key = "source_remove_short"
	SourceRemoved             Key = "source_removed"
	MCPCommandShort           Key = "mcp_command_short"
	MCPListShort              Key = "mcp_list_short"
	MCPEnableShort            Key = "mcp_enable_short"
	MCPDisableShort           Key = "mcp_disable_short"
	PromptCommandShort        Key = "prompt_command_short"
	PromptListShort           Key = "prompt_list_short"
	PromptEnableShort         Key = "prompt_enable_short"
	PromptDisableShort        Key = "prompt_disable_short"
	PromptHeader              Key = "prompt_header"
	FilesHeader               Key = "files_header"
	PromptFileSummary         Key = "prompt_file_summary"
	UpdateShort               Key = "update_short"
	DoctorShort               Key = "doctor_short"
	IncludeArchiveFlag        Key = "include_archive_flag"
	NameFlag                  Key = "name_flag"
	BranchFlag                Key = "branch_flag"
	SourceScopeFlag           Key = "source_scope_flag"
	SkillPathFlag             Key = "skill_path_flag"
	SparseFlag                Key = "sparse_flag"
	DiscoveryPriorityFlag     Key = "discovery_priority_flag"
	DryRunFlag                Key = "dry_run_flag"
	SourceAdded               Key = "source_added"
	BranchHeader              Key = "branch_header"
	CurrentHeader             Key = "current_header"
	RemoteHeader              Key = "remote_header"
	ChangedHeader             Key = "changed_header"
	PathHeader                Key = "path_header"
	SkillsHeader              Key = "skills_header"
	KindHeader                Key = "kind_header"
	DiscoveryHeader           Key = "discovery_header"
	ClientHeader              Key = "client_header"
	StateHeader               Key = "state_header"
	EnabledHeader             Key = "enabled_header"
	DisabledHeader            Key = "disabled_header"
	IssuesHeader              Key = "issues_header"
	IncompatibleHeader        Key = "incompatible_header"
	DoctorHealthy             Key = "doctor_healthy"
	DoctorFoundIssues         Key = "doctor_found_issues"
	UnknownSkill              Key = "unknown_skill"
	UnknownSource             Key = "unknown_source"
	ArchivedCannotCLIEnable   Key = "archived_cannot_cli_enable"
	SourceNotVendor           Key = "source_not_vendor"
	NoVendorSources           Key = "no_vendor_sources"
	ChangedYes                Key = "changed_yes"
	ChangedNo                 Key = "changed_no"
	UsageHeading              Key = "usage_heading"
	AliasesHeading            Key = "aliases_heading"
	ExamplesHeading           Key = "examples_heading"
	AvailableCommandsHeading  Key = "available_commands_heading"
	AdditionalCommandsHeading Key = "additional_commands_heading"
	FlagsHeading              Key = "flags_heading"
	GlobalFlagsHeading        Key = "global_flags_heading"
	AdditionalHelpHeading     Key = "additional_help_heading"
	MoreInformationHint       Key = "more_information_hint"
	HelpFlag                  Key = "help_flag"
	HelpCommandShort          Key = "help_command_short"
)

type Translator struct {
	language Language
}

var messages = map[Language]map[Key]string{
	English: {
		Ready:                     "Ready",
		ProductSubtitle:           "project resources · user-global prompts",
		TabSkills:                 "Skills",
		TabMCP:                    "MCP",
		TabSystemPrompts:          "System Prompts",
		ProjectLabel:              "project",
		UserLabel:                 "user",
		FilterAll:                 "all",
		FilterEnabled:             "enabled",
		FilterIssues:              "issues",
		FilterArchive:             "archive",
		FilterSelected:            "Filter: %s",
		SearchPlaceholder:         "filter sources and skills",
		SourceSkillHeader:         "SOURCE / SKILL",
		ResourceHeader:            "RESOURCE",
		NoSkillsMatch:             "No skills match this view.",
		NoResourcesMatch:          "No resources match this view.",
		MoreRows:                  "↓ %d more",
		HelpNavigate:              "navigate",
		HelpResource:              "resource",
		HelpClient:                "client",
		HelpToggle:                "toggle",
		HelpExpand:                "expand",
		HelpSearch:                "search",
		HelpFilter:                "filter",
		HelpUpdate:                "update",
		HelpMore:                  "more",
		HelpQuit:                  "quit",
		TargetSelected:            "Target: %s",
		NoMatchingSkills:          "No matching skills",
		IncompatibleSkill:         "Incompatible skill",
		UnavailableForClient:      "%s is unavailable for %s: %s",
		CatalogCompatibility:      "catalog compatibility policy",
		NoCompatibleSkills:        "No compatible skills for %s",
		InspectProjectFailed:      "Could not inspect project state",
		ArchiveCannotEnable:       "Archived sources cannot be enabled",
		ArchiveReferenceError:     "%s is archived and reference-only",
		NoChangesApplied:          "No changes applied",
		EnabledSkills:             "Enabled %d skill(s) for %s",
		DisabledSkills:            "Disabled %d skill(s) for %s",
		EnabledResource:           "Enabled %s for %s",
		DisabledResource:          "Disabled %s for %s",
		ResourceIncompatible:      "%s is unavailable for %s",
		UpdatesUnavailable:        "Source updates are unavailable",
		NoSourceSelected:          "No source selected",
		VendorOnlyUpdate:          "Only vendor sources can be updated",
		UpdatingSource:            "Updating %s…",
		UpdateFailed:              "Update failed",
		UpdateReloadFailed:        "Updated sources, but catalog reload failed",
		UpdatedSources:            "Updated %d source(s); %d changed",
		NoSelection:               "No selection",
		LocalSource:               "local",
		VendorBranch:              "vendor · branch %s",
		DiscoveryLabel:            "discovery %s",
		ArchiveReference:          "archive · reference-only",
		TargetsLabel:              "targets  %s",
		ArchivedLabel:             "archived",
		MetadataIssueLabel:        "metadata issue",
		RootShort:                 "Manage project resources and user-global system prompts",
		ResourcesFlag:             "agent resources root (default ~/.agents/resources)",
		ProjectFlag:               "project directory (default current directory)",
		LanguageFlag:              "interface language: auto, en, or zh",
		ListShort:                 "List catalog skills and their current project state",
		EmitJSONFlag:              "emit JSON",
		SkillHeader:               "SKILL",
		SourceHeader:              "SOURCE",
		EnableShort:               "Enable a skill or source for the current project",
		DisableShort:              "Disable a skill or source for the current project",
		ClientFlag:                "registered target client (repeatable)",
		SourceFlag:                "operate on every compatible skill in a source",
		SelectExactlyOne:          "select exactly one skill-id or --source",
		AtLeastOneClient:          "at least one --client is required",
		UnknownClient:             "unknown client %q",
		UnknownMCPServer:          "unknown MCP server %q",
		UnknownPromptGroup:        "unknown system prompt group %q",
		SourceNoCompatibleSkills:  "source %s has no skills compatible with %s",
		EnabledResult:             "enabled %d skill(s) for %s\n",
		DisabledResult:            "disabled %d skill(s) for %s\n",
		VersionShort:              "Print version information",
		TUIShort:                  "Open the interactive terminal UI",
		ShowShort:                 "Show one skill and its project projections",
		StatusShort:               "Summarize project projection state by client",
		SourceCommandShort:        "Manage catalog source repositories",
		SourceListShort:           "List catalog sources",
		SourceAddShort:            "Add a vendor repository as a git submodule",
		SourceRemoveShort:         "Remove a clean vendor submodule and its catalog policy",
		SourceRemoved:             "removed source %s\n",
		MCPCommandShort:           "Manage project-level MCP servers",
		MCPListShort:              "List MCP servers and project state",
		MCPEnableShort:            "Enable an MCP server for project clients",
		MCPDisableShort:           "Disable an MCP server for project clients",
		PromptCommandShort:        "Manage user-global system prompt files",
		PromptListShort:           "List system prompt groups and user-global state",
		PromptEnableShort:         "Enable a system prompt group for its user-global client",
		PromptDisableShort:        "Disable a system prompt group for its user-global client",
		PromptHeader:              "PROMPT",
		FilesHeader:               "FILES",
		PromptFileSummary:         "%d Markdown file(s) · %s",
		UpdateShort:               "Update vendor sources from their tracked branches",
		DoctorShort:               "Check project resources and user-global system prompts",
		IncludeArchiveFlag:        "include archived sources",
		NameFlag:                  "source name",
		BranchFlag:                "tracked branch",
		SourceScopeFlag:           "restrict the entire source to one registered client",
		SkillPathFlag:             "authoritative Skill directory path (repeatable)",
		SparseFlag:                "additional sparse-checkout path (repeatable)",
		DiscoveryPriorityFlag:     "source discovery strategy priority (repeatable)",
		DryRunFlag:                "inspect updates without changing submodules",
		SourceAdded:               "added source %s\n",
		BranchHeader:              "BRANCH",
		CurrentHeader:             "CURRENT",
		RemoteHeader:              "REMOTE",
		ChangedHeader:             "CHANGED",
		PathHeader:                "PATH",
		SkillsHeader:              "SKILLS",
		KindHeader:                "KIND",
		DiscoveryHeader:           "DISCOVERY",
		ClientHeader:              "CLIENT",
		StateHeader:               "STATE",
		EnabledHeader:             "ENABLED",
		DisabledHeader:            "DISABLED",
		IssuesHeader:              "ISSUES",
		IncompatibleHeader:        "INCOMPATIBLE",
		DoctorHealthy:             "healthy",
		DoctorFoundIssues:         "doctor found %d projection issue(s)",
		UnknownSkill:              "unknown skill %q",
		UnknownSource:             "unknown source %q",
		ArchivedCannotCLIEnable:   "archived source %s is reference-only and cannot be enabled",
		SourceNotVendor:           "source %s is not an updateable vendor source",
		NoVendorSources:           "no vendor sources selected",
		ChangedYes:                "yes",
		ChangedNo:                 "no",
		UsageHeading:              "Usage",
		AliasesHeading:            "Aliases",
		ExamplesHeading:           "Examples",
		AvailableCommandsHeading:  "Available Commands",
		AdditionalCommandsHeading: "Additional Commands",
		FlagsHeading:              "Flags",
		GlobalFlagsHeading:        "Global Flags",
		AdditionalHelpHeading:     "Additional help topics",
		MoreInformationHint:       "Use \"%s [command] --help\" for more information about a command.",
		HelpFlag:                  "help for this command",
		HelpCommandShort:          "Help about any command",
	},
	Chinese: {
		Ready:                     "就绪",
		ProductSubtitle:           "项目资源 · 用户级系统提示词",
		TabSkills:                 "Skills",
		TabMCP:                    "MCP",
		TabSystemPrompts:          "系统提示词",
		ProjectLabel:              "项目",
		UserLabel:                 "用户",
		FilterAll:                 "全部",
		FilterEnabled:             "已启用",
		FilterIssues:              "问题",
		FilterArchive:             "归档",
		FilterSelected:            "筛选：%s",
		SearchPlaceholder:         "筛选来源与 Skills",
		SourceSkillHeader:         "来源 / SKILL",
		ResourceHeader:            "资源",
		NoSkillsMatch:             "当前视图没有匹配的 Skills。",
		NoResourcesMatch:          "当前视图没有匹配的资源。",
		MoreRows:                  "↓ 还有 %d 项",
		HelpNavigate:              "导航",
		HelpResource:              "资源",
		HelpClient:                "客户端",
		HelpToggle:                "开关",
		HelpExpand:                "展开",
		HelpSearch:                "搜索",
		HelpFilter:                "筛选",
		HelpUpdate:                "更新",
		HelpMore:                  "更多",
		HelpQuit:                  "退出",
		TargetSelected:            "目标：%s",
		NoMatchingSkills:          "没有匹配的 Skills",
		IncompatibleSkill:         "Skill 不兼容",
		UnavailableForClient:      "%s 不适用于 %s：%s",
		CatalogCompatibility:      "目录兼容性策略",
		NoCompatibleSkills:        "%s 没有兼容的 Skills",
		InspectProjectFailed:      "无法检查项目状态",
		ArchiveCannotEnable:       "归档来源不能启用",
		ArchiveReferenceError:     "%s 已归档，仅供参考",
		NoChangesApplied:          "未应用任何变更",
		EnabledSkills:             "已启用 %d 个 Skill（%s）",
		DisabledSkills:            "已停用 %d 个 Skill（%s）",
		EnabledResource:           "已为 %[2]s 启用 %[1]s",
		DisabledResource:          "已为 %[2]s 停用 %[1]s",
		ResourceIncompatible:      "%s 不适用于 %s",
		UpdatesUnavailable:        "当前无法更新来源",
		NoSourceSelected:          "未选择来源",
		VendorOnlyUpdate:          "只有 vendor 来源可以更新",
		UpdatingSource:            "正在更新 %s…",
		UpdateFailed:              "更新失败",
		UpdateReloadFailed:        "来源已更新，但重新加载目录失败",
		UpdatedSources:            "已更新 %d 个来源；%d 个发生变化",
		NoSelection:               "未选择",
		LocalSource:               "本地",
		VendorBranch:              "vendor · 分支 %s",
		DiscoveryLabel:            "发现策略 %s",
		ArchiveReference:          "归档 · 仅供参考",
		TargetsLabel:              "适用客户端  %s",
		ArchivedLabel:             "已归档",
		MetadataIssueLabel:        "元数据问题",
		RootShort:                 "管理项目资源和用户级系统提示词",
		ResourcesFlag:             "Agent 资源根目录（默认 ~/.agents/resources）",
		ProjectFlag:               "项目目录（默认当前目录）",
		LanguageFlag:              "界面语言：auto、en 或 zh",
		ListShort:                 "列出目录 Skills 及当前项目状态",
		EmitJSONFlag:              "输出 JSON",
		SkillHeader:               "SKILL",
		SourceHeader:              "来源",
		EnableShort:               "为当前项目启用一个 Skill 或来源",
		DisableShort:              "为当前项目停用一个 Skill 或来源",
		ClientFlag:                "已注册的目标客户端（可重复）",
		SourceFlag:                "操作来源内所有兼容的 Skills",
		SelectExactlyOne:          "必须且只能选择一个 skill-id 或 --source",
		AtLeastOneClient:          "至少需要一个 --client",
		UnknownClient:             "未知客户端 %q",
		UnknownMCPServer:          "未知 MCP server %q",
		UnknownPromptGroup:        "未知系统提示词组 %q",
		SourceNoCompatibleSkills:  "来源 %s 没有适用于 %s 的 Skills",
		EnabledResult:             "已启用 %d 个 Skill（%s）\n",
		DisabledResult:            "已停用 %d 个 Skill（%s）\n",
		VersionShort:              "输出版本信息",
		TUIShort:                  "打开交互式终端界面",
		ShowShort:                 "查看一个 Skill 及其项目投影",
		StatusShort:               "按客户端汇总项目投影状态",
		SourceCommandShort:        "管理目录来源仓库",
		SourceListShort:           "列出目录来源",
		SourceAddShort:            "将 vendor 仓库添加为 Git submodule",
		SourceRemoveShort:         "移除干净的 vendor submodule 及其目录策略",
		SourceRemoved:             "已移除来源 %s\n",
		MCPCommandShort:           "管理项目级 MCP servers",
		MCPListShort:              "列出 MCP servers 及项目状态",
		MCPEnableShort:            "为项目客户端启用一个 MCP server",
		MCPDisableShort:           "为项目客户端停用一个 MCP server",
		PromptCommandShort:        "管理用户级系统提示词文件",
		PromptListShort:           "列出系统提示词组及用户级状态",
		PromptEnableShort:         "为对应用户级客户端启用系统提示词组",
		PromptDisableShort:        "为对应用户级客户端停用系统提示词组",
		PromptHeader:              "提示词",
		FilesHeader:               "文件",
		PromptFileSummary:         "%d 个 Markdown 文件 · %s",
		UpdateShort:               "从跟踪分支更新 vendor 来源",
		DoctorShort:               "检查项目资源和用户级系统提示词",
		IncludeArchiveFlag:        "包含归档来源",
		NameFlag:                  "来源名称",
		BranchFlag:                "跟踪分支",
		SourceScopeFlag:           "将整个来源限制为一个已注册客户端",
		SkillPathFlag:             "权威 Skill 目录路径（可重复）",
		SparseFlag:                "附加稀疏检出路径（可重复）",
		DiscoveryPriorityFlag:     "来源发现策略优先级（可重复）",
		DryRunFlag:                "只检查更新，不修改 submodule",
		SourceAdded:               "已添加来源 %s\n",
		BranchHeader:              "分支",
		CurrentHeader:             "当前版本",
		RemoteHeader:              "远端版本",
		ChangedHeader:             "有更新",
		PathHeader:                "路径",
		SkillsHeader:              "SKILLS",
		KindHeader:                "类型",
		DiscoveryHeader:           "发现策略",
		ClientHeader:              "客户端",
		StateHeader:               "状态",
		EnabledHeader:             "已启用",
		DisabledHeader:            "已停用",
		IssuesHeader:              "问题",
		IncompatibleHeader:        "不兼容",
		DoctorHealthy:             "健康",
		DoctorFoundIssues:         "doctor 发现 %d 个投影问题",
		UnknownSkill:              "未知 Skill %q",
		UnknownSource:             "未知来源 %q",
		ArchivedCannotCLIEnable:   "归档来源 %s 仅供参考，不能启用",
		SourceNotVendor:           "来源 %s 不是可更新的 vendor 来源",
		NoVendorSources:           "未选择 vendor 来源",
		ChangedYes:                "是",
		ChangedNo:                 "否",
		UsageHeading:              "用法",
		AliasesHeading:            "别名",
		ExamplesHeading:           "示例",
		AvailableCommandsHeading:  "可用命令",
		AdditionalCommandsHeading: "其他命令",
		FlagsHeading:              "选项",
		GlobalFlagsHeading:        "全局选项",
		AdditionalHelpHeading:     "其他帮助主题",
		MoreInformationHint:       "使用 \"%s [command] --help\" 查看命令详情。",
		HelpFlag:                  "显示当前命令帮助",
		HelpCommandShort:          "查看任意命令的帮助",
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
