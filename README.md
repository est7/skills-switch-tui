# skills-switch

`skills-switch` manages project-local Agent Skills and MCP servers plus user-global commands, hooks, and system prompts from a central resource catalog. Skills are projected into the current project, MCP servers are surgically merged into each client's project configuration, and user-global files are projected into each client's registered directory. There is no per-project manifest to drift away from the real client files.

The TUI is built with Bubble Tea v2, Bubbles v2, and Lip Gloss v2. Human-facing CLI and TUI text supports English and Simplified Chinese.

## Model

The default resource root is `~/.agents/resources`:

```text
~/.agents/
└── resources/
    ├── skills/
    │   ├── catalog.yaml
    │   ├── local/
    │   │   ├── shared/          # local skills supported by every client
    │   │   ├── codex/           # local skills supported only by Codex
    │   │   └── pi/              # valid when the pi client is registered
    │   ├── archived/
    │   │   ├── shared/          # reference-only collections; never enabled
    │   │   ├── codex/
    │   │   └── pi/
    │   └── vendor/
    │       ├── shared/<repo>/   # upstream repositories as git submodules
    │       ├── codex/<repo>/
    │       └── pi/<repo>/
    ├── registry.yaml            # client adapters; built-ins work without this file
    ├── mcp/
    │   └── mcp.json             # client-neutral MCP definitions
    ├── commands/
    │   ├── shared/              # available to every command-capable client
    │   └── codex-only/          # available only to Codex
    ├── hooks/
    │   ├── shared/              # available to every hook-capable client
    │   └── claude-only/         # available only to Claude
    ├── agents/                  # user-global model-specific agent definitions
    ├── output-styles/           # user-global Claude output styles
    └── system-prompts/
        ├── claude-prompt/
        │   ├── CLAUDE.md
        │   └── rules/*.md
        └── codex-prompt/
            └── AGENTS.md
```

Inside `resources/skills`, the physical layout remains a strict `kind / client-scope` matrix. TUI and CLI use stable logical source IDs derived from it:

| Physical path | Logical source ID |
| --- | --- |
| `local/shared` | `local-shared` |
| `local/pi` | `local-pi-only` |
| `archived/pi/<name>` | `archived-pi-only/<name>` |
| `vendor/shared/<repo>` | `vendor-shared/<repo>` |
| `vendor/pi/<repo>` | `vendor-pi-only/<repo>` |

The built-in client registry owns both project-local and user-global client adapters:

| Client | Project Skills | Project MCP | User commands/hooks | User agents | User output styles | User system prompts |
| --- | --- | --- | --- | --- | --- | --- |
| `codex` | `.agents/skills` | `.codex/config.toml` | `.codex/prompts`, `.codex/hooks` | `.codex/agents` | — | `.codex/` |
| `claude` | `.claude/skills` | `.mcp.json` | `.claude/commands`, `.claude/hooks` | `.claude/agents` | `.claude/output-styles` | `.claude/` |
| `gemini` | `.gemini/skills` | `.gemini/settings.json` | `.gemini/commands`, `.gemini/hooks` | `.gemini/agents` | — | `.gemini/` |

Clients are data, not a closed enum. Built-ins live in the binary; `resources/registry.yaml` can override them or add another client without changing Go code:

```yaml
version: 1

clients:
  pi:
    projectSkillsDir: .pi/skills
    userPromptDir: .pi
    userPromptMode: tree
    userCommandsDir: .pi/commands
    userHooksDir: .pi/hooks
    userAgentsDir: .pi/agents
```

`userPromptMode` is either `tree` (project every Markdown source at its relative path) or `concat` (compile all Markdown sources into `userPromptEntry`). A concat adapter must declare its entry file, for example `userPromptMode: concat` plus `userPromptEntry: AGENTS.md`.

Skill discovery policy remains separate in `resources/skills/catalog.yaml`:

```yaml
version: 1
sources:
  vendor-shared/worktrunk:
    branch: main
    discoveryPriority:
      - agents-marketplace
      - claude-marketplace
      - skills-dir
```

Override the complete resource root with `--resources` or `SKILLS_SWITCH_RESOURCES`. The old pre-release `--sources` surface is intentionally unsupported.

When `defaults.targets` is omitted, shared local and vendor Skills support every registered client. A physical scope directory named after a registered client supports only that client, so adding the `pi` adapter automatically makes `local/pi`, `vendor/pi`, and `archived/pi` valid scopes. Their logical IDs become `local-pi-only`, `vendor-pi-only/*`, and `archived-pi-only/*`. Unknown scope directories fail catalog loading instead of being ignored. Per-Skill overrides remain available for exceptions within a shared source. A new MCP serialization format still requires a Go adapter; Pi intentionally has no built-in MCP adapter because Pi core does not implement MCP.

## Install and build

Install the released binary from the third-party Homebrew tap:

```sh
brew install est7/tap/skills-switch
skills-switch init
```

`skills-switch init` is an idempotent bootstrap. It creates the default `~/.agents/resources` skeleton, initializes `~/.agents` as the catalog Git repository when needed, and registers this repository as `vendor-shared/skills-switch-tui`. Its bundled `skills/skills-switch` operator Skill then becomes discoverable without copying it into user-owned `local/shared`. Re-running `init` preserves existing catalogs and configuration.

For local development, Go 1.25 or newer is required:

```sh
go test ./...
go build -o dist/skills-switch ./cmd/skills-switch
```

## Usage

Every mutating capability is a first-class CLI subcommand grouped under its resource noun — `skills`, `mcp`, `source`, `commands`, `hooks`, `prompt` — alongside the top-level `init`, `status`, `doctor`, `tui`, and `version`. List commands accept `--json` for scripting, and every command accepts `--lang en|zh`; project commands act on the nearest Git root or an explicit `--project`. User-global command, hook, and prompt commands do not require a Git project. The TUI exposes the same mutation surfaces.

A complete end-to-end flow, from an empty machine to enabled resources:

```sh
# 1. install and bootstrap the ~/.agents/resources catalog (idempotent)
brew install est7/tap/skills-switch
skills-switch init

# 2. register an upstream Skill repository — owner/repo shorthand is enough
skills-switch source add vercel-labs/agent-skills

# 3. inspect what was discovered and which clients each Skill supports
skills-switch source list
skills-switch skills list

# 4. enable a Skill (or a whole --source) for this project, across clients atomically
cd ~/my-project
skills-switch skills enable <skill-id> --client claude --client codex

# 5. register and enable a project MCP server
skills-switch mcp import '{"mcpServers":{"context7":{"command":"npx","args":["-y","ctx7"]}}}'
skills-switch mcp enable context7 --client claude

# 6. project user-global files (not project-local)
skills-switch commands enable shared/remember.md --client claude --client codex
skills-switch hooks enable claude-only/audit.sh --client claude
skills-switch prompt enable claude-prompt

# 7. verify the project, or drive the same operations interactively
skills-switch doctor
skills-switch            # opens the TUI in the current Git project
```

Author a local Skill instead of vendoring one, then enable it like any other:

```sh
skills-switch skills create make-goal --description "Draft a project goal."
skills-switch skills enable local-shared/make-goal/make-goal --client claude
```

The sections below detail each surface: vendor source registration and discovery, non-interactive operations, and TUI keys.

## Vendor sources

Add an upstream repository as a submodule tracking `main`. `source add <ref>` accepts an `owner/repo` (or `owner/repo/sub/path`) GitHub shorthand, a `github:`/`gitlab:` prefix, a full web link (including `/tree/<branch>/<path>` and the GitLab `/-/tree/` form), a plain `<repo>.git`, or an scp-style `git@host:owner/repo`. It derives the source name, branch, and Skill subpath, so `--name` is optional when it can be derived and explicit flags override:

```sh
skills-switch source add DannyMac180/skills                                   # owner/repo shorthand
skills-switch source add https://github.com/DannyMac180/skills/tree/main/codex-dynamic-workflows
```

A repository with no manifest and no top-level `skills/` (a curated repo laid out by category, e.g. `github.com/android/skills`) is discovered by root-walking every `SKILL.md`; you then enable the ones you want.

Discovery uses the first available strategy in this default priority:

1. `agents-marketplace` — `.agents/plugins/marketplace.json`
2. `claude-marketplace` — `.claude-plugin/marketplace.json`
3. `codex-plugin` — `.codex-plugin/plugin.json`
4. `claude-plugin` — `.claude-plugin/plugin.json`
5. `skills-dir` — every Skill below the root `skills/` directory

A matched manifest is authoritative. A `skills` array exposes only its declared paths; an explicit empty array exposes no Skills. Marketplace entries resolve their local plugin source and then use that plugin's manifest or conventional `skills/` directory. Lower-priority strategies are not merged into the result.

When a repository has no authoritative manifest, or only a subset of its Skills is wanted, register exact directories with repeatable `--skill-path`. Explicit paths are the complete Skill set for that source and also drive sparse checkout; they cannot be combined with `--discovery-priority`:

```sh
skills-switch source add https://github.com/majiayu000/spellbook.git \
  --name spellbook \
  --skill-path skills/codebase-audit
```

A `--skill-path` may point at a leaf Skill directory (one holding a `SKILL.md`) or a container directory whose `SKILL.md` files live beneath it — a plugin directory such as `plugins/<name>` in a marketplace-of-plugins repo. A container expands to the Skills inside it and scopes discovery and sparse checkout to that subtree, so selecting one or several plugins from a large repo checks out only those:

```sh
skills-switch source add <marketplace-repo-url> \
  --skill-path plugins/android-debug-tools \
  --skill-path plugins/android-ui-tools
```

The default scope is `shared`. Restrict a complete vendor source to a registered client with `--client`; this stores it under the matching physical scope and exposes the corresponding logical ID:

```sh
skills-switch source add https://example.com/pi-tools.git \
  --name pi-tools \
  --client pi
# physical: vendor/pi/pi-tools
# logical:  vendor-pi-only/pi-tools
```

`source add` derives a sparse checkout from the matched manifest and registered Skill paths. Worktrunk therefore checks out its marketplace metadata and registered plugin Skills, without materializing the rest of the working tree:

```sh
skills-switch source add https://github.com/max-sixty/worktrunk.git \
  --name worktrunk \
  --branch main
```

Override the priority when a repository intentionally uses a simpler contract. Emil Kowalski's current repository has no registration manifest, so pinning `skills-dir` checks out its complete `skills/` tree and prevents a future manifest from silently changing discovery:

```sh
skills-switch source add https://github.com/emilkowalski/skills.git \
  --name emilkowalski-skills \
  --discovery-priority skills-dir
```

Matt Pocock's root plugin manifest declares a curated subset of a larger `skills/` tree:

```sh
skills-switch source add https://github.com/mattpocock/skills.git \
  --name mattpocock-skills \
  --discovery-priority claude-plugin \
  --discovery-priority skills-dir
```

Use `--sparse` only for additional non-Skill paths that must remain in the working tree. Manifest metadata and registered Skill paths are derived automatically.

This repository publishes its own `skills-switch` operator Skill through `.agents/plugins/marketplace.json`. Registering the repository makes natural-language Skill, MCP, prompt, and source management workflows available to compatible agents:

```sh
skills-switch source add https://github.com/est7/skills-switch-tui.git \
  --name skills-switch-tui
```

Inspect and explicitly update vendor sources:

```sh
skills-switch source list
skills-switch source update --dry-run
skills-switch source update
skills-switch source update vendor-shared/worktrunk
skills-switch source remove vendor-shared/worktrunk
```

Launching the TUI never updates submodules automatically. Press `u` for the selected vendor source, `U` for every vendor source, or use `source update` without a source ID. Vendor submodules are read-only mirrors: a real update first runs `git reset --hard HEAD` in each selected source, discarding tracked local edits before remote inspection. `--dry-run` remains non-mutating. Batch updates isolate failures per source, so a reset or Git failure in one repository does not block independent repositories. Each reported error includes the source id, repository path, failed operation, and underlying Git detail. After an update, the catalog is rediscovered from the new upstream snapshot; added and removed Skills immediately change each source's enabled/total counts. `source remove` still refuses dirty submodules and updates the gitlink, `.gitmodules`, and catalog policy as one managed operation.

When an update drops a Skill the current project had enabled, its projection is left pointing at a target that no longer exists. `source update` reconciles this automatically: after a successful update it removes such dangling managed links in the current project, scoped to the sources that changed, and reports them (its `--json` output is an object `{updates, pruned}`). Cleanup is best-effort — it never fails the update, and does nothing when the command is not run inside a project. A Skill that is merely no longer discovered but still present on disk is never removed. Run `skills prune` at any time to review these orphaned projections (`--yes` removes them), and `doctor` now reports them as unhealthy.

## Operations

Run the TUI anywhere inside a Git worktree. The nearest Git root receives Skills and MCP changes; Commands, Hooks, Agents, Output Styles, and System Prompts always manage the current user's client directories:

```sh
skills-switch
```

Common non-interactive commands:

```sh
skills-switch skills list
skills-switch skills show vendor-shared/worktrunk/plugins/worktrunk/skills/worktrunk
skills-switch skills create make-goal --description "Draft a goal."
skills-switch status
skills-switch skills enable --source vendor-shared/worktrunk --client codex --client claude
skills-switch skills disable local-shared/make-goal --client codex
skills-switch skills prune            # list projections orphaned by upstream removals; add --yes to remove
skills-switch mcp list
skills-switch mcp enable context7 --client claude --client codex --client gemini
skills-switch mcp disable context7 --client claude --client codex --client gemini
skills-switch mcp import '{"mcpServers":{"context7":{"command":"npx","args":["-y","ctx7"]}}}'
skills-switch commands list
skills-switch commands enable shared/remember.md --client claude --client codex
skills-switch commands disable shared/remember.md --client claude --client codex
skills-switch hooks list
skills-switch hooks enable claude-only/audit.sh --client claude
skills-switch hooks disable claude-only/audit.sh --client claude
skills-switch agents enable codex-only/reviewer.toml --client codex
skills-switch output-styles enable claude-only/tech-mentor.md --client claude
skills-switch prompt list
skills-switch prompt build codex-prompt
skills-switch prompt enable claude-prompt
skills-switch prompt disable claude-prompt
skills-switch doctor
```

Source-group and multi-client changes are preflighted as one transaction. A conflict prevents every selected projection from changing; an apply failure rolls back already-applied links or configuration files.

Skills from different active sources may intentionally share a name. They are alternative providers for the same project link: enabling one atomically switches a link currently owned by another catalog provider. A real directory or a symlink outside the active catalog remains an unmanaged conflict and is never overwritten.

MCP ownership is entry-level. Enabling adds only the selected server; disabling removes it only when the live definition is semantically identical to the catalog definition. Unknown servers, sibling settings, JSONC comments, TOML comments, ordering, and config-file symlinks are preserved. A same-name different definition is an unmanaged conflict and is never overwritten.

Commands and hooks use the same scoped catalog convention: `shared/<path>` supports every client with a registered target directory, while `<client>-only/<path>` supports only that client. Files are recursively projected at the same relative path. Multi-client changes preflight the whole selected set and roll back on apply failure; an unmanaged file or foreign symlink is a conflict and is never overwritten.

Each `<client>-prompt` directory is one atomic user-global group with its own model-specific source files. Do not share or symlink rules between model directories. A `tree` adapter, such as Claude, projects every Markdown file at its relative path. A `concat` adapter, such as Codex, builds the root entry followed by the remaining Markdown files in lexical path order into `~/.agents/generated/system-prompts/<group>/<entry>` and projects only that generated entry. Generated files carry source markers and must not be edited. `prompt build <group>` refreshes a concat output without changing enablement; `prompt enable` builds before linking. Editing a concat source makes an enabled group `stale` until rebuilt; `doctor` reports that state as an issue. Prompt commands do not require a Git project.

Archived sources are hidden by default and reference-only:

```sh
skills-switch skills list --archive
skills-switch source list --archive
```

## TUI keys

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Switch Skills, MCP, Commands, Hooks, Agents, Output Styles, and System Prompts |
| `↑` / `↓`, `j` / `k` | Navigate resource rows |
| `←` / `→`, `h` / `l` | Select a client column |
| `Space` | Toggle the selected resource or Skill source group for the selected client |
| `a` | Toggle the selected Skill, source, MCP server, command, or hook for every compatible client atomically |
| `b` | Build the selected concat system prompt (System Prompts tab only) |
| `n` | New: on Skills, a menu to add a remote repo source or scaffold a local Skill; on MCP, paste a JSON server definition |
| `d` | Delete the selected source, local Skill/group, or MCP server (two-step confirmation) |
| `Enter` | Expand or collapse a Skill source |
| `/` | Search the active resource tab |
| `f` | Cycle all, enabled, issues, and Skill archive views |
| `u` | Update the selected Skill vendor source |
| `U` | Update every vendor source and rediscover its current Skills |
| `L` | Switch the TUI language between English and Chinese |
| `?` | Toggle full help |
| `q` | Quit |

Keyboard actions render immediately and intentionally have no decorative animation. Dark and light themes use separate opaque, full-window terminal canvases; informational text and state colors are contrast-tested against their rendered backgrounds.

## Language

Language selection precedence is `--lang`, `SKILLS_SWITCH_LANG`, then the process locale:

```sh
skills-switch --lang zh
SKILLS_SWITCH_LANG=en skills-switch status
```

Accepted values are `auto`, `en`, and `zh`. JSON field names, client IDs, skill IDs, and state values remain stable English protocol values in every language.

## Release

Tags matching `v*` run the test suite and publish macOS, Linux, and Windows archives with GoReleaser. The `est7/homebrew-tap` repository independently follows the latest stable release and regenerates its Formula without a cross-repository token. Local release validation can be run with:

```sh
goreleaser check
goreleaser release --snapshot --clean
```
