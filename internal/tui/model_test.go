package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/projection"
)

func TestSourceRowToggleEnablesEveryCompatibleSkill(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "worktrunk", "skills", "worktrunk"), "worktrunk")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "worktrunk", "skills", "wt-switch-create"), "wt-switch-create")
	loaded, err := catalog.Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded.Clients), nil, i18n.New(i18n.English))
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("toggle source: %v", model.err)
	}

	for _, name := range []string{"worktrunk", "wt-switch-create"} {
		link := filepath.Join(projectRoot, ".agents", "skills", name)
		if _, err := os.Readlink(link); err != nil {
			t.Fatalf("source toggle did not enable %s: %v", name, err)
		}
	}
}

func TestViewKeepsSourceGroupsCollapsedUntilExpanded(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "worktrunk", "skills", "worktrunk"), "worktrunk")
	loaded, err := catalog.Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded.Clients), nil, i18n.New(i18n.English))
	view := model.View().Content
	for _, fragment := range []string{"SOURCE / SKILL", "CODEX", "CLAUDE", "GEMINI", "vendor/worktrunk"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("collapsed view does not contain %q:\n%s", fragment, view)
		}
	}
	if strings.Contains(view, "Manage worktrunk") {
		t.Fatalf("collapsed view unexpectedly renders child details:\n%s", view)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = updated.(Model).View().Content
	if !strings.Contains(view, "Manage worktrunk") {
		t.Fatalf("expanded view does not render child skill:\n%s", view)
	}
}

func TestSourceToggleSkipsSkillsIncompatibleWithSelectedClient(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "mixed", "skills", "universal"), "universal")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "mixed", "skills", "codex-only"), "codex-only")
	config := "version: 1\noverrides:\n  vendor/mixed/skills/codex-only:\n    targets: [codex]\n    reason: Uses Codex-only workflow APIs.\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded.Clients), nil, i18n.New(i18n.English))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	updated, _ = updated.(Model).Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("toggle source: %v", model.err)
	}
	if _, err := os.Readlink(filepath.Join(projectRoot, ".claude", "skills", "universal")); err != nil {
		t.Fatalf("compatible skill was not enabled: %v", err)
	}
	assertNotExist(t, filepath.Join(projectRoot, ".claude", "skills", "codex-only"))
}

func TestViewUsesSharedChineseTranslations(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded.Clients), nil, i18n.New(i18n.Chinese))
	view := model.View().Content
	for _, fragment := range []string{"项目级 Skill 投影管理", "来源 / SKILL", "就绪"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("Chinese view does not contain %q:\n%s", fragment, view)
		}
	}
}

func TestNarrowViewScrollsDynamicClientColumnsWithSelection(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")
	config := "version: 1\nclients:\n  pi:\n    projectSkillsDir: .pi/skills\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded.Clients), nil, i18n.New(i18n.English))
	resized, _ := model.Update(tea.WindowSizeMsg{Width: 48, Height: 24})
	model = resized.(Model)
	for range 3 {
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
		model = updated.(Model)
	}
	view := model.View().Content
	if !strings.Contains(view, "PI") || !strings.Contains(view, "‹") {
		t.Fatalf("narrow view did not reveal selected dynamic client:\n%s", view)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if _, err := os.Readlink(filepath.Join(projectRoot, ".pi", "skills", "portable")); err != nil {
		t.Fatalf("dynamic client toggle was not projected: %v", err)
	}
}

func TestSourceToggleCleansProjectionThatBecameIncompatible(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded.Clients), nil, i18n.New(i18n.English))
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	link := filepath.Join(projectRoot, ".agents", "skills", "portable")
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("initial projection was not enabled: %v", err)
	}

	model.catalog.Sources[0].Skills[0].Targets[catalog.ClientCodex] = false
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	assertNotExist(t, link)
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + name + "\ndescription: Manage " + name + ".\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s unexpectedly exists: %v", path, err)
	}
}
