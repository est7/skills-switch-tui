---
name: skills-switch
description: Manage Agent Skill source repositories, project-level Skill projections, project-level MCP servers, and user-global system prompts through the skills-switch CLI. Use when the user asks to add, register, update, query, or remove a GitHub Skill repository; enable or disable a Skill or source for one or all registered clients in the current project; add, remove, enable, or disable a project MCP server; delete a local Skill or group from the resource catalog; or manage a client system prompt. Trigger on requests such as "add this GitHub skill for all clients", "delete this skill repo", "disable this skill in this project", "delete this local skill", "add this MCP server", "remove this MCP server", "turn off this MCP here", "更新 skills", or equivalent Chinese requests.
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
5. Resolve exact source, Skill, and MCP identifiers before mutation:

   ```bash
   skills-switch source list --json
   skills-switch --project "$PROJECT" list --json
   skills-switch --project "$PROJECT" mcp list --json
   ```

## Keep Multi-client Changes Atomic

Pass every target client to one command with repeated `--client` flags. Do not loop over separate mutation commands. A single command preflights every projection before changing any of them.

For example, if `status --json` reports `codex`, `claude`, and `gemini`:

```bash
skills-switch --project "$PROJECT" enable <skill-id> \
  --client codex --client claude --client gemini
```

Use only compatible clients reported for the selected Skill. For source-level enablement, the CLI filters each source to compatible Skills and rejects a client with no compatible Skills.

## Add a GitHub Skill Repository

Interpret “add/register this GitHub Skill” as catalog source registration. Interpret “for all clients” as shared source registration plus project enablement for every compatible registered client.

### Normalize the URL and name

Normalize a GitHub tree or blob URL to the clone URL. Convert `https://github.com/<owner>/<repo>/tree/<branch>/<path>` into repository URL `https://github.com/<owner>/<repo>.git`, branch `<branch>`, and — only if the user pointed at a specific subtree — Skill directory `<path>`. Choose a stable lowercase source name, normally the repository name.

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

A `plugin.json` whose `skills` array is present checks out exactly those directories. A `plugin.json` with no `skills` field, or a marketplace plugin that has no manifest, falls back to that plugin's `skills/` directory.

Restrict or reorder the chain with `--discovery-priority` (repeatable strategy names from the table). For example, force the top-level `skills/` tree and ignore any manifest:

```bash
skills-switch source add <url> --name <repo> --discovery-priority skills-dir
```

`--discovery-priority` and `--skill-path` are mutually exclusive; passing both is refused.

### Use --skill-path only as an escape hatch

Pass `--skill-path` (authoritative, repeatable) ONLY when:

- the user names a specific Skill subtree, or
- the repo has no manifest and no top-level `skills/` directory, or
- you must register a strict subset of the discoverable Skills.

Each `--skill-path` must point at a directory that **directly contains a `SKILL.md`**; a parent directory holding several Skills is rejected (`does not contain SKILL.md`). Use `--sparse` (repeatable) for extra checkout paths a Skill depends on but that are not themselves Skill roots.

```bash
skills-switch source add <url> --name <repo> --branch <branch> \
  --skill-path path/to/one-skill --skill-path path/to/another-skill
```

Restrict the whole source to a single client with `--client <client>`; on `source add` this flag is single-valued (one client per source), unlike the repeatable `--client` on `enable`/`disable`.

### Confirm and enable

Re-run `source list --json` and `list --json`. Find the new source ID (normally `vendor-shared/<repo>`), confirm the recorded `discoveryStrategy`, and read its discovered Skill IDs. If immediate use was requested, enable the Skill or the entire source for every compatible registered client in one atomic command:

```bash
skills-switch --project "$PROJECT" enable \
  --source vendor-shared/<repo> \
  --client <client-1> --client <client-2>
```

## Disable or Re-enable Project Skills

Treat “disable/remove this Skill from this project” as projection removal, not source deletion. Preserve the SSOT in the catalog.

```bash
skills-switch --project "$PROJECT" disable <skill-id> \
  --client <client-1> --client <client-2>
```

Operate on a whole source when requested:

```bash
skills-switch --project "$PROJECT" disable \
  --source <source-id> \
  --client <client-1> --client <client-2>
```

Use `enable` with the same argument shape to re-enable it. If the user says “this project” without naming a client, target all currently registered compatible clients in one command.

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
  --command <exe> --arg <a1> --arg <a2> --env KEY=VALUE --cwd <dir>

# http server
skills-switch mcp add <server> --url https://<host>/mcp --header KEY=VALUE
```

Remove a server definition from the catalog. This first clears its enabled projection from every registered client, then deletes the catalog entry. Adding is catalog-level, so it takes no `--client`; removing cleans the current `--project`:

```bash
skills-switch --project "$PROJECT" mcp remove <server>
```

Interpret “add/register this MCP server” as `mcp add` (catalog definition) and “enable it here” as a subsequent `mcp enable ... --client`. Interpret “remove this MCP server” as `mcp remove` (deletes the definition); interpret “turn it off here” as `mcp disable` (projection only).

## Update, Query, or Remove Sources

Preview and then update one vendor source:

```bash
skills-switch update <source-id> --dry-run
skills-switch update <source-id>
```

Update every clean vendor source by omitting the source ID. The CLI follows each submodule's tracked branch and stops before mutation when a selected source is dirty.

Remove a vendor repository only when the user explicitly asks to delete the repository source, not merely disable a project Skill:

```bash
skills-switch source remove <source-id>
```

This removes the clean vendor submodule and its catalog policy. Before removal, report that projects outside the current one may still reference the source; disable known projections first when the user includes them in scope. Never force-remove a dirty source.

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

`skills delete` refuses to run without `--yes`, and rejects vendor sources (use `source remove`), read-only vendor Skills, archived references, and unknown ids. It first clears the target's projections across every registered client in `--project`, then removes the directory. Confirm the exact user intent before deleting, since the Skill source cannot be restored except from version control.

## Manage User-global System Prompts

System prompt operations are user-global, unlike Skills and MCP servers:

```bash
skills-switch prompt list --json
skills-switch prompt enable <group>
skills-switch prompt disable <group>
```

State that scope explicitly before mutation when the request could be mistaken for project-local behavior.

## Verify Every Mutation

Run the nearest read command after each change:

- Skill or source projection: `skills-switch --project "$PROJECT" list --json`
- Local Skill or group deletion: `skills-switch skills list --json` (the id is gone)
- MCP server projection or definition: `skills-switch --project "$PROJECT" mcp list --json`
- Source repository: `skills-switch source list --json`
- System prompt: `skills-switch prompt list --json`

Finish project mutations with:

```bash
skills-switch --project "$PROJECT" doctor --json
```

Treat a conflict, broken link, incompatible projection, dirty vendor, malformed manifest, or unknown identifier as a hard stop. Report the exact CLI error and preserve the existing state.
