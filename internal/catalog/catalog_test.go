package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestLoadDiscoversSourceGroupsAndAppliesCompatibilityOverrides(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "codex-tools", "codex-dynamic-workflows"), `---
name: codex-dynamic-workflows
description: Run Codex-native workflows.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "worktrunk"), `---
name: worktrunk
description: Manage worktrees with Worktrunk.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "wt-switch-create"), `---
name: wt-switch-create
description: Create and switch worktrees.
---
`)

	config := `version: 1
defaults:
  targets: [codex, claude, gemini]
overrides:
  local-shared/codex-tools/codex-dynamic-workflows:
    targets: [codex]
    reason: Requires Codex goal and collaboration tools.
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := len(loaded.Sources), 2; got != want {
		t.Fatalf("source count = %d, want %d", got, want)
	}

	worktrunk, ok := loaded.Skill("vendor-shared/worktrunk/skills/worktrunk")
	if !ok {
		t.Fatal("worktrunk skill was not discovered")
	}
	if !worktrunk.Supports(ClientCodex) || !worktrunk.Supports(ClientClaude) || !worktrunk.Supports(ClientGemini) {
		t.Fatalf("worktrunk targets = %v, want all clients", worktrunk.Targets)
	}

	dynamic, ok := loaded.Skill("local-shared/codex-tools/codex-dynamic-workflows")
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

func TestLoadDiscoversSharedAndClientOnlyLocalSources(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "base", "portable"), `---
name: portable
description: Runs in every registered client.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "local", "codex", "tools", "codex-workflow"), `---
name: codex-workflow
description: Uses Codex-native workflow APIs.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "local", "pi", "tools", "pi-workflow"), `---
name: pi-workflow
description: Uses Pi-native workflow APIs.
---
`)

	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {ProjectSkillsDir: ".pi/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, registry)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	portable, ok := loaded.Skill("local-shared/base/portable")
	if !ok {
		t.Fatal("shared local skill was not discovered")
	}
	for _, target := range []Client{ClientCodex, ClientClaude, ClientGemini, Client("pi")} {
		if !portable.Supports(target) {
			t.Fatalf("portable does not support %s: %v", target, portable.Targets)
		}
	}

	codexWorkflow, ok := loaded.Skill("local-codex-only/tools/codex-workflow")
	if !ok {
		t.Fatal("Codex-only local skill was not discovered")
	}
	if !codexWorkflow.Supports(ClientCodex) || codexWorkflow.Supports(ClientClaude) || codexWorkflow.Supports(ClientGemini) || codexWorkflow.Supports(Client("pi")) {
		t.Fatalf("codex-workflow targets = %v, want codex only", codexWorkflow.Targets)
	}

	piWorkflow, ok := loaded.Skill("local-pi-only/tools/pi-workflow")
	if !ok {
		t.Fatal("Pi-only local skill was not discovered")
	}
	if !piWorkflow.Supports(Client("pi")) || piWorkflow.Supports(ClientCodex) {
		t.Fatalf("pi-workflow targets = %v, want pi only", piWorkflow.Targets)
	}

	piSource, ok := loaded.Source("local-pi-only/tools")
	if !ok {
		t.Fatal("logical local-pi-only/tools source was not discovered")
	}
	if piSource.Kind != SourceLocal || piSource.Scope != "pi" {
		t.Fatalf("pi source = kind %q scope %q", piSource.Kind, piSource.Scope)
	}
}

func TestLoadDiscoversLocalGroupsAsSeparateSources(t *testing.T) {
	sourcesRoot := t.TempDir()
	// Nested group: skills live one level under the group directory.
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "design"), `---
name: design
description: Design philosophy.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "discipline"), `---
name: discipline
description: Coding discipline.
---
`)
	// Bare-root group: the group directory is itself a single skill, so the
	// trailing segment doubles, matching vendor root-level skills.
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "make-goal"), `---
name: make-goal
description: A standalone skill group.
---
`)
	// Manifest-registered group: a .claude-plugin/plugin.json marks the group;
	// skills stay flat under the group root and are still discovered by the
	// root-walk fallback.
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "android", "compose"), `---
name: compose
description: Compose component generator.
---
`)
	writeJSON(t, filepath.Join(sourcesRoot, "local", "shared", "android", ".claude-plugin", "plugin.json"), `{"name": "android"}`)

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	for _, id := range []string{"local-shared/core", "local-shared/make-goal", "local-shared/android"} {
		source, ok := loaded.Source(id)
		if !ok {
			t.Fatalf("local group source %q was not discovered", id)
		}
		if source.Kind != SourceLocal || source.Scope != "shared" {
			t.Fatalf("source %q = kind %q scope %q, want local/shared", id, source.Kind, source.Scope)
		}
	}

	for _, id := range []string{
		"local-shared/core/design",
		"local-shared/core/discipline",
		"local-shared/make-goal/make-goal",
		"local-shared/android/compose",
	} {
		if _, ok := loaded.Skill(id); !ok {
			t.Fatalf("skill %q was not discovered", id)
		}
	}
}

func TestRemoveLocalResourceDeletesGroupAndSkillButGuardsScopeRoot(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "design"), `---
name: design
description: Design philosophy.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "core", "discipline"), `---
name: discipline
description: Coding discipline.
---
`)

	// Removing a single skill leaves the group and its siblings intact.
	skillDir := filepath.Join(sourcesRoot, "local", "shared", "core", "design")
	if err := RemoveLocalResource(sourcesRoot, skillDir); err != nil {
		t.Fatalf("remove local skill: %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skill directory still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "core", "discipline", "SKILL.md")); err != nil {
		t.Fatalf("sibling skill was removed: %v", err)
	}

	// Removing the group removes the whole directory.
	groupDir := filepath.Join(sourcesRoot, "local", "shared", "core")
	if err := RemoveLocalResource(sourcesRoot, groupDir); err != nil {
		t.Fatalf("remove local group: %v", err)
	}
	if _, err := os.Stat(groupDir); !os.IsNotExist(err) {
		t.Fatalf("group directory still present: %v", err)
	}

	// A scope root and any path outside the local tree are refused.
	scopeRoot := filepath.Join(sourcesRoot, "local", "shared")
	if err := RemoveLocalResource(sourcesRoot, scopeRoot); err == nil {
		t.Fatal("removing a scope root must be refused")
	}
	if _, err := os.Stat(scopeRoot); err != nil {
		t.Fatalf("scope root was removed despite guard: %v", err)
	}
	if err := RemoveLocalResource(sourcesRoot, filepath.Join(sourcesRoot, "vendor", "shared", "x")); err == nil {
		t.Fatal("removing a vendor path must be refused")
	}
}

func TestLoadDiscoversClientScopedVendorSource(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "pi", "pi-tools", "skills", "pi-tool"), `---
name: pi-tool
description: Uses Pi-native APIs.
---
`)
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {ProjectSkillsDir: ".pi/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, registry)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	skill, ok := loaded.Skill("vendor-pi-only/pi-tools/skills/pi-tool")
	if !ok {
		t.Fatal("Pi-only vendor skill was not discovered")
	}
	if !skill.Supports(Client("pi")) || skill.Supports(ClientCodex) || skill.Supports(ClientClaude) || skill.Supports(ClientGemini) {
		t.Fatalf("pi-tool targets = %v, want pi only", skill.Targets)
	}
	source, ok := loaded.Source("vendor-pi-only/pi-tools")
	if !ok || source.Kind != SourceVendor || source.Scope != "pi" {
		t.Fatalf("Pi vendor source = %#v", source)
	}
}

func TestLoadRejectsLegacyAndUnknownSourceScopes(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want string
	}{
		{name: "legacy", path: []string{"local-shared", "example"}, want: "legacy source root"},
		{name: "unknown local client", path: []string{"local", "ghost", "example"}, want: `unknown client "ghost"`},
		{name: "unknown vendor client", path: []string{"vendor", "ghost", "repo", "example"}, want: `unknown client "ghost"`},
		{name: "unknown archived client", path: []string{"archived", "ghost", "repo", "example"}, want: `unknown client "ghost"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sourcesRoot := t.TempDir()
			parts := append([]string{sourcesRoot}, test.path...)
			writeSkill(t, filepath.Join(parts...), `---
name: example
description: Invalid local source fixture.
---
`)

			_, err := Load(sourcesRoot, client.DefaultRegistry())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadDegradesMalformedArchivedFrontmatterWithoutBlockingCatalog(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "active", "healthy"), `---
name: healthy
description: Active Skill remains available.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "archived", "shared", "legacy", "broken"), `---
name: broken
description: invalid: unquoted colon
---
`)

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Skill("local-shared/active/healthy"); !ok {
		t.Fatal("healthy active Skill was not discovered")
	}
	broken, ok := loaded.Skill("archived-shared/legacy/broken")
	if !ok {
		t.Fatal("malformed archived Skill was not retained")
	}
	if broken.MetadataIssue == "" {
		t.Fatal("malformed archived Skill does not expose its metadata issue")
	}
	archived, ok := loaded.Source("archived-shared/legacy")
	if !ok || !archived.IsArchived() {
		t.Fatalf("archived source = %#v", archived)
	}
}

func TestLoadRejectsUnsafeActiveSkillNameAndDegradesArchivedName(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		sourcesRoot := t.TempDir()
		writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "unsafe", "skills", "escape"), `---
name: ../../escape
description: Must not control the projection path.
---
`)

		_, err := Load(sourcesRoot, client.DefaultRegistry())
		if err == nil || !strings.Contains(err.Error(), `invalid skill name "../../escape"`) {
			t.Fatalf("Load() error = %v, want unsafe name rejection", err)
		}
	})

	t.Run("archived", func(t *testing.T) {
		sourcesRoot := t.TempDir()
		writeSkill(t, filepath.Join(sourcesRoot, "archived", "shared", "legacy", "escape"), `---
name: ../../escape
description: Retain as reference-only metadata.
---
`)

		loaded, err := Load(sourcesRoot, client.DefaultRegistry())
		if err != nil {
			t.Fatal(err)
		}
		skill, ok := loaded.Skill("archived-shared/legacy/escape")
		if !ok {
			t.Fatal("archived skill was not retained")
		}
		if skill.Name != "escape" || !strings.Contains(skill.MetadataIssue, "invalid skill name") {
			t.Fatalf("archived skill = name %q issue %q", skill.Name, skill.MetadataIssue)
		}
	})
}

func TestLoadUsesFirstAvailableDiscoveryStrategyAndOnlyDeclaredSkills(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "shared", "mixed")
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
  vendor-shared/mixed:
    discoveryPriority:
      - claude-plugin
      - agents-marketplace
      - skills-dir
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Skill("vendor-shared/mixed/skills/registered"); !ok {
		t.Fatal("manifest-declared skill was not discovered")
	}
	for _, id := range []string{
		"vendor-shared/mixed/skills/unregistered",
		"vendor-shared/mixed/plugins/codex/skills/codex-only",
	} {
		if _, ok := loaded.Skill(id); ok {
			t.Fatalf("lower-authority skill %q was discovered", id)
		}
	}
}

func TestLoadUsesExplicitSkillPathsAsAuthoritativeVendorRegistration(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "shared", "spellbook")
	writeSkill(t, filepath.Join(vendorRoot, "skills", "codebase-audit"), `---
name: codebase-audit
description: Registered explicitly.
---
`)
	writeSkill(t, filepath.Join(vendorRoot, "skills", "unrelated"), `---
name: unrelated
description: Present in the repository but not registered.
---
`)
	config := `version: 1
sources:
  vendor-shared/spellbook:
    branch: main
    skillPaths:
      - skills/codebase-audit
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Skill("vendor-shared/spellbook/skills/codebase-audit"); !ok {
		t.Fatal("explicitly registered skill was not discovered")
	}
	if _, ok := loaded.Skill("vendor-shared/spellbook/skills/unrelated"); ok {
		t.Fatal("unregistered skill was discovered")
	}
	source, ok := loaded.Source("vendor-shared/spellbook")
	if !ok {
		t.Fatal("spellbook source was not discovered")
	}
	if source.DiscoveryStrategy != DiscoveryExplicit {
		t.Fatalf("discovery strategy = %q, want %q", source.DiscoveryStrategy, DiscoveryExplicit)
	}
	if got, want := source.SkillPaths, []string{"skills/codebase-audit"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("skill paths = %v, want %v", got, want)
	}

	plan, err := PlanVendorDiscovery(vendorRoot, nil, source.SkillPaths)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.SparsePaths, []string{"skills/codebase-audit"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("sparse paths = %v, want %v", got, want)
	}
}

func TestPlanVendorDiscoveryAllowsExplicitRootSkillWithoutSparseCheckout(t *testing.T) {
	vendorRoot := t.TempDir()
	writeSkill(t, vendorRoot, `---
name: root-skill
description: The repository root is the Skill directory.
---
`)

	plan, err := PlanVendorDiscovery(vendorRoot, nil, []string{"."})
	if err != nil {
		t.Fatalf("PlanVendorDiscovery() error = %v", err)
	}
	if plan.Strategy != DiscoveryExplicit {
		t.Fatalf("strategy = %q, want %q", plan.Strategy, DiscoveryExplicit)
	}
	if len(plan.SparsePaths) != 0 {
		t.Fatalf("sparse paths = %v, want full root checkout", plan.SparsePaths)
	}
}

func TestLoadDiscoversSkillsFromAgentsMarketplacePluginSources(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk")
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
  vendor-shared/worktrunk:
    discoveryPriority: [agents-marketplace, skills-dir]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, id := range []string{
		"vendor-shared/worktrunk/plugins/worktrunk/skills/worktrunk",
		"vendor-shared/worktrunk/plugins/worktrunk/skills/wt-switch-create",
	} {
		if _, ok := loaded.Skill(id); !ok {
			t.Fatalf("marketplace skill %q was not discovered", id)
		}
	}
	if _, ok := loaded.Skill("vendor-shared/worktrunk/skills/distribution-copy"); ok {
		t.Fatal("lower-priority root skills directory was discovered")
	}
	source, ok := loaded.Source("vendor-shared/worktrunk")
	if !ok {
		t.Fatal("worktrunk source was not discovered")
	}
	if got, want := source.DiscoveryStrategy, DiscoveryAgentsMarketplace; got != want {
		t.Fatalf("discovery strategy = %q, want %q", got, want)
	}
	plan, err := PlanVendorDiscovery(vendorRoot, source.DiscoveryPriority, nil)
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
	}, nil)
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
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "shared", "empty")
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
  vendor-shared/empty:
    discoveryPriority: [claude-plugin, skills-dir]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := loaded.Skill("vendor-shared/empty/skills/must-stay-hidden"); ok {
		t.Fatal("explicit empty registration fell back to the skills directory")
	}
}

func TestLoadRejectsManifestSkillSymlinkOutsideSource(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "shared", "unsafe")
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
  vendor-shared/unsafe:
    discoveryPriority: [claude-plugin]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(sourcesRoot, client.DefaultRegistry())
	if err == nil || !strings.Contains(err.Error(), "symlink target escapes source root") {
		t.Fatalf("Load() error = %v, want source-boundary rejection", err)
	}
}

func TestLoadMarksArchivedSourcesAndLoadsVendorUpdatePolicy(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "worktrunk"), `---
name: worktrunk
description: Manage worktrees with Worktrunk.
---
`)
	writeSkill(t, filepath.Join(sourcesRoot, "archived", "shared", "waza", "read"), `---
name: waza-read
description: Archived reference material.
---
`)
	config := `version: 1
sources:
  vendor-shared/worktrunk:
    branch: main
    sparsePaths: [skills]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	worktrunk, ok := loaded.Source("vendor-shared/worktrunk")
	if !ok {
		t.Fatal("vendor-shared/worktrunk source was not discovered")
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
	plan, err := PlanVendorDiscovery(worktrunk.Path, worktrunk.DiscoveryPriority, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.SparsePaths, []string{"skills"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("derived sparse paths = %v, want %v", got, want)
	}

	archived, ok := loaded.Source("archived-shared/waza")
	if !ok {
		t.Fatal("archived-shared/waza source was not discovered")
	}
	if !archived.IsArchived() {
		t.Fatal("archived-shared/waza was not marked archived")
	}
}

func TestLoadRegistersConfiguredClientAndIncludesItInDefaultTargets(t *testing.T) {
	sourcesRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "base", "portable"), `---
name: portable
description: Works with every registered client.
---
`)
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {ProjectSkillsDir: ".pi/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(sourcesRoot, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Clients.Has(Client("pi")) {
		t.Fatal("configured pi client was not registered")
	}
	skill, ok := loaded.Skill("local-shared/base/portable")
	if !ok {
		t.Fatal("portable skill was not discovered")
	}
	if !skill.Supports(Client("pi")) {
		t.Fatalf("portable targets = %v, want pi included by default", skill.Targets)
	}
}

func TestLoadRejectsClientRegistryFieldsInSkillCatalog(t *testing.T) {
	sourcesRoot := t.TempDir()
	config := "version: 1\nclients:\n  pi:\n    projectSkillsDir: .pi/skills\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(sourcesRoot, client.DefaultRegistry())
	if err == nil || !strings.Contains(err.Error(), "field clients not found") {
		t.Fatalf("Load() error = %v, want client registry rejection", err)
	}
}

func TestLoadRejectsMultipleSkillCatalogDocuments(t *testing.T) {
	sourcesRoot := t.TempDir()
	config := "version: 1\n---\nversion: 1\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(sourcesRoot, client.DefaultRegistry())
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("Load() error = %v, want multiple-document rejection", err)
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
