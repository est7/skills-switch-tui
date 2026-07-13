package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDiscoversSourceGroupsAndAppliesCompatibilityOverrides(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "codex-dynamic-workflows"), `---
name: codex-dynamic-workflows
description: Run Codex-native workflows.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "worktrunk", "skills", "worktrunk"), `---
name: worktrunk
description: Manage worktrees with Worktrunk.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "worktrunk", "skills", "wt-switch-create"), `---
name: wt-switch-create
description: Create and switch worktrees.
---
`)

	config := `version: 1
defaults:
  targets: [codex, claude, gemini]
overrides:
  local/codex-dynamic-workflows:
    targets: [codex]
    reason: Requires Codex goal and collaboration tools.
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := len(loaded.Sources), 2; got != want {
		t.Fatalf("source count = %d, want %d", got, want)
	}

	worktrunk, ok := loaded.Skill("vendor/worktrunk/skills/worktrunk")
	if !ok {
		t.Fatal("worktrunk skill was not discovered")
	}
	if !worktrunk.Supports(ClientCodex) || !worktrunk.Supports(ClientClaude) || !worktrunk.Supports(ClientGemini) {
		t.Fatalf("worktrunk targets = %v, want all clients", worktrunk.Targets)
	}

	dynamic, ok := loaded.Skill("local/codex-dynamic-workflows")
	if !ok {
		t.Fatal("codex-dynamic-workflows skill was not discovered")
	}
	if !dynamic.Supports(ClientCodex) || dynamic.Supports(ClientClaude) || dynamic.Supports(ClientGemini) {
		t.Fatalf("codex-dynamic-workflows targets = %v, want codex only", dynamic.Targets)
	}
	if got, want := dynamic.CompatibilityReason, "Requires Codex goal and collaboration tools."; got != want {
		t.Fatalf("compatibility reason = %q, want %q", got, want)
	}
}

func TestLoadUsesFirstAvailableDiscoveryStrategyAndOnlyDeclaredSkills(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "mixed")
	writeSkill(t, filepath.Join(vendorRoot, "skills", "registered"), `---
name: registered
description: Declared by the preferred plugin manifest.
---
`)
	writeSkill(t, filepath.Join(vendorRoot, "skills", "unregistered"), `---
name: unregistered
description: Present on disk but not registered.
---
`)
	writeSkill(t, filepath.Join(vendorRoot, "plugins", "codex", "skills", "codex-only"), `---
name: codex-only
description: Declared by a lower-priority marketplace.
---
`)
	writeJSON(t, filepath.Join(vendorRoot, ".claude-plugin", "plugin.json"), `{
  "name": "mixed",
  "skills": ["./skills/registered"]
}`)
	writeJSON(t, filepath.Join(vendorRoot, ".agents", "plugins", "marketplace.json"), `{
  "name": "mixed",
  "plugins": [{
    "name": "codex",
    "source": {"source": "local", "path": "./plugins/codex"}
  }]
}`)
	config := `version: 1
sources:
  vendor/mixed:
    discoveryPriority:
      - claude-plugin
      - agents-marketplace
      - skills-dir
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Skill("vendor/mixed/skills/registered"); !ok {
		t.Fatal("manifest-declared skill was not discovered")
	}
	for _, id := range []string{
		"vendor/mixed/skills/unregistered",
		"vendor/mixed/plugins/codex/skills/codex-only",
	} {
		if _, ok := loaded.Skill(id); ok {
			t.Fatalf("lower-authority skill %q was discovered", id)
		}
	}
}

func TestLoadDiscoversSkillsFromAgentsMarketplacePluginSources(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "worktrunk")
	writeSkill(t, filepath.Join(vendorRoot, "skills", "distribution-copy"), `---
name: distribution-copy
description: A root copy that is outside the selected marketplace.
---
`)
	writeSkill(t, filepath.Join(vendorRoot, "plugins", "worktrunk", "skills", "worktrunk"), `---
name: worktrunk
description: Registered through the Codex marketplace.
---
`)
	writeSkill(t, filepath.Join(vendorRoot, "plugins", "worktrunk", "skills", "wt-switch-create"), `---
name: wt-switch-create
description: Registered through the Codex marketplace.
---
`)
	writeJSON(t, filepath.Join(vendorRoot, ".agents", "plugins", "marketplace.json"), `{
  "name": "worktrunk",
  "plugins": [{
    "name": "worktrunk",
    "source": {"source": "local", "path": "./plugins/worktrunk"}
  }]
}`)
	writeJSON(t, filepath.Join(vendorRoot, "plugins", "worktrunk", ".codex-plugin", "plugin.json"), `{
  "name": "worktrunk"
}`)
	config := `version: 1
sources:
  vendor/worktrunk:
    discoveryPriority: [agents-marketplace, skills-dir]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, id := range []string{
		"vendor/worktrunk/plugins/worktrunk/skills/worktrunk",
		"vendor/worktrunk/plugins/worktrunk/skills/wt-switch-create",
	} {
		if _, ok := loaded.Skill(id); !ok {
			t.Fatalf("marketplace skill %q was not discovered", id)
		}
	}
	if _, ok := loaded.Skill("vendor/worktrunk/skills/distribution-copy"); ok {
		t.Fatal("lower-priority root skills directory was discovered")
	}
	source, ok := loaded.Source("vendor/worktrunk")
	if !ok {
		t.Fatal("worktrunk source was not discovered")
	}
	if got, want := source.DiscoveryStrategy, DiscoveryAgentsMarketplace; got != want {
		t.Fatalf("discovery strategy = %q, want %q", got, want)
	}
	plan, err := PlanVendorDiscovery(vendorRoot, source.DiscoveryPriority)
	if err != nil {
		t.Fatalf("PlanVendorDiscovery() error = %v", err)
	}
	wantSparsePaths := []string{
		".agents/plugins",
		"plugins/worktrunk/.codex-plugin",
		"plugins/worktrunk/skills",
	}
	if len(plan.SparsePaths) != len(wantSparsePaths) {
		t.Fatalf("sparse paths = %v, want %v", plan.SparsePaths, wantSparsePaths)
	}
	for index, want := range wantSparsePaths {
		if got := plan.SparsePaths[index]; got != want {
			t.Fatalf("sparse path[%d] = %q, want %q", index, got, want)
		}
	}
}

func TestPlanVendorDiscoveryReturnsOnlyRequiredSparsePaths(t *testing.T) {
	vendorRoot := t.TempDir()
	writeSkill(t, filepath.Join(vendorRoot, "skills", "registered"), `---
name: registered
description: Registered by manifest.
---
`)
	writeSkill(t, filepath.Join(vendorRoot, "skills", "unregistered"), `---
name: unregistered
description: Not registered by manifest.
---
`)
	writeJSON(t, filepath.Join(vendorRoot, ".claude-plugin", "plugin.json"), `{
  "name": "sparse",
  "skills": ["./skills/registered"]
}`)

	plan, err := PlanVendorDiscovery(vendorRoot, []DiscoveryStrategy{
		DiscoveryClaudePlugin,
		DiscoverySkillsDir,
	})
	if err != nil {
		t.Fatalf("PlanVendorDiscovery() error = %v", err)
	}
	if got, want := plan.Strategy, DiscoveryClaudePlugin; got != want {
		t.Fatalf("strategy = %q, want %q", got, want)
	}
	wantPaths := []string{".claude-plugin", "skills/registered"}
	if len(plan.SparsePaths) != len(wantPaths) {
		t.Fatalf("sparse paths = %v, want %v", plan.SparsePaths, wantPaths)
	}
	for index, want := range wantPaths {
		if got := plan.SparsePaths[index]; got != want {
			t.Fatalf("sparse path[%d] = %q, want %q", index, got, want)
		}
	}
}

func TestLoadTreatsExplicitEmptyPluginSkillsAsAuthoritative(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "empty")
	writeSkill(t, filepath.Join(vendorRoot, "skills", "must-stay-hidden"), `---
name: must-stay-hidden
description: Not registered by the plugin.
---
`)
	writeJSON(t, filepath.Join(vendorRoot, ".claude-plugin", "plugin.json"), `{
  "name": "empty",
  "skills": []
}`)
	config := `version: 1
sources:
  vendor/empty:
    discoveryPriority: [claude-plugin, skills-dir]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Skill("vendor/empty/skills/must-stay-hidden"); ok {
		t.Fatal("explicit empty registration fell back to the skills directory")
	}
}

func TestLoadRejectsManifestSkillSymlinkOutsideSource(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "unsafe")
	outside := filepath.Join(t.TempDir(), "outside")
	writeSkill(t, outside, `---
name: outside
description: Must not escape the source boundary.
---
`)
	if err := os.MkdirAll(filepath.Join(vendorRoot, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(vendorRoot, "skills", "outside")); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(vendorRoot, ".claude-plugin", "plugin.json"), `{
  "name": "unsafe",
  "skills": ["./skills/outside"]
}`)
	config := `version: 1
sources:
  vendor/unsafe:
    discoveryPriority: [claude-plugin]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(sourcesRoot)
	if err == nil || !strings.Contains(err.Error(), "symlink target escapes source root") {
		t.Fatalf("Load() error = %v, want source-boundary rejection", err)
	}
}

func TestLoadMarksArchivedSourcesAndLoadsVendorUpdatePolicy(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "worktrunk", "skills", "worktrunk"), `---
name: worktrunk
description: Manage worktrees with Worktrunk.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "archive", "waza", "read"), `---
name: waza-read
description: Archived reference material.
---
`)
	config := `version: 1
sources:
  vendor/worktrunk:
    branch: main
    sparsePaths: [skills]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	worktrunk, ok := loaded.Source("vendor/worktrunk")
	if !ok {
		t.Fatal("vendor/worktrunk source was not discovered")
	}
	if got, want := worktrunk.Branch, "main"; got != want {
		t.Fatalf("branch = %q, want %q", got, want)
	}
	if got, want := worktrunk.SparsePaths, []string{"skills"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("sparse paths = %v, want %v", got, want)
	}
	if got, want := worktrunk.DiscoveryStrategy, DiscoverySkillsDir; got != want {
		t.Fatalf("discovery strategy = %q, want %q", got, want)
	}
	plan, err := PlanVendorDiscovery(worktrunk.Path, worktrunk.DiscoveryPriority)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.SparsePaths, []string{"skills"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("derived sparse paths = %v, want %v", got, want)
	}

	archived, ok := loaded.Source("archive/waza")
	if !ok {
		t.Fatal("archive/waza source was not discovered")
	}
	if !archived.Archived {
		t.Fatal("archive/waza was not marked archived")
	}
}

func TestLoadRegistersConfiguredClientAndIncludesItInDefaultTargets(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "portable"), `---
name: portable
description: Works with every registered client.
---
`)
	config := `version: 1
clients:
  pi:
    projectSkillsDir: .pi/skills
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Clients.Has(Client("pi")) {
		t.Fatal("configured pi client was not registered")
	}
	skill, ok := loaded.Skill("local/portable")
	if !ok {
		t.Fatal("portable skill was not discovered")
	}
	if !skill.Supports(Client("pi")) {
		t.Fatalf("portable targets = %v, want pi included by default", skill.Targets)
	}
}

func writeSkill(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
