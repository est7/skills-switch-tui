package projection

import (
	"errors"
	"os"
	"path/filepath"
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
	manager := New(projectRoot, client.DefaultRegistry())

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
	manager := New(projectRoot, client.DefaultRegistry())

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
	registry, err := client.NewRegistry(map[client.ID]string{"pi": ".pi/skills"})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(projectRoot, registry)

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.Client("pi"), true); err != nil {
		t.Fatal(err)
	}
	assertLinkTarget(t, filepath.Join(projectRoot, ".pi", "skills", "portable"), skill.Path)
}

func TestStateReportsEnabledLinkThatBecameIncompatible(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	skill := newSkill(t, sourceRoot, "codex-workflow")
	manager := New(projectRoot, client.DefaultRegistry())
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
