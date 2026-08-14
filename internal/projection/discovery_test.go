package projection

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestDiscoverUnmanagedSkillsReportsOnlyRealSkillDirectoriesInStableOrder(t *testing.T) {
	projectRoot := t.TempDir()
	userHome := t.TempDir()
	codexProject := filepath.Join(projectRoot, ".agents", "skills")
	claudeProject := filepath.Join(projectRoot, ".claude", "skills")
	codexGlobal := filepath.Join(userHome, ".agents", "skills")

	newSkill(t, codexProject, "zeta")
	newSkill(t, codexProject, "alpha")
	newSkill(t, claudeProject, "beta")
	newSkill(t, codexGlobal, "global-tool")
	if err := os.MkdirAll(filepath.Join(codexProject, "not-a-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codexProject, "manifest-directory", "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	managed := newSkill(t, t.TempDir(), "managed")
	if err := os.Symlink(managed.Path, filepath.Join(codexProject, "managed")); err != nil {
		t.Fatal(err)
	}

	manager := NewWithUserHome(projectRoot, userHome, catalog.Catalog{Clients: client.DefaultRegistry()})
	got, err := manager.DiscoverUnmanagedSkills(ScopeProject, ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	want := []UnmanagedSkill{
		{Client: catalog.ClientClaude, Scope: ScopeProject, Name: "beta", Path: filepath.Join(claudeProject, "beta")},
		{Client: catalog.ClientCodex, Scope: ScopeGlobal, Name: "global-tool", Path: filepath.Join(codexGlobal, "global-tool")},
		{Client: catalog.ClientCodex, Scope: ScopeProject, Name: "alpha", Path: filepath.Join(codexProject, "alpha")},
		{Client: catalog.ClientCodex, Scope: ScopeProject, Name: "zeta", Path: filepath.Join(codexProject, "zeta")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverUnmanagedSkills() = %#v, want %#v", got, want)
	}
}

func TestDiscoverUnmanagedSkillsTreatsMissingTargetsAsEmpty(t *testing.T) {
	projectRoot := t.TempDir()
	geminiRoot := filepath.Join(projectRoot, ".gemini")
	if err := os.MkdirAll(geminiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(geminiRoot, "skills")); err != nil {
		t.Fatal(err)
	}
	manager := NewWithUserHome(projectRoot, t.TempDir(), catalog.Catalog{Clients: client.DefaultRegistry()})
	got, err := manager.DiscoverUnmanagedSkills(ScopeProject, ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("DiscoverUnmanagedSkills() = %#v, want non-nil empty result", got)
	}
}
