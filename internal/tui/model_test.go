package tui

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
)

func TestSourceRowToggleEnablesEveryCompatibleSkill(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "worktrunk"), "worktrunk")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "wt-switch-create"), "wt-switch-create")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
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

func TestSkillToggleAllClientsEnablesAndDisablesEveryCompatibleProjection(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	model = selectFirstSkill(model)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("enable all clients: %v", model.err)
	}

	for _, target := range []string{
		filepath.Join(projectRoot, ".agents", "skills", "portable"),
		filepath.Join(projectRoot, ".claude", "skills", "portable"),
		filepath.Join(projectRoot, ".gemini", "skills", "portable"),
	} {
		if _, err := os.Readlink(target); err != nil {
			t.Fatalf("all-client toggle did not enable %s: %v", target, err)
		}
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("disable all clients: %v", model.err)
	}
	for _, target := range []string{
		filepath.Join(projectRoot, ".agents", "skills", "portable"),
		filepath.Join(projectRoot, ".claude", "skills", "portable"),
		filepath.Join(projectRoot, ".gemini", "skills", "portable"),
	} {
		assertNotExist(t, target)
	}
}

func TestSkillToggleAllClientsPreflightsEveryClientAtomically(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(projectRoot, ".claude", "skills", "portable")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	model = selectFirstSkill(model)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err == nil {
		t.Fatal("all-client toggle succeeded despite an unmanaged client conflict")
	}
	assertNotExist(t, filepath.Join(projectRoot, ".agents", "skills", "portable"))
	assertNotExist(t, filepath.Join(projectRoot, ".gemini", "skills", "portable"))
	data, err := os.ReadFile(conflict)
	if err != nil || string(data) != "user owned\n" {
		t.Fatalf("conflict was not preserved: %q, %v", data, err)
	}
}

func TestViewKeepsSourceGroupsCollapsedUntilExpanded(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "worktrunk"), "worktrunk")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	view := model.View().Content
	for _, fragment := range []string{"NAME / SKILL", "CODEX", "CLAUDE", "GEMINI", "shared/worktrunk"} {
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

func TestSourceRowsUseComparableKindAndNameColumns(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "android-skills", "skills", "android-cli"), "android-cli")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	view := model.View().Content
	for _, fragment := range []string{"SOURCE", "NAME / SKILL", "LOCAL", "REMOTE", "shared/android-skills"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("source table does not contain %q:\n%s", fragment, view)
		}
	}
}

func TestLanguageSelectorIsVisibleAndTogglesTheWholeTUI(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	view := model.View().Content
	for _, option := range []string{"EN", "中"} {
		if !strings.Contains(view, option) {
			t.Fatalf("language selector does not contain %q:\n%s", option, view)
		}
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	view = model.View().Content
	for _, translated := range []string{"全部", "来源", "就绪"} {
		if !strings.Contains(view, translated) {
			t.Fatalf("language toggle did not render %q:\n%s", translated, view)
		}
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	if view = updated.(Model).View().Content; !strings.Contains(view, "all") {
		t.Fatalf("second language toggle did not restore English:\n%s", view)
	}
}

func TestSourceToggleSkipsSkillsIncompatibleWithSelectedClient(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "mixed", "skills", "universal"), "universal")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "mixed", "skills", "codex-only"), "codex-only")
	config := "version: 1\noverrides:\n  vendor-shared/mixed/skills/codex-only:\n    targets: [codex]\n    reason: Uses Codex-only workflow APIs.\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
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
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.Chinese))
	view := model.View().Content
	for _, fragment := range []string{"项目资源 · 用户级系统提示词", "名称 / SKILL", "就绪"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("Chinese view does not contain %q:\n%s", fragment, view)
		}
	}
	model.tab = tabMCP
	if view := model.View().Content; !strings.Contains(view, "资源") {
		t.Fatalf("Chinese MCP view does not localize the resource header:\n%s", view)
	}
}

func TestPolishedViewFitsNarrowTerminalAndSurfacesAllClientAction(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	model = selectFirstSkill(model)
	resized, _ := model.Update(tea.WindowSizeMsg{Width: 48, Height: 24})
	view := resized.(Model).View().Content
	for _, fragment := range []string{"◆", "╭", "╰", "▌", "○", "all clients"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("polished view does not contain %q:\n%s", fragment, view)
		}
	}
	if width := lipgloss.Width(view); width > 48 {
		t.Fatalf("narrow view width = %d, want <= 48:\n%s", width, view)
	}
	if height := lipgloss.Height(view); height > 24 {
		t.Fatalf("narrow view height = %d, want <= 24:\n%s", height, view)
	}
	if description := defaultKeyMap(i18n.New(i18n.Chinese)).ToggleAll.Help().Desc; description != "全部客户端" {
		t.Fatalf("Chinese all-client help = %q", description)
	}
}

func TestViewPaintsTheEntireTerminalCanvasInLightAndDarkModes(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name       string
		background color.Color
	}{
		{name: "dark", background: color.Black},
		{name: "light", background: color.White},
	} {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
			updated, _ := model.Update(tea.BackgroundColorMsg{Color: tt.background})
			updated, _ = updated.(Model).Update(tea.WindowSizeMsg{Width: 48, Height: 24})
			view := updated.(Model).View()
			if view.BackgroundColor == nil {
				t.Fatal("terminal background remains transparent")
			}
			if width := lipgloss.Width(view.Content); width != 48 {
				t.Fatalf("canvas width = %d, want 48", width)
			}
			if height := lipgloss.Height(view.Content); height != 24 {
				t.Fatalf("canvas height = %d, want 24", height)
			}
		})
	}
}

func TestNarrowViewScrollsDynamicClientColumnsWithSelection(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {ProjectSkillsDir: ".pi/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(sourcesRoot, registry)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
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
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
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

func TestResourceTabsToggleMCPAndSystemPrompts(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	mcpCatalog := mcp.Catalog{Servers: map[string]mcp.Server{
		"context7": {Name: "context7", Transport: mcp.TransportStdio, Command: "npx"},
	}}
	promptRoot := t.TempDir()
	promptPath := filepath.Join(promptRoot, "claude-prompt", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("# Claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompts, err := systemprompt.Discover(promptRoot, loaded.Clients)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English), Resources{
		MCPCatalog:    mcpCatalog,
		MCPManager:    mcp.NewManager(projectRoot, mcpCatalog, loaded.Clients),
		Prompts:       prompts,
		PromptManager: systemprompt.NewManager(userHome, loaded.Clients),
		UserHome:      userHome,
	})

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if !strings.Contains(model.View().Content, "context7") {
		t.Fatalf("MCP tab did not render server:\n%s", model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if data, err := os.ReadFile(filepath.Join(projectRoot, ".codex", "config.toml")); err != nil || !strings.Contains(string(data), "context7") {
		t.Fatalf("MCP tab did not enable Codex server: %s, %v", data, err)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if view := model.View().Content; !strings.Contains(view, "USER") || strings.Contains(view, "PROJECT  ") {
		t.Fatalf("System Prompts tab did not render the user-global scope:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if _, err := os.Readlink(filepath.Join(userHome, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("System Prompts tab did not project CLAUDE.md: %v", err)
	}
	view := model.View().Content
	for _, label := range []string{"Skills", "MCP", "System Prompts"} {
		if !strings.Contains(view, label) {
			t.Fatalf("tab bar is missing %q:\n%s", label, view)
		}
	}
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

func selectFirstSkill(model Model) Model {
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ = updated.(Model).Update(tea.KeyPressMsg{Code: tea.KeyDown})
	return updated.(Model)
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s unexpectedly exists: %v", path, err)
	}
}
