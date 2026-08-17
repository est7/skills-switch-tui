# Changelog

All notable changes to this project will be documented in this file.

## [v0.15.1] - 2026-08-17

### Bug Fixes

- **projection:** Scope same-name conflicts to the same provider

## [v0.15.0] - 2026-08-14

### Features

- **tui:** Surface discover/adopt, spinner, and errors

### Documentation

- **changelog:** Update for v0.15.0

## [v0.14.0] - 2026-08-14

### Features

- **cli:** Report mutating command results as JSON
- **cli:** Cancel git work on SIGINT/SIGTERM
- **skills:** Discover and adopt unmanaged skills

### Documentation

- **cli:** Generate the command reference
- **release:** Generate the changelog with git-cliff
- **changelog:** Update for v0.14.0

### Styling

- **core:** Satisfy golangci-lint defaults

### Performance

- **tui:** Serve rendering from a state cache

### Testing

- **filelock:** Prove exclusive lock serialization

### Continuous Integration

- Add lint, race matrix, and docs drift guard

## [v0.13.0] - 2026-08-11

### Features

- **source:** Identify vendor sources by remote under owner/repo

### Documentation

- **core:** Record the owner-qualified layout and source migrate
- **core:** State the version floor a migrated catalog requires

## [v0.12.2] - 2026-07-17

### Bug Fixes

- **bootstrap:** Anchor derived skills/ gitignore entry to repo root

## [v0.12.1] - 2026-07-17

### Bug Fixes

- **catalog:** Fall back past unusable marketplace manifests

## [v0.12.0] - 2026-07-16

### Features

- **skills:** 支持互斥的全局 Skill 作用域
- **tui:** 优化界面并校准作用域文案

### Bug Fixes

- **init:** 隔离全局 Skill 派生目录

### Documentation

- **skills:** 说明全局作用域互斥规则
- **core:** 同步资源作用域与更新契约

### Refactoring

- **core:** 统一资源生命周期与投影事务
- **core:** 深化投影事务接口

## [v0.11.0] - 2026-07-16

### Features

- **resources:** 管理全局 Agent 资源与提示词

### Bug Fixes

- **source:** 隔离只读仓库更新失败
- **resources:** 恢复命令与钩子的项目作用域

## [v0.10.0] - 2026-07-15

### Features

- **projection:** Detect and prune orphaned projections
- **cli:** Reconcile project projections against source updates

### Documentation

- **readme:** Document orphan reconciliation and skills prune

## [v0.9.1] - 2026-07-15

### Documentation

- **readme:** Add end-to-end usage quickstart

## [v0.9.0] - 2026-07-14

### Features

- **catalog:** Root-walk a manifest-less vendor repo so a flat skills repo works
- **catalog:** Make discovery robust to real-world skill repos

### Documentation

- Document container/plugin-dir --skill-path and marketplace-repo selection
- Document owner/repo shorthand and manifest-less root-walk discovery

### Refactoring

- **source:** Unify source input parsing into ParseSourceRef

## [v0.8.0] - 2026-07-14

### Features

- **catalog,source:** Register one plugin from a marketplace repo via a container skill-path

## [v0.7.0] - 2026-07-14

### Features

- **cli,tui:** **Breaking:** URL/JSON-driven adds, local skill scaffold, huh dialogs, noun-grouped CLI

### Bug Fixes

- **source:** Force-remove staged submodules on source remove

### Documentation

- **skill:** Clarify manifest discovery vs --skill-path in operator prompt
- Update operator SKILL, README, and CLAUDE for the new commands and CLI reorg

## [v0.6.0] - 2026-07-13

### Features

- **tui:** Add and delete MCP servers from the catalog
- **cli:** Add skills delete and mcp add/remove subcommands

### Documentation

- **skill:** Document skills delete and mcp add/remove in the operator SKILL

## [v0.5.0] - 2026-07-13

### Features

- **catalog:** Treat local group directories as distinct sources
- **tui:** Delete sources and local skills with confirmation

## [v0.4.1] - 2026-07-13

### Bug Fixes

- **tui:** Eliminate transparent spacing gaps

## [v0.4.0] - 2026-07-13

### Features

- **tui:** Add cohesive bulk resource controls

## [v0.3.1] - 2026-07-13

### Bug Fixes

- **tui:** Render an opaque full-window background

### Documentation

- Add repository and release guidance

## [v0.3.0] - 2026-07-13

### Features

- **app:** Add bootstrap skill and multi-client TUI controls

## [v0.2.0] - 2026-07-13

### Features

- **resources:** 统一多类 Agent 资源管理

## [v0.1.0] - 2026-07-13

### Other

- Build project-local skill switcher
- Document Homebrew installation

