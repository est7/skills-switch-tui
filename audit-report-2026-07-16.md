# skills-switch-tui Codebase Audit Report

> Date: 2026-07-16
> Target: `/Users/est9/EstProjects/skills-switch-tui`
> Stack: Go 1.24, Cobra, Bubble Tea, Git submodules, filesystem projections
> Mode: full
> Dimensions: API Contract & Data Integrity; Error Handling & Security; Architecture & Code Quality; Config & Persistence
> Dependency audit: 依赖审计降级跳过: `govulncheck`
> Previous audit: none

## Summary

| Level | Count | Verified | Key Areas |
|---|---:|---:|---|
| Critical | 0 | 0 | — |
| High / P1 | 9 | 9 | source lifecycle, read-only vendor integrity, client capabilities, persistence concurrency |
| Medium / P2 | 18 | 0 | diagnostics, JSON contracts, transaction depth, extension cost, crash consistency |

Baseline validation:

- `go test ./...`: 197 passed in 15 packages
- `go test -race ./...`: passed
- `go vet ./...`: passed
- `staticcheck`: unavailable
- `govulncheck`: unavailable

## High / P1 (Fix This Week)

### H1. CLI source removal leaves unattributable dangling projections

- **Fact:** `internal/cli/sources.go:35-47` calls only `runtime.manager.Remove(...)`. `internal/source/manager.go:196-234` removes the gitlink and catalog policy but never retires projections. TUI separately disables project/global projections at `internal/tui/model.go:516-533`.
- **Inference (high confidence):** after CLI removal, symlinks point into the deleted source. Because doctor scans only `activeSources(runtime.catalog)` at `internal/cli/inspect.go:308-323`, the removed provider can no longer be attributed and the dangling links become invisible.
- **Recommendation:** create one source-removal workflow that preflights and retires project/global projections, removes the source, and restores both sides on failure.
- **Verify:** confirmed by adversarial review.

### H2. TUI source update omits orphan reconciliation

- **Fact:** `internal/tui/model.go:1161-1164` runs only `updater.Update`; completion at `internal/tui/model.go:248-283` reloads the catalog and projection manager. CLI calls `autoPruneAfterUpdate` at `internal/cli/sources.go:231-238`, which prunes both scopes at `internal/cli/root.go:543-557`.
- **Inference (high confidence):** an upstream-removed Skill disappears from the refreshed TUI while its project/global symlink remains broken.
- **Recommendation:** deepen update into a shared workflow used by both CLI and TUI, returning update, reload, prune, and partial-failure facts together.
- **Verify:** confirmed by adversarial review; `internal/tui/model_test.go:333-366` characterizes the reload but does not assert cleanup.

### H3. A source updated to zero Skills bypasses every orphan detector

- **Fact:** `internal/catalog/catalog.go:441-460` retains a physical vendor source with zero discovered Skills. `internal/projection/orphan.go:55-58` then executes `if len(source.Skills) == 0 { continue }`. CLI auto-prune and doctor both use this detector.
- **Inference (high confidence):** when upstream legitimately removes the last Skill, every old projection remains dangling and doctor reports no orphan.
- **Recommendation:** distinguish `unavailable checkout` from `successfully refreshed empty source`; reconciliation may clean only the latter, using update provenance or the pre-update provider set.
- **Verify:** confirmed by adversarial review; the safety guard is intentional but conflates two states.

### H4. `reset --hard` does not enforce read-only vendor content

- **Fact:** `internal/source/manager.go:298-302` runs only `git reset --hard HEAD`; no `git clean` exists. Discovery accepts every filesystem `SKILL.md` under active roots at `internal/catalog/catalog.go:567-633` without checking Git ownership.
- **Inference (high confidence):** an untracked or ignored local Skill survives update and is loaded as remote source content, violating the read-only catalog contract.
- **Recommendation:** define and enforce a reset-plus-clean policy before update, then verify `status --porcelain --untracked-files=all` is empty. Cover untracked, ignored, sparse, and collision cases with real-Git tests.
- **Verify:** confirmed by adversarial review.

### H5. `skills create --scope` can persist an unloadable catalog

- **Fact:** `internal/cli/skills.go:112` passes the raw scope to `ScaffoldLocalSkill`; `internal/catalog/catalog.go:283-318` validates syntax only and writes `local/<scope>`. The next load rejects an unknown client at `internal/catalog/catalog.go:535-542`.
- **Inference (high confidence):** `skills create demo --scope ghost` succeeds, then every catalog-dependent command fails until the directory is manually repaired.
- **Recommendation:** validate a typed scope before mutation and enforce the registry invariant inside the catalog mutation module.
- **Verify:** confirmed by adversarial review.

### H6. Registry-valid partial clients break Skill CLI surfaces

- **Fact:** `internal/client/registry.go:328-347` accepts a client with any one adapter. Catalog defaults target all registered clients at `internal/catalog/catalog.go:346-352`. `buildListOutput`, status, and doctor inspect project Skill state without `SupportsScope` guards (`internal/cli/root.go:275-295`, `internal/cli/inspect.go:180-205,273-291`).
- **Inference (high confidence):** a valid MCP-only or prompt-only custom client makes `skills list`, `status`, and `doctor` abort with `does not support skills`; TUI renders issue cells instead of a capability state.
- **Recommendation:** deepen the client adapter registry around explicit capabilities; default Skill targets only to supported adapters and map unsupported scopes to incompatible/omitted facts.
- **Verify:** confirmed by adversarial review.

### H7. Catalog branch preflight and Git update can target different revisions

- **Fact:** preflight reads `refs/heads/<source.Branch>` at `internal/source/manager.go:304-326`; apply runs `git submodule update --remote` without branch/SHA at `internal/source/manager.go:360-364`, then never verifies post-update `HEAD`.
- **Inference (high confidence):** drift between `catalog.yaml`, `.gitmodules`, and local submodule config can report branch A's SHA while checking out branch B.
- **Recommendation:** make one store authoritative and checkout the exact planned revision, or synchronize branch configuration and verify actual `HEAD` before returning success.
- **Verify:** confirmed by adversarial review.

### H8. Catalog and MCP SSOT mutations lose concurrent updates

- **Fact:** source policy mutations read the full YAML then rename a replacement at `internal/catalog/catalog.go:134-180,202-237`. MCP catalog mutation does the same at `internal/mcp/catalog.go:149-185`. Neither path locks nor compares the destination immediately before rename.
- **Inference (high confidence):** two CLI/TUI processes can both read A, successfully write A+X and A+Y, and silently lose X or Y.
- **Recommendation:** add advisory locking or compare-and-retry semantics around each SSOT read-modify-write transaction.
- **Verify:** confirmed by adversarial review.

### H9. Missing vendor checkout directories disappear from control surfaces

- **Fact:** `discoverVendorSources` is driven only by physical directories and consults `config.Sources[id]` inside that loop (`internal/catalog/catalog.go:423-460`). It never cross-checks configured policies without directories.
- **Inference (high confidence):** a missing checkout is absent from list/update/doctor; `source add` cannot repair it because the policy already exists.
- **Recommendation:** represent configured-but-missing sources as broken/uninitialized sources and provide an initialization/recovery path.
- **Verify:** confirmed by adversarial review.

## Medium / P2 (Plan to Fix)

All Medium findings are evidence-backed but `unverified` under the full-audit protocol.

| ID | Category | Fact | Inference | Recommendation |
|---|---|---|---|---|
| M1 | update safety | `applyUpdate` returns `applied=true` after sparse-checkout failures (`internal/source/manager.go:368-383`), and CLI still calls auto-prune. | An incomplete checkout may drive destructive cleanup. | Separate checkout movement from fully reconciled readiness; prune only verified sources. |
| M2 | diagnostic fidelity | `projection.directState` converts `Readlink`/`Stat` errors to `StateBroken,nil` (`internal/projection/projection.go:112-129`). | doctor cannot distinguish ENOENT, EACCES, loops, or I/O failures. | Carry structured state issues with original causes. |
| M3 | exit semantics | prune failure is printed as a warning but omitted from returned error (`internal/cli/sources.go:231-267`). | automation can receive exit 0 for incomplete reconciliation. | Return a nonzero incomplete result by default; expose structured warnings in JSON. |
| M4 | filesystem cleanup | `removeSubmoduleGitdir` trusts a Git-returned path and ignores `RemoveAll` errors (`internal/source/manager.go:240-252`). | remove may report success while re-add later fails; containment is not independently checked. | Validate against git common-dir/modules and return structured cleanup errors. |
| M5 | secrets management | MCP `env`/`headers` are persisted verbatim in the Git-backed catalog (`internal/mcp/catalog.go:57-65,128-146`). | static tokens can be committed or pushed accidentally. | Prefer `${ENV_VAR}` references; reject or explicitly confirm plaintext secret-like values. |
| M6 | transaction integrity | MCP removal disables project configs before catalog deletion, without rollback (`internal/cli/resources.go:181-188`). | catalog write failure leaves registered-but-disabled state. | Coordinate catalog and projection mutations with captured rollback state. |
| M7 | discovery errors | `containsSkillManifest` discards every `WalkDir` error (`internal/catalog/discovery.go:262-274`). | permissions/I/O failures become misleading `contains no SKILL.md`. | Return `(bool,error)` and preserve the first cause. |
| M8 | version compatibility | MCP catalog decodes but never validates `version` (`internal/mcp/catalog.go:52-94`). | future formats may be rewritten under v1 semantics. | Reject unsupported versions on load and mutation. |
| M9 | batch atomicity | MCP import preflights names but calls `AddServer` once per entry (`internal/cli/resources.go:59-69`). | a later failure leaves a partial import. | Validate and persist all servers in one catalog transaction. |
| M10 | crash consistency | bootstrap writes final config files directly and leaves failed partial paths (`internal/bootstrap/bootstrap.go:133-147`). | later init sees `ErrExist` and accepts a corrupt partial file. | install synced temp files atomically and clean failures. |
| M11 | permission preservation | catalog mutations always chmod replacements to `0644` (`internal/catalog/catalog.go:161-178,215-236`). | user-selected restrictive modes are widened. | preserve existing permission bits; use `0644` only on creation. |
| M12 | root resolution | `project.FindRoot` accepts any existing `.git` entry (`internal/project/project.go:26-30`). | unrelated directories can be treated as project roots for project-scoped writes. | use `git rev-parse --show-toplevel` or validate gitdir format. |
| M13 | global state contract | project view returns `StateGlobal` for any occupied global path before validating it (`internal/projection/projection.go:68-90`). | unmanaged/broken paths are counted as enabled by status. | validate direct global state first; map only healthy managed links to global. |
| M14 | JSON failure contract | update failures live only in returned errors; JSON contains successes and pruned links (`internal/source/manager.go:292-348`, `internal/cli/sources.go:240-278`). | machine consumers cannot associate failure operation/path/source structurally. | return per-source structured outcomes including failures. |
| M15 | JSON scope contract | project/global pruned links are merged and serialized without scope (`internal/cli/root.go:544-556`, `internal/cli/sources.go:280-295`). | consumers must infer scope from filesystem paths. | carry and serialize `projection.Scope`. |
| M16 | provider deletion | `skills delete --client` clears only selected clients, then removes the shared provider (`internal/cli/skills.go:183-201`). | omitted clients retain links to a deleted directory. | provider deletion must retire all managed projections; make client filtering projection-only. |
| M17 | duplicated transaction module | Skill projection and file projection each implement preflight, directory tracking, apply, concurrency recheck, and rollback (`internal/projection/projection.go:216-396,518-614`; `internal/linkprojection/manager.go:90-315`). | failure semantics can drift; `linkprojection` also has no direct package tests. | deepen one atomic link transaction module while keeping Skill policy outside its seam. |
| M18 | extension cost | adding global resources changed 26 files (`959e657`); current kind/scope decisions repeat across client config/registry, CLI runtime, TUI model/view, i18n, and tests. | each new resource kind causes shotgun surgery and duplicated state interpretation. | deepen client adapters and resource switching; keep CLI/TUI as presentation adapters. |

## Remediation Progress (2026-07-16)

The implementation pass resolved all 9 High findings and all 18 Medium findings. The shared source lifecycle now owns CLI/TUI update and removal; vendor updates reset and clean immutable checkouts, fetch the configured branch, and verify the exact advertised revision. Refreshed zero-Skill sources reconcile project and global projections, while missing configured checkouts remain visible and recoverable through `source update`.

Projection changes now use reversible atomic retirement, clients are routed by declared resource capability, and projection health retains concrete filesystem causes. Catalog and MCP mutations use inter-process locks, preserve file modes, and batch MCP imports atomically. Project-root detection, bootstrap first-write crash consistency, structured update failures, MCP removal rollback, and provider-deletion safety were also hardened.

Submodule gitdir cleanup now validates Git common-dir containment and propagates deletion failures. New MCP definitions reject plaintext secret-like env/header values unless they reference `${ENV_VAR}`, while doctor surfaces legacy plaintext entries for migration.

Skill and file projection now delegate create/remove/replace execution, concurrent-change checks, rollback, and reversible restore to `internal/linktransaction`; their modules retain only projection-specific planning and source validation. File resource metadata now lives in the `userresource` descriptor registry, and CLI registration/runtime loading/doctor plus TUI capability/catalog/manager routing consume that registry instead of four parallel resource-kind branches.

The ledger has no open findings. `go test ./...` and `go test -race ./...` both passed 229 tests across 17 packages; `go vet ./...` passed. Windows cross-compilation passed for `internal/filelock` and `internal/linktransaction`.

## Refuted by Verification

None.

## Repair Roadmap

| Phase | Scope | Est. Files |
|---|---|---:|
| 0 | Source lifecycle correctness: H1-H4, H7, H9; shared update/remove workflow | 10-14 |
| 1 | Catalog/client validity: H5-H6, H8; capability-aware registry and locked SSOT | 8-12 |
| 2 | Atomic link transactions: M6, M17; migrate Skill/file projection | 6-9 |
| 3 | Resource switching: deepen operation planning used by CLI/TUI | 8-12 |
| 4 | Projection health: M2-M3, M13-M15; one normalized health model | 6-10 |
| 5 | Persistence hardening: M4-M12, M16; docs and migration notes | 10-15 |

The repository does not currently ignore `.audit/`; add it to `.gitignore` if audit ledgers should remain local. This audit did not edit `.gitignore`.
