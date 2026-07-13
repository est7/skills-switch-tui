---
name: skills-switch
description: Manage Agent Skill source repositories, project-level Skill projections, project-level MCP servers, and user-global system prompts through the skills-switch CLI. Use when the user asks to add, register, update, query, or remove a GitHub Skill repository; enable or disable a Skill or source for one or all registered clients in the current project; enable or disable a project MCP server; or manage a client system prompt. Trigger on requests such as "add this GitHub skill for all clients", "delete this skill repo", "disable this skill in this project", "turn off this MCP here", "更新 skills", or equivalent Chinese requests.
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

Interpret “add/register this GitHub Skill” as catalog registration. Interpret “for all clients” as both shared source registration and project enablement for all compatible registered clients.

1. Normalize a GitHub tree or blob URL to the repository clone URL. Convert:

   ```text
   https://github.com/<owner>/<repo>/tree/<branch>/<path>
   ```

   into repository URL `https://github.com/<owner>/<repo>.git`, branch `<branch>`, and Skill directory `<path>`.

2. Choose a stable lowercase source name, normally the repository name.
3. Prefer the repository's registered manifest paths. Add `--skill-path` only when the user names a specific directory or the repository has no trustworthy manifest. A `--skill-path` is authoritative and repeatable.
4. Register a shared source:

   ```bash
   skills-switch source add https://github.com/<owner>/<repo>.git \
     --name <repo> \
     --branch <branch>
   ```

   For an explicit Skill subtree:

   ```bash
   skills-switch source add https://github.com/<owner>/<repo>.git \
     --name <repo> \
     --branch <branch> \
     --skill-path <path/to/skill>
   ```

5. Re-run `source list --json` and `list --json`. Find the exact new source ID, normally `vendor-shared/<repo>`, and its discovered Skill IDs.
6. If the user requested immediate use in this project, enable the Skill or entire source with all compatible registered clients in one command:

   ```bash
   skills-switch --project "$PROJECT" enable \
     --source vendor-shared/<repo> \
     --client <client-1> --client <client-2>
   ```

If the user requests one client only, register the entire source with `source add ... --client <client>` or pass only that client during enablement, according to whether the repository itself is client-specific.

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
- MCP server: `skills-switch --project "$PROJECT" mcp list --json`
- Source repository: `skills-switch source list --json`
- System prompt: `skills-switch prompt list --json`

Finish project mutations with:

```bash
skills-switch --project "$PROJECT" doctor --json
```

Treat a conflict, broken link, incompatible projection, dirty vendor, malformed manifest, or unknown identifier as a hard stop. Report the exact CLI error and preserve the existing state.
