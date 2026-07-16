package tui

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/i18n"
	"github.com/est7/skills-switch-tui/internal/mcp"
	"github.com/est7/skills-switch-tui/internal/projection"
	"github.com/est7/skills-switch-tui/internal/source"
	"github.com/est7/skills-switch-tui/internal/systemprompt"
	"github.com/est7/skills-switch-tui/internal/userresource"
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

func TestSkillScopeKeyPromotesToGlobalAndLocksProjectToggle(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	userHome := t.TempDir()
	skillPath := filepath.Join(sourcesRoot, "local", "shared", "core")
	writeSkill(t, skillPath, "core")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	manager := projection.NewWithUserHome(projectRoot, userHome, loaded)
	model := NewModel(loaded, projectRoot, manager, nil, i18n.New(i18n.English), Resources{UserHome: userHome})

	updated, _ := model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	model = updated.(Model)
	if model.skillScope != projection.ScopeGlobal || !model.keys.Scope.Enabled() {
		t.Fatalf("scope key did not select global scope: %q", model.skillScope)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("global toggle: %v", model.err)
	}
	globalLink := filepath.Join(userHome, ".agents", "skills", "core")
	if target, err := os.Readlink(globalLink); err != nil || filepath.Clean(target) != filepath.Clean(skillPath) {
		t.Fatalf("global link = %q, %v", target, err)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	model = updated.(Model)
	if model.skillScope != projection.ScopeProject {
		t.Fatalf("scope key did not return to project scope: %q", model.skillScope)
	}
	if cell, _ := model.stateCell(loaded.Sources[0].Skills[0], catalog.ClientCodex); cell != "G" {
		t.Fatalf("project cell = %q, want global lock marker", cell)
	}
	if cell, _ := model.cell(row{kind: sourceRow, sourceIndex: 0}, catalog.ClientCodex); cell != "G 1/1" {
		t.Fatalf("project source cell = %q, want global summary marker", cell)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err == nil || !strings.Contains(model.err.Error(), "globally configured") {
		t.Fatalf("project toggle was not locked: %v", model.err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "core")); !os.IsNotExist(err) {
		t.Fatalf("locked project toggle created a link: %v", err)
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

func TestSourceToggleAllClientsEnablesAndDisablesEveryCompatibleProjection(t *testing.T) {
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
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("enable source for all clients: %v", model.err)
	}
	for _, target := range []string{
		filepath.Join(projectRoot, ".agents", "skills", "universal"),
		filepath.Join(projectRoot, ".claude", "skills", "universal"),
		filepath.Join(projectRoot, ".gemini", "skills", "universal"),
		filepath.Join(projectRoot, ".agents", "skills", "codex-only"),
	} {
		if _, err := os.Readlink(target); err != nil {
			t.Fatalf("source all-client toggle did not enable %s: %v", target, err)
		}
	}
	assertNotExist(t, filepath.Join(projectRoot, ".claude", "skills", "codex-only"))
	assertNotExist(t, filepath.Join(projectRoot, ".gemini", "skills", "codex-only"))

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("disable source for all clients: %v", model.err)
	}
	for _, clientDir := range []string{".agents", ".claude", ".gemini"} {
		for _, skill := range []string{"universal", "codex-only"} {
			assertNotExist(t, filepath.Join(projectRoot, clientDir, "skills", skill))
		}
	}
}

func TestSourceToggleAllClientsPreflightsTheWholeMatrixAtomically(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "bundle", "skills", "first"), "first")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "bundle", "skills", "second"), "second")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(projectRoot, ".claude", "skills", "second")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err == nil {
		t.Fatal("source all-client toggle succeeded despite an unmanaged conflict")
	}
	for _, target := range []string{
		filepath.Join(projectRoot, ".agents", "skills", "first"),
		filepath.Join(projectRoot, ".agents", "skills", "second"),
		filepath.Join(projectRoot, ".claude", "skills", "first"),
		filepath.Join(projectRoot, ".gemini", "skills", "first"),
		filepath.Join(projectRoot, ".gemini", "skills", "second"),
	} {
		assertNotExist(t, target)
	}
	data, err := os.ReadFile(conflict)
	if err != nil || string(data) != "user owned\n" {
		t.Fatalf("conflict was not preserved: %q, %v", data, err)
	}
}

func TestResourceTabsExposeOnlyClientsWithMatchingAdapters(t *testing.T) {
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"mcp-only":   {ProjectMCPFile: ".mcp-only.json", ProjectMCPFormat: client.MCPClaudeJSON},
		"skill-only": {ProjectSkillsDir: ".skill-only/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := catalog.Catalog{Clients: registry}
	model := NewModel(loaded, t.TempDir(), projection.New(t.TempDir(), loaded), nil, i18n.New(i18n.English))

	if got := model.clientsForTab(); containsClient(got, "mcp-only") || !containsClient(got, "skill-only") {
		t.Fatalf("Skill tab clients = %v", got)
	}
	model.tab = tabMCP
	if got := model.clientsForTab(); !containsClient(got, "mcp-only") || containsClient(got, "skill-only") {
		t.Fatalf("MCP tab clients = %v", got)
	}
}

func containsClient(clients []client.ID, target client.ID) bool {
	for _, clientID := range clients {
		if clientID == target {
			return true
		}
	}
	return false
}

func TestEveryUserResourceDescriptorHasExactlyOneTab(t *testing.T) {
	counts := make(map[userresource.Kind]int)
	for _, descriptor := range tabDescriptors {
		if descriptor.userResource {
			counts[descriptor.resourceKind]++
		}
	}
	for _, descriptor := range userresource.Descriptors() {
		if counts[descriptor.Kind] != 1 {
			t.Fatalf("resource kind %s has %d TUI tabs, want 1", descriptor.Kind, counts[descriptor.Kind])
		}
		delete(counts, descriptor.Kind)
	}
	if len(counts) != 0 {
		t.Fatalf("TUI tabs reference unknown resource kinds: %v", counts)
	}
}

func TestUpdateAllRefreshesEveryVendorSource(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	for _, name := range []string{"first", "second"} {
		writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", name, "skills", name), name)
	}
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	git := &tuiSourceGit{responses: make(map[string]string)}
	for _, candidate := range loaded.Sources {
		git.responses[candidate.Path+"|reset --hard HEAD"] = "HEAD is now at aaaaaaaa current\n"
		git.responses[candidate.Path+"|clean -ffdx"] = ""
		git.responses[candidate.Path+"|rev-parse HEAD"] = "aaaaaaaa\n"
		git.responses[candidate.Path+"|ls-remote origin refs/heads/main"] = "aaaaaaaa\trefs/heads/main\n"
	}
	updater := source.Manager{RepositoryRoot: t.TempDir(), SkillsRoot: sourcesRoot, Git: git}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), &updater, i18n.New(i18n.English))

	updated, command := model.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	model = updated.(Model)
	if command == nil || !model.updating {
		t.Fatal("update-all key did not start an update")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("update all vendors: %v", model.err)
	}
	for _, candidate := range loaded.Sources {
		if !git.called(candidate.Path + "|reset --hard HEAD") {
			t.Errorf("update all did not hard-reset %s", candidate.ID)
		}
	}
}

func TestUpdateAllContinuesCleanSourcesAndReportsResetFailure(t *testing.T) {
	repositoryRoot := t.TempDir()
	sourcesRoot := filepath.Join(repositoryRoot, "resources", "skills")
	projectRoot := t.TempDir()
	for _, name := range []string{"broken", "clean"} {
		writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", name, "skills", name), name)
	}
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	broken := loaded.Sources[0]
	clean := loaded.Sources[1]
	git := &tuiSourceGit{responses: map[string]string{
		clean.Path + "|reset --hard HEAD":                                                   "HEAD is now at aaaaaaaa current\n",
		clean.Path + "|clean -ffdx":                                                         "",
		clean.Path + "|rev-parse HEAD":                                                      "aaaaaaaa\n",
		clean.Path + "|ls-remote origin refs/heads/main":                                    "bbbbbbbb\trefs/heads/main\n",
		repositoryRoot + "|submodule update --init -- resources/skills/vendor/shared/clean": "",
		clean.Path + "|fetch --no-tags origin refs/heads/main":                              "",
		clean.Path + "|reset --hard bbbbbbbb":                                               "",
		clean.Path + "|rev-parse --verify HEAD":                                             "bbbbbbbb\n",
		clean.Path + "|sparse-checkout disable":                                             "",
		clean.Path + "|sparse-checkout init --cone":                                         "",
		clean.Path + "|sparse-checkout set skills":                                          "",
	}}
	updater := source.Manager{RepositoryRoot: repositoryRoot, SkillsRoot: sourcesRoot, Git: git}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), &updater, i18n.New(i18n.English))

	updated, command := model.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)

	if model.err == nil {
		t.Fatal("update all did not report the reset failure")
	}
	for _, fragment := range []string{broken.ID, broken.Path, "reset read-only checkout"} {
		if !strings.Contains(model.err.Error(), fragment) {
			t.Fatalf("update error %q does not contain %q", model.err, fragment)
		}
	}
	if !strings.Contains(model.status, "Updated 1 source(s)") {
		t.Fatalf("partial update status = %q err=%v calls=%v", model.status, model.err, git.calls)
	}
	if !git.called(repositoryRoot + "|submodule update --init -- resources/skills/vendor/shared/clean") {
		t.Fatalf("clean source was not updated; calls = %v", git.calls)
	}
}

func TestUpdateReloadsChangedSkillTotalsFromTheVendorSnapshot(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	sourceRoot := filepath.Join(sourcesRoot, "vendor", "shared", "changing", "skills")
	removedPath := filepath.Join(sourceRoot, "removed")
	writeSkill(t, removedPath, "removed")
	writeSkill(t, filepath.Join(sourceRoot, "kept"), "kept")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	manager := projection.New(projectRoot, loaded)
	changing := loaded.Sources[0]
	if err := manager.SetEnabled(changing.Skills, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, manager, nil, i18n.New(i18n.English))

	if err := os.RemoveAll(removedPath); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(sourceRoot, "added"), "added")
	updated, _ := model.Update(updateFinishedMsg{results: []source.UpdateResult{{SourceID: changing.ID, Changed: true}}})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("reload changed vendor snapshot: %v", model.err)
	}
	if got := model.tabCount(tabSkills); got != 2 {
		t.Fatalf("skill total after update = %d, want 2", got)
	}
	value, _ := model.cell(row{kind: sourceRow, sourceIndex: 0}, catalog.ClientCodex)
	if value != "1/2" {
		t.Fatalf("Codex source count after removed/kept/added update = %s, want 1/2", value)
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
	for _, fragment := range []string{"项目资源 · 用户级 Agent 文件", "名称 / SKILL", "就绪"} {
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

func TestNarrowTabStripKeepsActiveResourceVisible(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	resized, _ := model.Update(tea.WindowSizeMsg{Width: 48, Height: 24})
	model = resized.(Model)
	model.tab = tabHooks
	tabs := model.renderTabs()
	for _, fragment := range []string{"Hooks", "‹", "›"} {
		if !strings.Contains(tabs, fragment) {
			t.Fatalf("narrow tab strip does not contain %q:\n%s", fragment, tabs)
		}
	}
	if width := lipgloss.Width(tabs); width > model.contentWidth() {
		t.Fatalf("tab strip width = %d, content width = %d", width, model.contentWidth())
	}
}

func TestScrollHintPreservesPositionInBothDirections(t *testing.T) {
	model := NewModel(catalog.Catalog{}, t.TempDir(), projection.Manager{}, nil, i18n.New(i18n.English))
	hint := model.renderScrollHint(3, 7, 10, 40)
	for _, fragment := range []string{"↑ 3 above", "↓ 3 more"} {
		if !strings.Contains(hint, fragment) {
			t.Fatalf("scroll hint does not contain %q: %s", fragment, hint)
		}
	}

	model.translator = i18n.New(i18n.Chinese)
	hint = model.renderScrollHint(2, 5, 8, 40)
	for _, fragment := range []string{"↑ 上方 2 项", "↓ 还有 3 项"} {
		if !strings.Contains(hint, fragment) {
			t.Fatalf("Chinese scroll hint does not contain %q: %s", fragment, hint)
		}
	}
}

func TestErrorStatusStaysInsideFooter(t *testing.T) {
	model := NewModel(catalog.Catalog{}, t.TempDir(), projection.Manager{}, nil, i18n.New(i18n.English))
	resized, _ := model.Update(tea.WindowSizeMsg{Width: 48, Height: 24})
	model = resized.(Model)
	model.status = "Update failed"
	model.err = errors.New(strings.Repeat("remote repository unavailable ", 5))
	statusLine := strings.SplitN(model.renderFooter(), "\n", 2)[0]
	if !strings.Contains(statusLine, "×") || !strings.Contains(statusLine, "…") {
		t.Fatalf("error footer lacks semantic icon or truncation:\n%s", statusLine)
	}
	if width := lipgloss.Width(statusLine); width > model.contentWidth() {
		t.Fatalf("error footer width = %d, content width = %d", width, model.contentWidth())
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

func TestResourceTabsToggleMCPCommandsHooksAndSystemPrompts(t *testing.T) {
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
	commandRoot := t.TempDir()
	commandPath := filepath.Join(commandRoot, "shared", "remember.md")
	hookRoot := t.TempDir()
	hookPath := filepath.Join(hookRoot, "claude-only", "audit.sh")
	for _, path := range []string{commandPath, hookPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commands, err := userresource.Discover(commandRoot, userresource.KindCommand, loaded.Clients)
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := userresource.Discover(hookRoot, userresource.KindHook, loaded.Clients)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English), Resources{
		MCPCatalog:    mcpCatalog,
		MCPManager:    mcp.NewManager(projectRoot, mcpCatalog, loaded.Clients),
		Prompts:       prompts,
		PromptManager: systemprompt.NewManager(userHome, loaded.Clients),
		UserResources: map[userresource.Kind]UserResourceSet{
			userresource.KindCommand: {Catalog: commands, Manager: userresource.NewManager(projectRoot, loaded.Clients)},
			userresource.KindHook:    {Catalog: hooks, Manager: userresource.NewManager(projectRoot, loaded.Clients)},
		},
		UserHome: userHome,
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
	if !strings.Contains(model.View().Content, "shared/remember.md") {
		t.Fatalf("Commands tab did not render command:\n%s", model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if _, err := os.Readlink(filepath.Join(projectRoot, ".codex", "prompts", "remember.md")); err != nil {
		t.Fatalf("Commands tab did not project command: %v", err)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if !strings.Contains(model.View().Content, "claude-only/audit.sh") {
		t.Fatalf("Hooks tab did not render hook:\n%s", model.View().Content)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if _, err := os.Readlink(filepath.Join(projectRoot, ".claude", "hooks", "audit.sh")); err != nil {
		t.Fatalf("Hooks tab did not project hook: %v", err)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	if view := model.View().Content; !strings.Contains(view, "USER") || strings.Contains(view, "PROJECT  ") {
		t.Fatalf("System Prompts tab did not render the user-global scope:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if _, err := os.Readlink(filepath.Join(userHome, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("System Prompts tab did not project CLAUDE.md: %v", err)
	}
	view := model.View().Content
	for _, label := range []string{"Skills", "MCP", "Commands", "Hooks", "Agents", "Output Styles", "System Prompts"} {
		if !strings.Contains(view, label) {
			t.Fatalf("tab bar is missing %q:\n%s", label, view)
		}
	}
}

func TestPromptTabBuildKeyBuildsCodexPromptWithoutEnablingIt(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "portable"), "portable")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	promptRoot := t.TempDir()
	base := filepath.Join(promptRoot, "codex-prompt", "AGENTS.md")
	rule := filepath.Join(promptRoot, "codex-prompt", "rules", "10-core.md")
	for path, contents := range map[string]string{base: "# Base\n", rule: "## Core\n"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prompts, err := systemprompt.Discover(promptRoot, loaded.Clients)
	if err != nil {
		t.Fatal(err)
	}
	manager := systemprompt.NewManager(userHome, loaded.Clients)
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English), Resources{
		Prompts: prompts, PromptManager: manager, UserHome: userHome,
	})
	model.tab = tabSystemPrompts
	model.syncContextKeys()
	if !model.keys.Build.Enabled() {
		t.Fatal("prompt tab build key is disabled")
	}
	if help := model.keys.Build.Help(); help.Key != "b" || help.Desc != "build prompt" {
		t.Fatalf("unexpected prompt build help: %+v", help)
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	model = updated.(Model)
	if command == nil || !model.updating {
		t.Fatal("prompt build key did not start a build")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.err != nil || model.updating {
		t.Fatalf("prompt build failed: %v", model.err)
	}
	group, _ := prompts.Group("codex-prompt")
	if _, err := os.Stat(manager.GeneratedPath(group)); err != nil {
		t.Fatalf("prompt build did not create output: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".codex", "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("build key unexpectedly enabled the prompt: %v", err)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	if model.err != nil {
		t.Fatal(model.err)
	}
	if err := os.WriteFile(rule, []byte("## Core changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(group); err != nil || state != systemprompt.StateStale {
		t.Fatalf("prompt state after edit = %q, %v", state, err)
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if state, err := manager.State(group); err != nil || state != systemprompt.StateEnabled {
		t.Fatalf("prompt state after b rebuild = %q, %v", state, err)
	}
}

type tuiSourceGit struct {
	responses map[string]string
	calls     []string
}

func (g *tuiSourceGit) Output(_ context.Context, directory string, arguments ...string) ([]byte, error) {
	key := directory + "|" + strings.Join(arguments, " ")
	g.calls = append(g.calls, key)
	response, ok := g.responses[key]
	if !ok {
		return nil, errors.New("unexpected git call: " + key)
	}
	return []byte(response), nil
}

func (g *tuiSourceGit) called(expected string) bool {
	for _, call := range g.calls {
		if call == expected {
			return true
		}
	}
	return false
}

func TestDeleteLocalSkillClearsProjectionThenRemovesItAfterConfirmation(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "beta"), "beta")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	model = selectFirstSkill(model) // expand demo, select alpha
	// Enable alpha for the current client so a projection exists to clean up.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	model = updated.(Model)
	skill, ok := loaded.Skill("local-shared/demo/alpha")
	if !ok {
		t.Fatal("alpha skill missing from catalog")
	}
	link, err := projection.New(projectRoot, loaded).TargetPath(skill, model.currentClient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("alpha projection was not enabled: %v", err)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.pendingDelete == nil || model.pendingDelete.kind != deleteLocalSkill {
		t.Fatalf("expected pending local skill deletion, got %#v", model.pendingDelete)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if command == nil {
		t.Fatal("expected a delete command on confirmation")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("delete failed: %v", model.err)
	}

	assertNotExist(t, link)
	assertNotExist(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"))
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "demo", "beta", "SKILL.md")); err != nil {
		t.Fatalf("sibling skill was removed: %v", err)
	}
	if _, ok := model.catalog.Skill("local-shared/demo/alpha"); ok {
		t.Fatal("deleted skill still present in reloaded catalog")
	}
}

func TestDeleteLocalGroupRemovesTheWholeDirectory(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "keep", "gamma"), "gamma")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}) // cursor 0 = first source (local-shared/demo)
	model = updated.(Model)
	if model.pendingDelete == nil || model.pendingDelete.kind != deleteLocalSource {
		t.Fatalf("expected pending local group deletion, got %#v", model.pendingDelete)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("delete failed: %v", model.err)
	}

	assertNotExist(t, filepath.Join(sourcesRoot, "local", "shared", "demo"))
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "keep", "gamma", "SKILL.md")); err != nil {
		t.Fatalf("unrelated group was removed: %v", err)
	}
}

func TestDeleteVendorSkillRowIsRejectedAsReadOnly(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "worktrunk"), "worktrunk")
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "wt-switch-create"), "wt-switch-create")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), &source.Manager{}, i18n.New(i18n.English))
	model = selectFirstSkill(model) // expand worktrunk, select a skill row
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	model = updated.(Model)
	if model.pendingDelete != nil {
		t.Fatal("individual vendor skills must not be deletable")
	}
	if got := model.status; got != i18n.New(i18n.English).Text(i18n.DeleteReadOnlySkill) {
		t.Fatalf("status = %q, want read-only skill message", got)
	}
}

func TestDeleteVendorSourceRowBuildsAWholeSourcePlan(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk", "skills", "worktrunk"), "worktrunk")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), &source.Manager{}, i18n.New(i18n.English))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}) // cursor 0 = vendor source row
	model = updated.(Model)
	if model.pendingDelete == nil || model.pendingDelete.kind != deleteVendorSource {
		t.Fatalf("expected pending vendor source deletion, got %#v", model.pendingDelete)
	}
	if view := model.View().Content; !strings.Contains(view, i18n.New(i18n.English).Text(i18n.DeleteConfirmTitle)) {
		t.Fatal("confirmation panel is not rendered while a deletion is pending")
	}
	// Cancelling leaves the source in place.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if model.pendingDelete != nil {
		t.Fatal("cancel should clear the pending deletion")
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk")); err != nil {
		t.Fatalf("vendor source removed after cancel: %v", err)
	}
}

func TestMCPDeleteRemovesServerAfterConfirmation(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"version":1,"mcpServers":{"context7":{"command":"npx"},"grafana":{"url":"https://x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpCatalog, err := mcp.LoadCatalog(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English), Resources{
		MCPCatalog: mcpCatalog,
		MCPManager: mcp.NewManager(projectRoot, mcpCatalog, loaded.Clients),
	})
	model.tab = tabMCP

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}) // cursor 0 = context7 (sorted)
	model = updated.(Model)
	if model.pendingDelete == nil || model.pendingDelete.kind != deleteMCPServer || model.pendingDelete.server != "context7" {
		t.Fatalf("expected pending mcp delete of context7, got %#v", model.pendingDelete)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("delete failed: %v", model.err)
	}
	if _, ok := model.mcpCatalog.Server("context7"); ok {
		t.Fatal("deleted MCP server still present in reloaded catalog")
	}
	if _, ok := model.mcpCatalog.Server("grafana"); !ok {
		t.Fatal("unrelated MCP server was removed")
	}
}

func TestMCPToggleAllClientsEnablesAndDisablesEveryClient(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"version":1,"mcpServers":{"context7":{"command":"npx"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpCatalog, err := mcp.LoadCatalog(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English), Resources{
		MCPCatalog: mcpCatalog,
		MCPManager: mcp.NewManager(projectRoot, mcpCatalog, loaded.Clients),
	})
	model.tab = tabMCP

	configFiles := []string{
		filepath.Join(projectRoot, ".codex", "config.toml"),
		filepath.Join(projectRoot, ".mcp.json"),
		filepath.Join(projectRoot, ".gemini", "settings.json"),
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("enable MCP for all clients: %v", model.err)
	}
	for _, path := range configFiles {
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "context7") {
			t.Fatalf("all-client MCP toggle did not enable %s: %s, %v", path, data, err)
		}
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	if model.err != nil {
		t.Fatalf("disable MCP for all clients: %v", model.err)
	}
	for _, path := range configFiles {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), "context7") {
			t.Fatalf("all-client MCP toggle did not disable %s:\n%s", path, data)
		}
	}
}

// submitForm drives an open huh dialog to completion deterministically: it sets
// the fields the user would have entered, marks the form completed, then feeds a
// message so the model runs its completion handler. This exercises the model's
// form plumbing without depending on huh's internal focus/paste timing.
func submitForm(t *testing.T, model Model, prepare func(*activeForm)) Model {
	t.Helper()
	if model.active == nil {
		t.Fatal("no active form to submit")
	}
	prepare(model.active)
	model.active.form.State = huh.StateCompleted
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model)
}

func newMCPModel(t *testing.T, servers string) (Model, string) {
	t.Helper()
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(servers), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpCatalog, err := mcp.LoadCatalog(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English), Resources{
		MCPCatalog: mcpCatalog,
		MCPManager: mcp.NewManager(projectRoot, mcpCatalog, loaded.Clients),
	})
	model.tab = tabMCP
	return model, projectRoot
}

func TestMCPAddViaJSONFormWritesWrapperAndBareObjects(t *testing.T) {
	model, _ := newMCPModel(t, `{"version":1,"mcpServers":{"context7":{"command":"npx"}}}`)

	// The n key opens the JSON paste form on the MCP tab.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if model.active == nil || model.active.kind != formAddMCP {
		t.Fatalf("n did not open the MCP JSON form: %#v", model.active)
	}

	// A wrapper block adds every keyed server at once.
	model = submitForm(t, model, func(form *activeForm) {
		form.json = `{"mcpServers":{"grafana":{"url":"https://mcp.example.com"},"deno":{"command":"deno"}}}`
	})
	if model.err != nil {
		t.Fatalf("wrapper add failed: %v", model.err)
	}
	if model.active != nil {
		t.Fatal("form should close after a successful add")
	}
	if server, ok := model.mcpCatalog.Server("grafana"); !ok || server.Transport != mcp.TransportHTTP {
		t.Fatalf("grafana missing or incorrect: %#v", server)
	}
	if _, ok := model.mcpCatalog.Server("deno"); !ok {
		t.Fatal("deno from wrapper not added")
	}

	// A bare object prompts for a name, then adds it.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	model = submitForm(t, model, func(form *activeForm) { form.json = `{"command":"uvx"}` })
	if model.active == nil || model.active.kind != formAddMCPName {
		t.Fatalf("bare object did not prompt for a name: %#v", model.active)
	}
	model = submitForm(t, model, func(form *activeForm) { form.name = "uvxmcp" })
	if model.err != nil {
		t.Fatalf("named bare add failed: %v", model.err)
	}
	if _, ok := model.mcpCatalog.Server("uvxmcp"); !ok {
		t.Fatal("named bare server not added")
	}
}

func TestModalDialogRendersCenteredWithoutResizingCanvas(t *testing.T) {
	model, _ := newMCPModel(t, `{"version":1,"mcpServers":{"context7":{"command":"npx"}}}`)
	resized, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = resized.(Model)

	// Baseline canvas size with no dialog open.
	base := model.View().Content
	baseWidth, baseHeight := lipgloss.Width(base), lipgloss.Height(base)

	opened, _ := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = opened.(Model)
	if model.active == nil {
		t.Fatal("n did not open the MCP dialog")
	}
	view := model.View().Content
	if got := lipgloss.Width(view); got != baseWidth {
		t.Fatalf("dialog changed canvas width: %d != %d", got, baseWidth)
	}
	if got := lipgloss.Height(view); got != baseHeight {
		t.Fatalf("dialog changed canvas height: %d != %d", got, baseHeight)
	}
	// The dialog is a rounded card carrying the form title.
	for _, fragment := range []string{"╭", "╰", "Paste MCP JSON"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("modal view missing %q:\n%s", fragment, view)
		}
	}
}

func TestSkillsTabAddMenuCreatesLocalSkill(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), nil, i18n.New(i18n.English))

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if model.active == nil || model.active.kind != formAddMenu {
		t.Fatalf("n did not open the add menu on the Skills tab: %#v", model.active)
	}
	model = submitForm(t, model, func(form *activeForm) { form.choice = "create" })
	if model.active == nil || model.active.kind != formCreateSkill {
		t.Fatalf("menu selection did not open the create form: %#v", model.active)
	}
	model = submitForm(t, model, func(form *activeForm) {
		form.name = "make-goal"
		form.desc = "Draft a goal."
	})
	if model.err != nil {
		t.Fatalf("create skill failed: %v", model.err)
	}
	if model.active != nil {
		t.Fatal("form should close after creating a skill")
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "local", "shared", "make-goal", "SKILL.md")); err != nil {
		t.Fatalf("skill was not scaffolded: %v", err)
	}
	if _, ok := model.catalog.Skill("local-shared/make-goal/make-goal"); !ok {
		t.Fatal("created skill is not in the reloaded catalog")
	}
}

func TestSkillsTabAddMenuStartsRepoAdd(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkill(t, filepath.Join(sourcesRoot, "local", "shared", "demo", "alpha"), "alpha")
	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(loaded, projectRoot, projection.New(projectRoot, loaded), &source.Manager{}, i18n.New(i18n.English))

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	model = submitForm(t, model, func(form *activeForm) { form.choice = "repo" })
	if model.active == nil || model.active.kind != formAddRepo {
		t.Fatalf("menu selection did not open the repo form: %#v", model.active)
	}
	model = submitForm(t, model, func(form *activeForm) { form.url = "https://github.com/owner/repo" })
	if model.active != nil {
		t.Fatal("repo form should close when submitted")
	}
	if !model.updating {
		t.Fatal("submitting a repo URL did not start an asynchronous add")
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
