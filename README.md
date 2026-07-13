# skills-switch

`skills-switch` manages project-local Agent Skills as symlink projections from a central source catalog. It keeps project selection state in the filesystem itself: there is no per-project manifest to drift away from the links.

The TUI is built with Bubble Tea v2, Bubbles v2, and Lip Gloss v2. Human-facing CLI and TUI text supports English and Simplified Chinese.

## Model

The default source root is `~/.agents/sources`:

```text
~/.agents/
└── sources/
    ├── catalog.yaml
    ├── local/       # skills you own
    ├── vendor/      # upstream repositories as git submodules
    └── archive/     # reference-only skills; never enabled
```

The built-in client registry projects enabled skills into these project directories:

| Client | Project directory |
| --- | --- |
| `codex` | `.agents/skills` |
| `claude` | `.claude/skills` |
| `gemini` | `.gemini/skills` |

Clients are data, not a closed enum. Add another client without changing Go code:

```yaml
version: 1

clients:
  pi:
    projectSkillsDir: .pi/skills

sources:
  vendor/worktrunk:
    branch: main
    discoveryPriority:
      - agents-marketplace
      - claude-marketplace
      - skills-dir

overrides:
  local/codex-dynamic-workflows:
    targets:
      - codex
    reason: Uses Codex-only workflow APIs.
```

When `defaults.targets` is omitted, new skills support every registered client. An override narrows exceptional skills to the clients they actually support.

## Install and build

Go 1.25 or newer is required.

```sh
go install github.com/est7/skills-switch-tui/cmd/skills-switch@latest
```

For local development:

```sh
go test ./...
go build -o dist/skills-switch ./cmd/skills-switch
```

## Vendor sources

Add an upstream repository as a submodule tracking `main`. Discovery uses the first available strategy in this default priority:

1. `agents-marketplace` — `.agents/plugins/marketplace.json`
2. `claude-marketplace` — `.claude-plugin/marketplace.json`
3. `codex-plugin` — `.codex-plugin/plugin.json`
4. `claude-plugin` — `.claude-plugin/plugin.json`
5. `skills-dir` — every Skill below the root `skills/` directory

A matched manifest is authoritative. A `skills` array exposes only its declared paths; an explicit empty array exposes no Skills. Marketplace entries resolve their local plugin source and then use that plugin's manifest or conventional `skills/` directory. Lower-priority strategies are not merged into the result.

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

Inspect and explicitly update vendor sources:

```sh
skills-switch source list
skills-switch update --dry-run
skills-switch update vendor/worktrunk
```

Launching the TUI never updates submodules. Press `u` on a vendor source, or use `update`, when an update is intended.

## Project operations

Run the TUI anywhere inside a Git worktree; the nearest Git root is the managed project:

```sh
skills-switch
```

Common non-interactive commands:

```sh
skills-switch list
skills-switch show vendor/worktrunk/plugins/worktrunk/skills/worktrunk
skills-switch status
skills-switch enable --source vendor/worktrunk --client codex --client claude
skills-switch disable local/codex-dynamic-workflows --client codex
skills-switch doctor
```

Source-group and multi-client changes are preflighted as one transaction. A conflict prevents every selected projection from changing; an apply failure rolls back already-applied links.

Archived sources are hidden by default and reference-only:

```sh
skills-switch list --archive
skills-switch source list --archive
```

## TUI keys

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | Navigate source and skill rows |
| `←` / `→`, `h` / `l` | Select a client column |
| `Space` | Toggle the selected skill or entire source group atomically |
| `Enter` | Expand or collapse a source |
| `/` | Search sources and skills |
| `Tab` / `Shift+Tab` | Cycle all, enabled, issues, and archive views |
| `u` | Update the selected vendor source |
| `?` | Toggle full help |
| `q` | Quit |

Keyboard actions render immediately and intentionally have no decorative animation.

## Language

Language selection precedence is `--lang`, `SKILLS_SWITCH_LANG`, then the process locale:

```sh
skills-switch --lang zh
SKILLS_SWITCH_LANG=en skills-switch status
```

Accepted values are `auto`, `en`, and `zh`. JSON field names, client IDs, skill IDs, and state values remain stable English protocol values in every language.

## Release

Tags matching `v*` run the test suite and publish macOS, Linux, and Windows archives with GoReleaser. Local release validation can be run with:

```sh
goreleaser check
goreleaser release --snapshot --clean
```
