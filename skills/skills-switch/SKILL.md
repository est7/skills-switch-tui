---
name: skills-switch
description: Manage mutually exclusive project or user-global Skills, project MCP servers, commands, and hooks, plus user-global agents, output styles, and system prompts through the skills-switch CLI. Use when the user asks to manage these resources for a project or user account.
---

# Skills Switch

Use `skills-switch` as the only mutation boundary. Let it preserve unmanaged project files, preflight conflicts, and apply multi-client changes atomically. Never replace its operations with direct symlink deletion or manual MCP config editing.

## Resolve the Target

1. Verify the binary before mutation:

   ```bash
   command -v skills-switch
   skills-switch version
   ```

   If the default resource catalog has not been initialized, bootstrap it once:

   ```bash
   skills-switch init
   ```

   This idempotently creates `~/.agents/resources` and registers this repository's bundled operator Skill as `vendor-shared/skills-switch-tui/skills/skills-switch`.

2. For project operations, resolve the intended Git root and pass it explicitly:

   ```bash
   PROJECT="$(git rev-parse --show-toplevel)"
   skills-switch --project "$PROJECT" status --json
   ```

3. Use the default `~/.agents/resources` catalog unless the user explicitly supplies another resource root.
4. Read registered client IDs from `status --json` under `clients[].client`. Never hardcode the current built-in client set; adapters are extensible.
5. Resolve exact source, Skill, MCP, command, hook, and prompt identifiers before mutation:

   ```bash
   skills-switch source list --json
   skills-switch --project "$PROJECT" skills list --json
   skills-switch --project "$PROJECT" mcp list --json
   skills-switch --project "$PROJECT" commands list --json
   skills-switch --project "$PROJECT" hooks list --json
   skills-switch agents list --json
   skills-switch output-styles list --json
   skills-switch prompt list --json
   ```

## Keep Multi-client Changes Atomic

Pass every target client to one command with repeated `--client` flags. Do not loop over separate mutation commands. A single command preflights every projection before changing any of them.

For example, if `status --json` reports `codex`, `claude`, and `gemini`:

```bash
skills-switch --project "$PROJECT" skills enable <skill-id> \
  --client codex --client claude --client gemini
```

Use only compatible clients reported for the selected Skill. For source-level enablement, the CLI filters each source to compatible Skills and rejects a client with no compatible Skills.

## Choose Exactly One Skill Scope

Skills support `--scope project|global`; project is the default. Resolve both views before changing an existing Skill:

```bash
skills-switch --project "$PROJECT" skills list --scope project --json
skills-switch --project "$PROJECT" skills list --scope global --json
```

The scopes are mutually exclusive by final Skill name for each client. A project state of `global` is locked and must not be enabled at project scope. To make an always-on Skill global, use one global command; it atomically retires any catalog-managed project link and creates the native user-global link:

```bash
skills-switch --project "$PROJECT" skills enable <skill-id> \
  --client <client-1> --client <client-2> --scope global
```

To return it to project ownership, disable global first, then enable project. Never create both links manually; `doctor` reports that historical state as `duplicate`.

## Add a GitHub Skill Repository

Interpret “add/register this GitHub Skill” as catalog source registration. Interpret “for all clients” as shared source registration plus project enablement for every compatible registered client.

### Normalize the reference and name

`source add <ref>` derives the clone URL, source name, branch, and (for a tree/blob link or an `owner/repo/subpath` shorthand) the Skill subpath from any of these forms:

- `owner/repo` and `owner/repo/sub/path` — GitHub shorthand
- `github:owner/repo`, `gitlab:owner/repo` — host prefix
- `https://github.com/<owner>/<repo>/tree/<branch>/<path>` (or `/blob/`), and the GitLab `/-/tree/` form
- a plain repository URL, `<repo>.git`, or an scp-style `git@host:owner/repo`

So `--name` is optional when it can be derived — a tree link or `owner/repo/plugins/<x>` registers `<repo>` with Skill subtree `<x>` on its own. Pass `--name`, `--branch`, or `--skill-path` only to override a derived value, and always pass `--name` for an input the parser cannot resolve (it fails with a clear "source name is required" error rather than guessing). Local filesystem paths are not accepted — author local Skills with `skills create`.

### Prefer automatic discovery — do not hand-list paths

`source add` **without** `--skill-path` auto-discovers the repository's Skills by walking its manifests. This is the default and correct path for any repo that ships a plugin manifest or a top-level `skills/` directory. Do not enumerate `--skill-path` for such a repo; a plain add reads its manifest and checks out exactly the declared Skills.

```bash
skills-switch source add https://github.com/<owner>/<repo>.git \
  --name <repo> --branch <branch>          # --branch defaults to "main"
```

Discovery tries these strategies in priority order and stops at the first that matches the repo root:

| strategy | matches when the root has | checks out |
|---|---|---|
| `agents-marketplace` | `.agents/plugins/marketplace.json` | each listed plugin's `.codex-plugin/plugin.json` skills |
| `claude-marketplace` | `.claude-plugin/marketplace.json` | each listed plugin's `.claude-plugin/plugin.json` skills |
| `codex-plugin` | `.codex-plugin/plugin.json` | that manifest's `skills` array |
| `claude-plugin` | `.claude-plugin/plugin.json` | that manifest's `skills` array |
| `skills-dir` | a top-level `skills/` directory | every Skill under `skills/` |

A `plugin.json` `skills` field may be an array of paths (checked out exactly) or a string pointing at a skills directory (e.g. `"./skills/"`); a `plugin.json` with no `skills` field, or a marketplace plugin that has no manifest, falls back to that plugin's `skills/` directory. When a repo has **no manifest and no top-level `skills/`** (a curated repo of skills laid out by category, like `github.com/android/skills`), discovery **root-walks the whole repo** and registers every `SKILL.md` — so `source add <owner>/<repo>` on such a repo discovers all of them and you enable the ones you want. A skill whose frontmatter is not strictly valid YAML (a common unquoted `:` in the description) is still read, not dropped.

Restrict or reorder the chain with `--discovery-priority` (repeatable strategy names from the table). For example, force the top-level `skills/` tree and ignore any manifest:

```bash
skills-switch source add <url> --name <repo> --discovery-priority skills-dir
```

`--discovery-priority` and `--skill-path` are mutually exclusive; passing both is refused.

### Use --skill-path to scope, including inside a marketplace repo

Pass `--skill-path` (authoritative, repeatable) when:

- the user names a specific Skill subtree, or
- the repo has no manifest and no top-level `skills/` directory, or
- you must register a strict subset of the discoverable Skills, or
- the user wants one plugin (or a few) out of a **marketplace-of-plugins** repo.

Each `--skill-path` may point at either a **leaf Skill directory** (one that directly contains a `SKILL.md`) or a **container directory** whose `SKILL.md` files live beneath it — for example a plugin directory like `plugins/<name>` in a repo that nests many plugins under `plugins/`. A container is walked and expands to the Skills inside it; only a subtree that contains no `SKILL.md` at all is rejected. Discovery and sparse checkout are scoped to exactly the listed paths, so unlisted plugins are neither registered nor checked out. Use `--sparse` (repeatable) for extra checkout paths a Skill depends on but that are not themselves Skill roots.

```bash
# a single plugin from a marketplace repo (its Skills expand automatically)
skills-switch source add <url> --skill-path plugins/android-debug-tools

# select several plugins into one source, still one clone
skills-switch source add <url> \
  --skill-path plugins/android-debug-tools \
  --skill-path plugins/android-ui-tools
```

One repository is one vendor source (a single git submodule), so plugins selected from the same repo share one source id; their Skill ids carry the `plugins/<name>/...` path that distinguishes them, and each Skill still enables/disables independently. `--name` defaults to the repository name; pass it to override.

Restrict the whole source to a single client with `--client <client>`; on `source add` this flag is single-valued (one client per source), unlike the repeatable `--client` on `skills enable`/`skills disable`.

### Confirm and enable

Re-run `source list --json` and `skills list --json`. Find the new source ID (normally `vendor-shared/<repo>`), confirm the recorded `discoveryStrategy`, and read its discovered Skill IDs. If immediate use was requested, enable the Skill or the entire source for every compatible registered client in one atomic command:

```bash
skills-switch --project "$PROJECT" skills enable \
  --source vendor-shared/<repo> \
  --client <client-1> --client <client-2>
```

## Disable or Re-enable Project Skills

Treat “disable/remove this Skill from this project” as projection removal, not source deletion. Preserve the SSOT in the catalog.

```bash
skills-switch --project "$PROJECT" skills disable <skill-id> \
  --client <client-1> --client <client-2>
```

Operate on a whole source when requested:

```bash
skills-switch --project "$PROJECT" skills disable \
  --source <source-id> \
  --client <client-1> --client <client-2>
```

Use `skills enable` with the same argument shape to re-enable it. If the user says “this project” without naming a client, target all currently registered compatible clients in one command.

## Manage Project MCP Servers

Resolve the server name with `mcp list --json`, then mutate all requested clients atomically:

```bash
skills-switch --project "$PROJECT" mcp disable <server> \
  --client <client-1> --client <client-2>

skills-switch --project "$PROJECT" mcp enable <server> \
  --client <client-1> --client <client-2>
```

Do not edit `.mcp.json`, `.codex/config.toml`, or another client config directly. The CLI appends or removes only its managed server entry and preserves unrelated project configuration.

Register a new server definition in the catalog SSOT. Transport is inferred from whether you pass a command or a URL; pass `--transport` only to override:

```bash
# stdio server
skills-switch mcp add <server> \
  --command <exe> --arg <a1> --arg <a2> --env API_TOKEN='${API_TOKEN}' --cwd <dir>

# http server
skills-switch mcp add <server> --url https://<host>/mcp --header 'Authorization=${MCP_AUTHORIZATION}'
```

When the user pastes a client-style JSON block, register it with `mcp import` instead of translating it into flags. It accepts either a full `{"mcpServers": {...}}` wrapper (names come from the keys; several servers are validated and added in one catalog transaction) or a single bare object such as `{"command": "npx", ...}` / `{"url": "..."}` (supply the name with `--name`). Read the JSON from an argument, `--file`, or standard input. Never store plaintext credentials in secret-like environment or header fields; new definitions must use `${ENV_VAR}` references.

```bash
# full wrapper (names from keys)
skills-switch mcp import '{"mcpServers":{"context7":{"command":"npx","args":["-y","ctx7"]}}}'

# bare object needs a name
skills-switch mcp import '{"url":"https://mcp.example.com"}' --name grafana

# from a file or stdin
skills-switch mcp import --file servers.json
pbpaste | skills-switch mcp import
```

Remove a server definition from the catalog. This first clears its enabled projection from every registered client, then deletes the catalog entry. Adding is catalog-level, so it takes no `--client`; removing cleans the current `--project`:

```bash
skills-switch --project "$PROJECT" mcp remove <server>
```

Interpret “add/register this MCP server” as `mcp add` (catalog definition) and “enable it here” as a subsequent `mcp enable ... --client`. Interpret “remove this MCP server” as `mcp remove` (deletes the definition); interpret “turn it off here” as `mcp disable` (projection only).

## Update, Query, or Remove Sources

Preview and then update one vendor source:

```bash
skills-switch --project "$PROJECT" source update <source-id> --dry-run
skills-switch --project "$PROJECT" source update <source-id>
```

When a project is in scope, pass `--project "$PROJECT"` to `source update` and `source remove` so project reconciliation or projection retirement cannot attach to an unrelated working directory. Omit it only when the operation intentionally has no project scope.

Update every vendor source by omitting the source ID. Each checkout is a read-only mirror: before remote inspection, a real update runs `git reset --hard HEAD` and `git clean -ffdx`, discarding tracked, untracked, and ignored local changes. It initializes a missing registered checkout, reads the exact configured branch tip, fetches that ref, resets to the advertised SHA, and verifies `HEAD`. `--dry-run` is non-mutating. Failures are isolated per source and identify the source, path, operation, and underlying Git error.

After changed sources are rediscovered, the command removes dangling catalog-managed Skill links from the current project scope (when available) and user-global scope. Reconciliation failures are command failures, not warnings. JSON output contains `updates`, scope-bearing `pruned` links, and structured `failures`.

Remove a vendor repository only when the user explicitly asks to delete the repository source, not merely disable a project Skill:

```bash
skills-switch --project "$PROJECT" source remove <source-id>
```

This first retires catalog-managed projections in the current project and user-global scopes, then removes the clean vendor submodule and its catalog policy. If Git or catalog removal fails, the retired projections are restored. Report that projects outside the current one may still reference the source and must be handled in their own scope. Never force-remove a dirty source.

## Delete Local Skills or Groups

Local sources are authored in place, not vendored. Each directory under `local/<scope>/` is a distinct group source (for example `local-shared/core`), and the Skills inside it carry ids like `local-shared/core/<skill>`. Deletion here removes files from the resource SSOT, so it is destructive and irreversible; it is separate from `disable`, which only removes a project projection.

Choose the operation by ownership:

- Vendor source (a submodule): `source remove <source-id>`.
- Local group or a single local Skill: `skills delete <id> --yes`.

```bash
# resolve ids first
skills-switch skills list --json

# delete one local Skill (keeps its group and siblings)
skills-switch --project "$PROJECT" skills delete local-<scope>/<group>/<skill> --yes

# delete an entire local group
skills-switch --project "$PROJECT" skills delete local-<scope>/<group> --yes
```

`skills delete` refuses to run without `--yes`, and rejects vendor sources (use `source remove`), read-only vendor Skills, archived references, and unknown ids. It first clears the target's project projections and every supported user-global projection across all registered clients, then removes the directory. Confirm the exact user intent before deleting, since the Skill source cannot be restored except from version control.

## Scaffold a Local Skill

Interpret “create/scaffold a new local skill” as `skills create <name>`. It writes a minimal discoverable `SKILL.md` skeleton (frontmatter `name` + `description`, placeholder body) under the resource SSOT and does not touch the project until the user enables it. By default the Skill becomes its own standalone group at `skills/local/shared/<name>/SKILL.md` (id `local-shared/<name>/<name>`); pass `--group` to nest it under an existing or new group, `--scope` to target a client-only scope, and `--description` to fill the frontmatter:

```bash
skills-switch skills create <name> --description "<one line>"
skills-switch skills create <name> --group <group> --scope <client>
```

It refuses to overwrite an existing Skill and rejects invalid names. After scaffolding, the user edits the generated `SKILL.md`; enable it into a project with `skills enable` like any other Skill.

## Manage User-global System Prompts

System prompt operations are user-global. Skills have an explicit project/global scope; MCP servers, commands, and hooks are project-scoped:

```bash
skills-switch prompt list --json
skills-switch prompt build <concat-group>
skills-switch prompt enable <group>
skills-switch prompt disable <group>
```

Prompt groups are model-owned source trees. Never copy or symlink rules between different model groups. Read `mode` from `prompt list --json`: `tree` projects every Markdown file at its relative path, while `concat` compiles the entry file and remaining Markdown files in lexical path order into the managed generated directory, then projects only that generated entry. Use `prompt build` to refresh a concat output without changing enablement. `prompt enable` builds first. Treat `stale` as an issue requiring a rebuild; never edit a generated prompt directly. State the user-global scope explicitly before mutation when the request could be mistaken for project-local behavior.

## Manage Commands, Hooks, Agents, and Output Styles

Commands and hooks are catalog files under `shared/<path>` or `<client>-only/<path>`. Resolve their logical IDs first, then mutate every requested compatible client in one command:

```bash
skills-switch --project "$PROJECT" commands list --json
skills-switch --project "$PROJECT" commands enable shared/<path> --client <client-1> --client <client-2>
skills-switch --project "$PROJECT" commands disable shared/<path> --client <client-1> --client <client-2>

skills-switch --project "$PROJECT" hooks list --json
skills-switch --project "$PROJECT" hooks enable <client>-only/<path> --client <client>
skills-switch --project "$PROJECT" hooks disable <client>-only/<path> --client <client>

skills-switch agents enable <client>-only/<path> --client <client>
skills-switch output-styles enable claude-only/<path> --client claude
```

Commands and hooks are project-scoped. Agents and output styles are user-global; agents are model-specific and output styles currently have only a Claude adapter. All preserve unmanaged files. Do not create or remove target symlinks manually.

## Verify Every Mutation

Run the nearest read command after each change:

- Skill or source projection: `skills-switch --project "$PROJECT" skills list --json`
- Local Skill scaffold or deletion: `skills-switch skills list --json` (the id appears or is gone)
- MCP server projection or definition: `skills-switch --project "$PROJECT" mcp list --json`
- Project command: `skills-switch --project "$PROJECT" commands list --json`
- Project hook: `skills-switch --project "$PROJECT" hooks list --json`
- User-global agent: `skills-switch agents list --json`
- User-global output style: `skills-switch output-styles list --json`
- Source repository: `skills-switch source list --json`
- System prompt: `skills-switch prompt list --json` (enabled concat prompts must not be `stale`)

Finish project mutations with:

```bash
skills-switch --project "$PROJECT" doctor --json
```

Treat vendor sources as read-only mirrors. Before remote inspection, a real source update discards tracked, untracked, and ignored local changes with `git reset --hard HEAD` plus `git clean -ffdx`; `--dry-run` never mutates them. In a batch update, isolate reset, clean, and other Git failures to the affected source, allow independent sources to finish, and report every Git or reconciliation error exactly. Treat conflicts, broken links, incompatible projections, malformed manifests, and unknown identifiers as hard stops for the affected resource.
