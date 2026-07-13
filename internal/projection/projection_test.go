package projection

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestSetEnabledAppliesSourceGroupAtomically(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	skills := []catalog.Skill{
		newSkill(t, sourceRoot, "worktrunk"),
		newSkill(t, sourceRoot, "wt-switch-create"),
	}
	manager := newManager(projectRoot, skills)

	conflictingPath := filepath.Join(projectRoot, ".agents", "skills", "wt-switch-create")
	if err := os.MkdirAll(conflictingPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := manager.SetEnabled(skills, catalog.ClientCodex, true)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("SetEnabled() error = %v, want ConflictError", err)
	}
	assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", "worktrunk"))

	if err := os.Remove(conflictingPath); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEnabled(skills, catalog.ClientCodex, true); err != nil {
		t.Fatalf("enable group: %v", err)
	}
	for _, skill := range skills {
		assertLinkTarget(t, filepath.Join(projectRoot, ".agents", "skills", skill.Name), skill.Path)
	}

	if err := manager.SetEnabled(skills, catalog.ClientCodex, false); err != nil {
		t.Fatalf("disable group: %v", err)
	}
	for _, skill := range skills {
		assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", skill.Name))
	}
}

func TestApplyPreflightsEveryClientBeforeMutating(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	skill := newSkill(t, sourceRoot, "worktrunk")
	skill.Targets[catalog.ClientClaude] = true
	manager := newManager(projectRoot, []catalog.Skill{skill})

	conflictingPath := filepath.Join(projectRoot, ".claude", "skills", skill.Name)
	if err := os.MkdirAll(conflictingPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := manager.Apply([]Operation{
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientCodex, Enabled: true},
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientClaude, Enabled: true},
	})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Apply() error = %v, want ConflictError", err)
	}
	assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", skill.Name))
}

func TestSetEnabledUsesConfiguredClientProjectionDirectory(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	skill := newSkill(t, sourceRoot, "portable")
	skill.Targets = map[catalog.Client]bool{catalog.Client("pi"): true}
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {ProjectSkillsDir: ".pi/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(projectRoot, catalog.Catalog{
		Clients: registry,
		Sources: []catalog.Source{{ID: "local-shared", Skills: []catalog.Skill{skill}}},
	})

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.Client("pi"), true); err != nil {
		t.Fatal(err)
	}
	assertLinkTarget(t, filepath.Join(projectRoot, ".pi", "skills", "portable"), skill.Path)
}

func TestStateReportsEnabledLinkThatBecameIncompatible(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	skill := newSkill(t, sourceRoot, "codex-workflow")
	manager := newManager(projectRoot, []catalog.Skill{skill})
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}

	skill.Targets[catalog.ClientCodex] = false
	state, err := manager.State(skill, catalog.ClientCodex)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := state, StateIncompatibleEnabled; got != want {
		t.Fatalf("State() = %q, want %q", got, want)
	}
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, false); err != nil {
		t.Fatalf("disable stale projection: %v", err)
	}
	assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", skill.Name))
}

func TestManagedProvidersWithTheSameNameCanBeSwitched(t *testing.T) {
	projectRoot := t.TempDir()
	localRoot := t.TempDir()
	vendorRoot := t.TempDir()
	local := newSkill(t, localRoot, "shared-name")
	local.ID = "local-shared/shared-name"
	local.SourceID = "local-shared"
	vendor := newSkill(t, vendorRoot, "shared-name")
	vendor.ID = "vendor-shared/upstream/skills/shared-name"
	vendor.SourceID = "vendor-shared/upstream"
	manager := newManager(projectRoot, []catalog.Skill{local, vendor})

	if err := manager.SetEnabled([]catalog.Skill{local}, catalog.ClientCodex, true); err != nil {
		t.Fatalf("enable local provider: %v", err)
	}
	if state, err := manager.State(vendor, catalog.ClientCodex); err != nil || state != StateDisabled {
		t.Fatalf("vendor state = %q, %v; want disabled", state, err)
	}

	if err := manager.SetEnabled([]catalog.Skill{vendor}, catalog.ClientCodex, true); err != nil {
		t.Fatalf("switch to vendor provider: %v", err)
	}
	assertLinkTarget(t, filepath.Join(projectRoot, ".agents", "skills", "shared-name"), vendor.Path)
	if state, err := manager.State(local, catalog.ClientCodex); err != nil || state != StateDisabled {
		t.Fatalf("local state = %q, %v; want disabled", state, err)
	}
	if state, err := manager.State(vendor, catalog.ClientCodex); err != nil || state != StateEnabled {
		t.Fatalf("vendor state = %q, %v; want enabled", state, err)
	}
}

func TestManagedProviderSwitchIsPreflightedWithTheWholeGroup(t *testing.T) {
	projectRoot := t.TempDir()
	localRoot := t.TempDir()
	vendorRoot := t.TempDir()
	local := newSkill(t, localRoot, "shared-name")
	vendor := newSkill(t, vendorRoot, "shared-name")
	second := newSkill(t, vendorRoot, "blocked")
	manager := newManager(projectRoot, []catalog.Skill{local, vendor, second})

	if err := manager.SetEnabled([]catalog.Skill{local}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".agents", "skills", "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := manager.SetEnabled([]catalog.Skill{vendor, second}, catalog.ClientCodex, true)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("switch group error = %v, want ConflictError", err)
	}
	assertLinkTarget(t, filepath.Join(projectRoot, ".agents", "skills", "shared-name"), local.Path)
}

func TestUnmanagedSymlinkStillBlocksProviderEnable(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	unmanagedRoot := t.TempDir()
	skill := newSkill(t, sourceRoot, "shared-name")
	manager := newManager(projectRoot, []catalog.Skill{skill})
	linkPath := filepath.Join(projectRoot, ".agents", "skills", skill.Name)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unmanagedRoot, linkPath); err != nil {
		t.Fatal(err)
	}

	err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("enable error = %v, want ConflictError", err)
	}
	assertLinkTarget(t, linkPath, unmanagedRoot)
}

func TestDisablePreservesLinkReplacedAfterPreflight(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	skill := newSkill(t, sourceRoot, "portable")
	manager := newManager(projectRoot, []catalog.Skill{skill})
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectRoot, ".agents", "skills", "portable")
	manager.beforeApply = func(next change) {
		if next.action != removeLink {
			return
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("user-owned\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, false)
	if err == nil {
		t.Fatal("disable succeeded after the managed link was replaced")
	}
	contents, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("replacement file was removed: %v", readErr)
	}
	if !strings.Contains(string(contents), "user-owned") {
		t.Fatalf("replacement contents = %q", contents)
	}
}

func TestRollbackPreservesLinksReplacedDuringApply(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	first := newSkill(t, sourceRoot, "a")
	second := newSkill(t, sourceRoot, "b")
	manager := newManager(projectRoot, []catalog.Skill{first, second})
	firstTarget := filepath.Join(projectRoot, ".agents", "skills", "a")
	secondTarget := filepath.Join(projectRoot, ".agents", "skills", "b")
	manager.beforeApply = func(next change) {
		if next.path != secondTarget {
			return
		}
		if err := os.Remove(firstTarget); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(firstTarget, []byte("first user file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(secondTarget, []byte("second user file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err := manager.SetEnabled([]catalog.Skill{first, second}, catalog.ClientCodex, true)
	if err == nil {
		t.Fatal("enable succeeded after targets changed during apply")
	}
	for path, want := range map[string]string{
		firstTarget:  "first user file",
		secondTarget: "second user file",
	} {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("replacement file %s was removed: %v", path, readErr)
		}
		if !strings.Contains(string(contents), want) {
			t.Fatalf("replacement contents at %s = %q", path, contents)
		}
	}
}

func newManager(projectRoot string, skills []catalog.Skill) Manager {
	return New(projectRoot, catalog.Catalog{
		Clients: client.DefaultRegistry(),
		Sources: []catalog.Source{{ID: "test", Skills: skills}},
	})
}

func newSkill(t *testing.T, root, name string) catalog.Skill {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return catalog.Skill{
		ID:       "vendor/worktrunk/skills/" + name,
		Name:     name,
		SourceID: "vendor/worktrunk",
		Path:     dir,
		Targets:  map[catalog.Client]bool{catalog.ClientCodex: true},
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or returned unexpected error: %v", path, err)
	}
}

func assertLinkTarget(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("readlink %s: %v", path, err)
	}
	if got != want {
		t.Fatalf("readlink %s = %q, want %q", path, got, want)
	}
}
