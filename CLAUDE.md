# CLAUDE.md

## Repository

`skills-switch` is a Go CLI and Bubble Tea TUI for managing:

- mutually exclusive project or user-global Agent Skill projections;
- project-local MCP server entries, commands, and hooks;
- user-global agents, output styles, and system prompt projections;
- Git-submodule-backed Skill source repositories.

Repository path: `/Users/est9/EstProjects/skills-switch-tui`

Related paths:

- resource SSOT: `/Users/est9/.agents/resources`
- registered self source: `/Users/est9/.agents/resources/skills/vendor/shared/skills-switch-tui`
- Homebrew tap: `/Users/est9/EstProjects/homebrew-tap`
- bundled operator Skill: `skills/skills-switch/SKILL.md`
- marketplace registration: `.agents/plugins/marketplace.json`

## Architecture Order

CLI subcommands are the first-class interface. Every mutating capability lands in this order:

1. Core package operation (`internal/catalog`, `internal/mcp`, `internal/projection`, `internal/source`, `internal/userresource`, `internal/systemprompt`) — the single source of truth for the mutation and its invariants.
2. A first-class CLI subcommand exposing it (human + `--json`), grouped under its resource noun (`skills`, `mcp`, `source`, `commands`, `hooks`, `agents`, `output-styles`, `prompt`).
3. Any UI (the TUI) drives the same core operation — it never reimplements mutation logic.

Build the CLI path first, then wrap it in UI. A capability that exists only in the TUI is incomplete: agents drive this tool through the CLI and cannot use interactive keys.

## Product Invariants

- Skills support mutually exclusive project and user-global scopes. MCP servers, commands, and hooks are project-local; agents, output styles, and system prompts are user-global.
- `~/.agents/resources` is the resource SSOT. Do not add a per-project manifest.
- Mutations append or remove only managed entries. Preserve unrelated files, symlinks, MCP entries, comments, and ordering.
- Apply multi-client changes atomically: preflight every client before changing any client.
- Treat vendor checkouts as disposable read-only mirrors. A real update resets tracked changes, cleans untracked and ignored files, and checks out the exact configured remote branch SHA before discovery.
- Treat registered clients as data from `resources/registry.yaml`, not a closed enum.
- Treat file-resource kind metadata as data from `internal/userresource.Descriptor`, not duplicated CLI/TUI switches.
- Keep `local`, `archived`, and `vendor` source ownership distinct. Vendor repositories remain Git submodules.
- Do not copy the bundled `skills-switch` Skill into `local/shared`; `skills-switch init` registers this repository as a vendor source.
- Keep English and Simplified Chinese CLI/TUI strings in sync.
- High-frequency keyboard actions must render immediately without decorative animation.

## Code Map

- `cmd/skills-switch`: executable entry point and build-time version variable.
- `internal/cli`: Cobra commands and human/JSON output.
- `internal/bootstrap`: idempotent `skills-switch init` orchestration.
- `internal/catalog`: Skill discovery, manifest priority, and catalog policy.
- `internal/client`: extensible client adapter registry.
- `internal/projection`: project/global Skill projection policy, mutual exclusion, retirement, and orphan reconciliation.
- `internal/linktransaction`: common validated, reversible symlink transaction engine.
- `internal/linkprojection`: file-projection planning adapter over link transactions.
- `internal/mcp`: MCP catalog and surgical client-config mutations.
- `internal/userresource`: descriptor-driven command, hook, agent, and output-style discovery and projection.
- `internal/systemprompt`: user-global prompt discovery, concat builds, and projection.
- `internal/source`: vendor Git mechanics plus cross-catalog project/global projection lifecycle.
- `internal/filelock`: inter-process locks for persistent catalog and configuration mutations.
- `internal/tui`: Bubble Tea model, keyboard behavior, Lip Gloss themes, and rendering.
- `internal/i18n`: shared English and Chinese messages.

## Development

Require Go 1.25 or newer.

```bash
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -o /tmp/skills-switch ./cmd/skills-switch
```

Run against the real catalog without mutating it:

```bash
/tmp/skills-switch \
  --resources /Users/est9/.agents/resources \
  --project /Users/est9/EstProjects/skills-switch-tui \
  tui
```

The TUI uses `Space` for the selected client; `a` for every compatible client; `s` for project/global Skill scope; `u`/`U` to update one/all vendor sources; `b` to build the selected concat prompt; `n` for the context-appropriate "new" dialog (add repo / create local Skill / paste MCP JSON, built on `charm.land/huh/v2`); `d` to delete with confirmation; and `L` to switch between English and Chinese.

Validate the bundled Skill and marketplace after changing either:

```bash
uv run --no-project --with pyyaml \
  python /Users/est9/.codex/skills/.system/skill-creator/scripts/quick_validate.py \
  skills/skills-switch
jq empty .agents/plugins/marketplace.json
```

## Versioning

There is no version file to edit. `cmd/skills-switch/main.go` defaults to `dev`; GoReleaser injects the release version with:

```text
-X main.version={{ .Version }}
```

Therefore, “bump version” means choosing and pushing the next SemVer Git tag:

- patch: compatible bug fix, for example `v0.3.0` to `v0.3.1`;
- minor: backward-compatible feature, for example `v0.3.0` to `v0.4.0`;
- major: breaking public CLI, config, or behavior contract.

Never edit generated Homebrew checksums by hand. The tap workflow renders them from the published release.

## Release

Release only from a clean `main` that has been pushed to `origin`.

1. Run repository gates:

   ```bash
   go test ./...
   go vet ./...
   go test -race ./...
   git diff --check
   ```

2. Validate GoReleaser and all target archives:

   ```bash
   go run github.com/goreleaser/goreleaser/v2@v2.17.0 check
   go run github.com/goreleaser/goreleaser/v2@v2.17.0 \
     release --snapshot --clean
   ```

3. Create and push the chosen tag:

   ```bash
   VERSION=v0.4.0
   git tag -a "$VERSION" -m "$VERSION"
   git push origin "$VERSION"
   ```

4. Watch the release workflow and verify the published assets:

   ```bash
   gh run list --workflow release.yml --limit 3
   gh run watch <release-run-id> --exit-status
   gh release view "$VERSION"
   ```

   A complete release contains `checksums.txt` plus macOS, Linux, and Windows archives for both `amd64` and `arm64`.

5. Update the third-party Homebrew tap immediately instead of waiting for its hourly schedule:

   ```bash
   gh workflow run update-skills-switch.yml \
     -R est7/homebrew-tap \
     -f tag="$VERSION"
   gh run list \
     -R est7/homebrew-tap \
     --workflow update-skills-switch.yml \
     --limit 3
   gh run watch <tap-run-id> -R est7/homebrew-tap --exit-status
   ```

6. Refresh the local tap and verify the shipped binary:

   ```bash
   git -C /Users/est9/EstProjects/homebrew-tap pull --ff-only
   brew upgrade est7/tap/skills-switch
   brew test est7/tap/skills-switch
   skills-switch version
   skills-switch init --json
   ```

7. Verify that the self-hosted operator Skill remains discoverable:

   ```bash
   skills-switch source list --json | \
     jq '.[] | select(.id == "vendor-shared/skills-switch-tui")'
   skills-switch \
     --project /Users/est9/EstProjects/skills-switch-tui \
     skills list --json | \
     jq '.skills[] | select(.id == "vendor-shared/skills-switch-tui/skills/skills-switch")'
   ```

## Git Scope

Before committing, inspect the worktree and stage only files owned by this task. Use English Conventional Commit messages. Do not include generated `dist/` artifacts. When the repository is updated after a release, update the registered vendor gitlink through `skills-switch source update vendor-shared/skills-switch-tui`; do not edit the submodule checkout directly.
