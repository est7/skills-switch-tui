package projection

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func setupCopyTestCatalog(t *testing.T, skills ...string) (skillsRoot, projectRoot, userHome string, loaded catalog.Catalog, manager Manager) {
	t.Helper()
	skillsRoot = t.TempDir()
	projectRoot = t.TempDir()
	userHome = t.TempDir()
	for _, name := range skills {
		dir := filepath.Join(skillsRoot, "local", "shared", name)
		if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test "+name+"\n---\n# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("guide for "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	loaded, err = catalog.Load(skillsRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	manager = NewWithUserHome(projectRoot, userHome, loaded)
	return skillsRoot, projectRoot, userHome, loaded, manager
}

func TestAgyProjectSkillUsesManagedCopyAndSharesAgentsDirWithCodex(t *testing.T) {
	_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "wiki")
	skill := loaded.Sources[0].Skills[0]
	target := filepath.Join(projectRoot, ".agents", "skills", "wiki")

	// Enabling Codex alone writes a symlink: Codex reads it, Agy does not.
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(target); err != nil {
		t.Fatalf("expected Codex symlink at %s: %v", target, err)
	}
	if state, err := manager.State(skill, catalog.ClientCodex); err != nil || state != StateEnabled {
		t.Fatalf("codex state = %q, %v; want enabled", state, err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateDisabled {
		t.Fatalf("agy state with codex symlink = %q, %v; want disabled", state, err)
	}

	// Disabling Agy while only Codex's symlink exists must be a no-op and keep Codex's symlink.
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, false); err != nil {
		t.Fatalf("disable agy with codex symlink: %v", err)
	}
	if _, err := os.Readlink(target); err != nil {
		t.Fatalf("disabling agy removed codex symlink: %v", err)
	}

	// Enabling Agy atomically upgrades the symlink to a managed copy readable by both.
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("expected managed real directory at %s, got info=%v err=%v", target, info, err)
	}
	if !hasManagedCopyMarker(target) {
		t.Fatalf("managed copy missing marker at %s", target)
	}
	if _, err := os.Stat(filepath.Join(target, "references", "guide.md")); err != nil {
		t.Fatalf("managed copy missing nested file: %v", err)
	}
	for _, clientID := range []catalog.Client{catalog.ClientCodex, catalog.ClientAgy} {
		if state, err := manager.State(skill, clientID); err != nil || state != StateEnabled {
			t.Fatalf("%s state after copy = %q, %v; want enabled", clientID, state, err)
		}
	}

	// Re-enabling Codex against an existing fresh copy must keep the copy (no downgrade).
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("enabling codex downgraded active agy copy: %v, %v", info, err)
	}

	// Disabling Agy removes the shared managed copy cleanly.
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("expected copy removed at %s, got err=%v", target, err)
	}
	for _, clientID := range []catalog.Client{catalog.ClientCodex, catalog.ClientAgy} {
		if state, err := manager.State(skill, clientID); err != nil || state != StateDisabled {
			t.Fatalf("%s state after disable = %q, %v; want disabled", clientID, state, err)
		}
	}
}

func TestManagedCopyDetectsLocalEditsAndUpstreamStaleness(t *testing.T) {
	_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "wiki")
	skill := loaded.Sources[0].Skills[0]
	target := filepath.Join(projectRoot, ".agents", "skills", "wiki")

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}

	// Editing the SSOT source makes the clean copy stale; re-enabling refreshes it.
	updatedBody := "---\nname: wiki\ndescription: Updated\n---\n# updated\n"
	if err := os.WriteFile(filepath.Join(skill.Path, "SKILL.md"), []byte(updatedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateStale {
		t.Fatalf("state after source edit = %q, %v; want stale", state, err)
	}
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatalf("refresh stale copy: %v", err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateEnabled {
		t.Fatalf("state after refresh = %q, %v; want enabled", state, err)
	}
	copied, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil || string(copied) != updatedBody {
		t.Fatalf("refreshed SKILL.md = %q, %v; want %q", copied, err, updatedBody)
	}

	// Editing the copy locally turns it into a conflict and refuses overwrite or deletion.
	localEdit := "local user notes\n"
	if err := os.WriteFile(filepath.Join(target, "local.md"), []byte(localEdit), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateConflict {
		t.Fatalf("state after local edit = %q, %v; want conflict", state, err)
	}
	for _, enabled := range []bool{true, false} {
		err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, enabled)
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("SetEnabled(%v) on modified copy err = %v, want ConflictError", enabled, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(target, "local.md")); err != nil || string(got) != localEdit {
		t.Fatalf("local edit was lost: %q, %v", got, err)
	}
}

func TestApplyMergesSharedAgentsSlotAndRollsBackAtomicallyOnLaterConflict(t *testing.T) {
	_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "wiki")
	skill := loaded.Sources[0].Skills[0]
	agentsTarget := filepath.Join(projectRoot, ".agents", "skills", "wiki")
	claudeTarget := filepath.Join(projectRoot, ".claude", "skills", "wiki")

	// Start with an existing Codex symlink at .agents/skills/wiki and an unmanaged
	// file blocking Claude at .claude/skills/wiki.
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(claudeTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeTarget, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Atomic multi-client apply fails preflight: Codex symlink stays untouched.
	err := manager.Apply([]Operation{
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientCodex, Enabled: true, Scope: ScopeProject},
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientAgy, Enabled: true, Scope: ScopeProject},
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientClaude, Enabled: true, Scope: ScopeProject},
	})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Apply() error = %v, want ConflictError", err)
	}
	if target, linkErr := os.Readlink(agentsTarget); linkErr != nil || filepath.Clean(target) != filepath.Clean(skill.Path) {
		t.Fatalf("existing codex symlink was mutated on conflict: target=%q err=%v", target, linkErr)
	}

	// Removing Claude's blocker lets codex+agy+claude apply together as one copy + one symlink.
	if err := os.Remove(claudeTarget); err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply([]Operation{
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientCodex, Enabled: true, Scope: ScopeProject},
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientAgy, Enabled: true, Scope: ScopeProject},
		{Skills: []catalog.Skill{skill}, Client: catalog.ClientClaude, Enabled: true, Scope: ScopeProject},
	}); err != nil {
		t.Fatalf("Apply(all 3 clients): %v", err)
	}
	if info, statErr := os.Lstat(agentsTarget); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("expected managed copy at %s, got %v, %v", agentsTarget, info, statErr)
	}
	if _, linkErr := os.Readlink(claudeTarget); linkErr != nil {
		t.Fatalf("expected symlink at %s: %v", claudeTarget, linkErr)
	}
}

func TestAgyGlobalScopeUsesSymlinkAndStaysIsolatedFromCodex(t *testing.T) {
	_, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "wiki")
	skill := loaded.Sources[0].Skills[0]

	// Codex has a project symlink; promoting Agy globally creates a symlink under
	// ~/.gemini/config/skills without disturbing Codex's project symlink.
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEnabledAt([]catalog.Skill{skill}, catalog.ClientAgy, true, ScopeGlobal); err != nil {
		t.Fatalf("enable agy global: %v", err)
	}
	agyGlobal := filepath.Join(userHome, ".gemini", "config", "skills", "wiki")
	if target, err := os.Readlink(agyGlobal); err != nil || filepath.Clean(target) != filepath.Clean(skill.Path) {
		t.Fatalf("agy global symlink = %q, %v; want %q", target, err, skill.Path)
	}
	// Codex project symlink stays intact and Agy reports StateGlobal (not duplicate).
	if _, err := os.Readlink(filepath.Join(projectRoot, ".agents", "skills", "wiki")); err != nil {
		t.Fatalf("promoting agy global removed codex project symlink: %v", err)
	}
	if state, err := manager.StateAt(skill, catalog.ClientAgy, ScopeProject); err != nil || state != StateGlobal {
		t.Fatalf("agy project state = %q, %v; want global", state, err)
	}
	if state, err := manager.StateAt(skill, catalog.ClientCodex, ScopeProject); err != nil || state != StateEnabled {
		t.Fatalf("codex project state = %q, %v; want enabled", state, err)
	}
}

func TestManagedCopyDiscoveryOrphansRetireAndRetarget(t *testing.T) {
	skillsRoot, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "wiki")
	source := loaded.Sources[0]
	skill := source.Skills[0]
	copyPath := filepath.Join(projectRoot, ".agents", "skills", "wiki")

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}

	// DiscoverUnmanagedSkills must skip managed copies and still report unmanaged agy global skills.
	unmanagedAgyGlobal := filepath.Join(userHome, ".gemini", "config", "skills", "handwritten")
	if err := os.MkdirAll(unmanagedAgyGlobal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmanagedAgyGlobal, "SKILL.md"), []byte("---\nname: handwritten\ndescription: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	discovered, err := manager.DiscoverUnmanagedSkills(ScopeProject, ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 || discovered[0].Client != catalog.ClientAgy || discovered[0].Name != "handwritten" {
		t.Fatalf("DiscoverUnmanagedSkills() = %#v, want only agy global handwritten", discovered)
	}

	// RetireSource + Restore round-trips a managed copy cleanly.
	retirement, err := manager.RetireSource(source, ScopeProject)
	if err != nil {
		t.Fatalf("RetireSource: %v", err)
	}
	if _, err := os.Lstat(copyPath); !os.IsNotExist(err) {
		t.Fatalf("copy survived RetireSource: %v", err)
	}
	if err := retirement.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateEnabled {
		t.Fatalf("state after Restore = %q, %v; want enabled", state, err)
	}

	// RetargetSource rewrites marker sourcePath cleanly and restores on rollback.
	movedRoot := filepath.Join(skillsRoot, "local", "shared", "wiki-moved")
	if err := os.Rename(source.Path, movedRoot); err != nil {
		t.Fatal(err)
	}
	retargeted, err := manager.RetargetSource(source, movedRoot, ScopeProject)
	if err != nil {
		t.Fatalf("RetargetSource: %v", err)
	}
	entry, err := inspectEntry(copyPath)
	if err != nil || entry.kind != entryManagedCopy || entry.resolvedTarget != canonicalPath(movedRoot) {
		t.Fatalf("entry after RetargetSource = %+v, %v", entry, err)
	}
	if err := retargeted.Restore(); err != nil {
		t.Fatalf("retarget Restore: %v", err)
	}
	if err := os.Rename(movedRoot, source.Path); err != nil {
		t.Fatal(err)
	}

	// Removing upstream skill makes the managed copy an orphan reported once (not duplicated for codex+agy).
	if err := os.RemoveAll(skill.Path); err != nil {
		t.Fatal(err)
	}
	orphans, err := manager.OrphanedProjectionsAfterRefreshAt([]catalog.Source{{ID: source.ID, Path: filepath.Dir(source.Path)}}, ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].Path != copyPath {
		t.Fatalf("orphans = %#v, want single orphan at %s", orphans, copyPath)
	}
	pruned, err := manager.PruneOrphans(orphans)
	if err != nil || len(pruned) != 1 {
		t.Fatalf("PruneOrphans = %#v, %v", pruned, err)
	}
	if _, err := os.Lstat(copyPath); !os.IsNotExist(err) {
		t.Fatalf("orphaned managed copy was not pruned: %v", err)
	}
}

func TestSharedAgentsDirPeerPreflightAndDisableGuards(t *testing.T) {
	t.Run("codex global blocks agy project enable", func(t *testing.T) {
		_, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "wiki")
		skill := loaded.Sources[0].Skills[0]
		if err := manager.SetEnabledAt([]catalog.Skill{skill}, catalog.ClientCodex, true, ScopeGlobal); err != nil {
			t.Fatal(err)
		}
		err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true)
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("enable agy project while codex global = %v, want ConflictError", err)
		}
		codexGlobal := filepath.Join(userHome, ".agents", "skills", "wiki")
		for _, want := range []string{"codex", codexGlobal, "--scope global --client agy"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("conflict %q missing %q", err.Error(), want)
			}
		}
		assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", "wiki"))
		if state, stateErr := manager.State(skill, catalog.ClientCodex); stateErr != nil || state != StateGlobal {
			t.Fatalf("codex state = %q, %v; want global", state, stateErr)
		}
	})

	t.Run("agy project blocks solo codex disable and solo codex global promote", func(t *testing.T) {
		_, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "wiki")
		skill := loaded.Sources[0].Skills[0]
		copyPath := filepath.Join(projectRoot, ".agents", "skills", "wiki")
		if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
			t.Fatal(err)
		}

		// Solo codex disable must fail preflight naming agy.
		err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientCodex, false)
		var conflict *ConflictError
		if !errors.As(err, &conflict) || !strings.Contains(err.Error(), "agy") {
			t.Fatalf("solo disable codex on shared copy = %v, want ConflictError naming agy", err)
		}
		if entry, inspectErr := inspectEntry(copyPath); inspectErr != nil || entry.kind != entryManagedCopy {
			t.Fatalf("shared copy was altered after rejected codex disable: %+v, %v", entry, inspectErr)
		}

		// Solo codex global promote must fail preflight naming agy and leave both scopes untouched.
		err = manager.SetEnabledAt([]catalog.Skill{skill}, catalog.ClientCodex, true, ScopeGlobal)
		if !errors.As(err, &conflict) || !strings.Contains(err.Error(), "agy") {
			t.Fatalf("solo promote codex on shared copy = %v, want ConflictError naming agy", err)
		}
		assertMissing(t, filepath.Join(userHome, ".agents", "skills", "wiki"))
		if entry, inspectErr := inspectEntry(copyPath); inspectErr != nil || entry.kind != entryManagedCopy {
			t.Fatalf("shared copy was altered after rejected codex promote: %+v, %v", entry, inspectErr)
		}

		// Promoting both codex and agy together removes the shared copy atomically.
		if err := manager.Apply([]Operation{
			{Skills: []catalog.Skill{skill}, Client: catalog.ClientCodex, Enabled: true, Scope: ScopeGlobal},
			{Skills: []catalog.Skill{skill}, Client: catalog.ClientAgy, Enabled: true, Scope: ScopeGlobal},
		}); err != nil {
			t.Fatalf("promote codex+agy together: %v", err)
		}
		assertMissing(t, copyPath)
		assertLinkTarget(t, filepath.Join(userHome, ".agents", "skills", "wiki"), skill.Path)
		assertLinkTarget(t, filepath.Join(userHome, ".gemini", "config", "skills", "wiki"), skill.Path)
	})

	t.Run("disable codex and agy together removes shared copy", func(t *testing.T) {
		_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "wiki")
		skill := loaded.Sources[0].Skills[0]
		copyPath := filepath.Join(projectRoot, ".agents", "skills", "wiki")
		if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
			t.Fatal(err)
		}
		if err := manager.Apply([]Operation{
			{Skills: []catalog.Skill{skill}, Client: catalog.ClientCodex, Enabled: false, Scope: ScopeProject},
			{Skills: []catalog.Skill{skill}, Client: catalog.ClientAgy, Enabled: false, Scope: ScopeProject},
		}); err != nil {
			t.Fatalf("disable codex+agy together: %v", err)
		}
		assertMissing(t, copyPath)
	})

	t.Run("agy-only skill rejected in shared project dir and allowed in global", func(t *testing.T) {
		skillsRoot := t.TempDir()
		projectRoot := t.TempDir()
		userHome := t.TempDir()
		dir := filepath.Join(skillsRoot, "local", "agy", "only-agy")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: only-agy\ndescription: agy only\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
		if err != nil {
			t.Fatal(err)
		}
		skill := loaded.Sources[0].Skills[0]
		manager := NewWithUserHome(projectRoot, userHome, loaded)

		err = manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true)
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("enable agy-only in shared project dir = %v, want ConflictError", err)
		}
		for _, want := range []string{"codex", "--scope global --client agy"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("conflict %q missing %q", err.Error(), want)
			}
		}
		assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", "only-agy"))

		if err := manager.SetEnabledAt([]catalog.Skill{skill}, catalog.ClientAgy, true, ScopeGlobal); err != nil {
			t.Fatalf("enable agy-only in global scope: %v", err)
		}
		assertLinkTarget(t, filepath.Join(userHome, ".gemini", "config", "skills", "only-agy"), skill.Path)
	})

	t.Run("downgradeToSymlink replaces shared copy when enabling codex while disabling agy", func(t *testing.T) {
		_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "wiki")
		skill := loaded.Sources[0].Skills[0]
		copyPath := filepath.Join(projectRoot, ".agents", "skills", "wiki")
		if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
			t.Fatal(err)
		}
		if err := manager.Apply([]Operation{
			{Skills: []catalog.Skill{skill}, Client: catalog.ClientCodex, Enabled: true, Scope: ScopeProject},
			{Skills: []catalog.Skill{skill}, Client: catalog.ClientAgy, Enabled: false, Scope: ScopeProject},
		}); err != nil {
			t.Fatalf("downgradeToSymlink Apply: %v", err)
		}
		assertLinkTarget(t, copyPath, skill.Path)
	})
}

func TestTransactionMidExecutionRollbackRestoresStashAndRetargetTag(t *testing.T) {
	t.Run("mid-execution failure restores stashed copy cleanly", func(t *testing.T) {
		skillsRoot, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "alpha", "beta")
		alpha := loaded.Sources[0].Skills[0]
		if alpha.Name != "alpha" {
			alpha = loaded.Sources[1].Skills[0]
		}
		beta := loaded.Sources[1].Skills[0]
		if beta.Name != "beta" {
			beta = loaded.Sources[0].Skills[0]
		}
		_ = skillsRoot
		if err := manager.SetEnabled([]catalog.Skill{alpha}, catalog.ClientAgy, true); err != nil {
			t.Fatal(err)
		}
		alphaCopy := filepath.Join(projectRoot, ".agents", "skills", "alpha")
		betaCopy := filepath.Join(projectRoot, ".agents", "skills", "beta")
		beforeEntry, err := inspectEntry(alphaCopy)
		if err != nil || beforeEntry.kind != entryManagedCopy {
			t.Fatalf("beforeEntry = %+v, %v", beforeEntry, err)
		}

		// Change 1 removes alpha (stashing it); Change 2 fails mid-execution via hook.
		changes := []slotChange{
			{
				path:     alphaCopy,
				expected: slotSpec{kind: slotCopy, target: beforeEntry.resolvedTarget, hash: beforeEntry.contentHash},
				desired:  slotSpec{kind: slotAbsent},
			},
			{
				path:     betaCopy,
				expected: slotSpec{kind: slotAbsent},
				desired:  slotSpec{kind: slotCopy, target: beta.Path},
			},
		}
		_, execErr := executeSlotChangesWithHook(changes, false, func(next slotChange) error {
			if next.path == betaCopy {
				return errors.New("injected second-slot failure")
			}
			return nil
		})
		if execErr == nil || !strings.Contains(execErr.Error(), "injected second-slot failure") {
			t.Fatalf("executeSlotChangesWithHook err = %v", execErr)
		}
		afterEntry, err := inspectEntry(alphaCopy)
		if err != nil || afterEntry.kind != entryManagedCopy || afterEntry.contentHash != beforeEntry.contentHash {
			t.Fatalf("alpha copy not restored from stash: %+v, %v", afterEntry, err)
		}
		assertNoStagingEntries(t, filepath.Dir(alphaCopy))
	})

	t.Run("mid-execution failure rolls back retargetTag marker", func(t *testing.T) {
		skillsRoot, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "alpha")
		alpha := loaded.Sources[0].Skills[0]
		if err := manager.SetEnabled([]catalog.Skill{alpha}, catalog.ClientAgy, true); err != nil {
			t.Fatal(err)
		}
		alphaCopy := filepath.Join(projectRoot, ".agents", "skills", "alpha")
		beforeEntry, err := inspectEntry(alphaCopy)
		if err != nil || beforeEntry.kind != entryManagedCopy {
			t.Fatalf("beforeEntry = %+v, %v", beforeEntry, err)
		}
		moved := filepath.Join(skillsRoot, "local", "shared", "alpha-moved")
		if err := os.MkdirAll(moved, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moved, "SKILL.md"), []byte("---\nname: alpha\ndescription: moved\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		secondPath := filepath.Join(projectRoot, ".claude", "skills", "alpha")
		changes := []slotChange{
			{
				path:        alphaCopy,
				expected:    slotSpec{kind: slotCopy, target: beforeEntry.resolvedTarget, hash: beforeEntry.contentHash},
				desired:     slotSpec{kind: slotCopy, target: moved},
				retargetTag: true,
			},
			{
				path:     secondPath,
				expected: slotSpec{kind: slotAbsent},
				desired:  slotSpec{kind: slotSymlink, target: moved},
			},
		}
		_, execErr := executeSlotChangesWithHook(changes, false, func(next slotChange) error {
			if next.path == secondPath {
				return errors.New("injected retarget failure")
			}
			return nil
		})
		if execErr == nil {
			t.Fatal("expected mid-execution failure")
		}
		afterEntry, err := inspectEntry(alphaCopy)
		if err != nil || afterEntry.kind != entryManagedCopy || afterEntry.resolvedTarget != canonicalPath(alpha.Path) {
			t.Fatalf("retargetTag rollback failed: %+v, %v; want %s", afterEntry, err, canonicalPath(alpha.Path))
		}
	})

	t.Run("failed rollback preserves stash instead of deleting it", func(t *testing.T) {
		_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "alpha")
		alpha := loaded.Sources[0].Skills[0]
		if err := manager.SetEnabled([]catalog.Skill{alpha}, catalog.ClientAgy, true); err != nil {
			t.Fatal(err)
		}
		alphaCopy := filepath.Join(projectRoot, ".agents", "skills", "alpha")
		beforeEntry, err := inspectEntry(alphaCopy)
		if err != nil || beforeEntry.kind != entryManagedCopy {
			t.Fatalf("beforeEntry = %+v, %v", beforeEntry, err)
		}
		// Replace alpha with a symlink in change 1 (creating a stash of the copy),
		// then in the hook for change 2 make the symlink path parent unwritable OR
		// simulate a failed restore by moving a blocking directory into place?
		// Actually, if we remove the symlink in rollbackExecutedChange and then
		// rename(stash, path) fails because stash was replaced or parent read-only,
		// test directly via rollbackExecutedChange + fail path: create a non-empty
		// dir at path AFTER remove would require concurrency; instead chmod parent
		// dir read-only isn't removable either. Best deterministic test: run
		// executeSlotChangesWithHook where change 1 replaces copy -> slotAbsent
		// (stash exists), and in the hook for change 2 create a non-empty file at
		// alphaCopy? Wait: rollbackExecutedChange refuses to remove a concurrently
		// created entry, returning error; either way stash must NOT be deleted!
		secondPath := filepath.Join(projectRoot, ".claude", "skills", "alpha")
		changes := []slotChange{
			{
				path:     alphaCopy,
				expected: slotSpec{kind: slotCopy, target: beforeEntry.resolvedTarget, hash: beforeEntry.contentHash},
				desired:  slotSpec{kind: slotAbsent},
			},
			{
				path:     secondPath,
				expected: slotSpec{kind: slotAbsent},
				desired:  slotSpec{kind: slotSymlink, target: alpha.Path},
			},
		}
		_, execErr := executeSlotChangesWithHook(changes, false, func(next slotChange) error {
			if next.path == secondPath {
				// Plant an unexpected file at alphaCopy so rollbackExecutedChange fails.
				if err := os.WriteFile(alphaCopy, []byte("concurrent"), 0o644); err != nil {
					t.Fatal(err)
				}
				return errors.New("injected failure after concurrent write")
			}
			return nil
		})
		if execErr == nil || !strings.Contains(execErr.Error(), "rollback") {
			t.Fatalf("expected rollback error, got %v", execErr)
		}
		entries, err := os.ReadDir(filepath.Dir(alphaCopy))
		if err != nil {
			t.Fatal(err)
		}
		foundStash := false
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".skills-switch-stash-") {
				foundStash = true
				break
			}
		}
		if !foundStash {
			t.Fatalf("stash was deleted despite failed rollback: entries=%v", entries)
		}
	})
}

func TestManagedCopyExecBitTamperAndReadOnlySourceCleanup(t *testing.T) {
	skillsRoot, projectRoot, userHome, _, _ := setupCopyTestCatalog(t, "tool")
	skillDir := filepath.Join(skillsRoot, "local", "shared", "tool")
	scriptDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptDir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	readonlyDir := filepath.Join(skillDir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readonlyFile := filepath.Join(readonlyDir, "data.txt")
	if err := os.WriteFile(readonlyFile, []byte("immutable\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(readonlyDir, 0o755)
		_ = os.Chmod(readonlyFile, 0o644)
	})

	loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	skill := loaded.Sources[0].Skills[0]
	manager := NewWithUserHome(projectRoot, userHome, loaded)

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatalf("enable read-only/executable skill: %v", err)
	}
	copyDir := filepath.Join(projectRoot, ".agents", "skills", "tool")
	copiedScript := filepath.Join(copyDir, "scripts", "run.sh")
	info, err := os.Stat(copiedScript)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("copied script lost executable bit: info=%v err=%v", info, err)
	}

	// Stripping the executable bit must be detected as local tampering (StateConflict).
	if err := os.Chmod(copiedScript, 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateConflict {
		t.Fatalf("state after chmod -x = %q, %v; want conflict", state, err)
	}
	// Restoring the executable bit returns to StateEnabled, and disable removes read-only source copy cleanly.
	if err := os.Chmod(copiedScript, 0o755); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateEnabled {
		t.Fatalf("state after restoring +x = %q, %v; want enabled", state, err)
	}
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, false); err != nil {
		t.Fatalf("disable copy from read-only source: %v", err)
	}
	assertMissing(t, copyDir)
}

func TestManagedCopySymlinkValidationAndMarkerDirSafety(t *testing.T) {
	t.Run("valid internal symlinks and marker directory in source", func(t *testing.T) {
		skillsRoot, projectRoot, userHome, _, _ := setupCopyTestCatalog(t, "linked")
		skillDir := filepath.Join(skillsRoot, "local", "shared", "linked")
		if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("run.sh", filepath.Join(skillDir, "scripts", "latest.sh")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("latest.sh", filepath.Join(skillDir, "scripts", "chained.sh")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("references", filepath.Join(skillDir, "docs")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("docs/guide.md", filepath.Join(skillDir, "via-dir.md")); err != nil {
			t.Fatal(err)
		}
		// A directory named .skills-switch-managed.json in source must be skipped with filepath.SkipDir.
		markerNamedDir := filepath.Join(skillDir, managedCopyMarkerFile)
		if err := os.MkdirAll(markerNamedDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(markerNamedDir, "child.txt"), []byte("ignored\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
		if err != nil {
			t.Fatal(err)
		}
		skill := loaded.Sources[0].Skills[0]
		manager := NewWithUserHome(projectRoot, userHome, loaded)
		if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
			t.Fatalf("enable skill with valid internal symlinks: %v", err)
		}
		copyDir := filepath.Join(projectRoot, ".agents", "skills", "linked")
		if got, err := os.Readlink(filepath.Join(copyDir, "scripts", "latest.sh")); err != nil || got != "run.sh" {
			t.Fatalf("preserved symlink = %q, %v; want run.sh", got, err)
		}
		if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateEnabled {
			t.Fatalf("state = %q, %v; want enabled", state, err)
		}
	})

	t.Run("rejects absolute escaping chained cyclic and dangling symlinks", func(t *testing.T) {
		outsideFile := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outsideFile, []byte("secret\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cases := map[string]func(t *testing.T, skillDir string){
			"absolute symlink": func(t *testing.T, skillDir string) {
				if err := os.Symlink(outsideFile, filepath.Join(skillDir, "abs.txt")); err != nil {
					t.Fatal(err)
				}
			},
			"escaping relative symlink": func(t *testing.T, skillDir string) {
				rel, err := filepath.Rel(filepath.Join(skillDir, "references"), outsideFile)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(rel, filepath.Join(skillDir, "references", "escape.txt")); err != nil {
					t.Fatal(err)
				}
			},
			"bounce-out relative symlink": func(t *testing.T, skillDir string) {
				if err := os.Symlink("../bad/SKILL.md", filepath.Join(skillDir, "bounce.md")); err != nil {
					t.Fatal(err)
				}
			},
			"chained escaping symlink": func(t *testing.T, skillDir string) {
				rel, err := filepath.Rel(skillDir, outsideFile)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(rel, filepath.Join(skillDir, "hop2")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("hop2", filepath.Join(skillDir, "hop1")); err != nil {
					t.Fatal(err)
				}
			},
			"cyclic symlinks": func(t *testing.T, skillDir string) {
				if err := os.Symlink("c2", filepath.Join(skillDir, "c1")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("c1", filepath.Join(skillDir, "c2")); err != nil {
					t.Fatal(err)
				}
			},
			"dangling symlink": func(t *testing.T, skillDir string) {
				if err := os.Symlink("does-not-exist.md", filepath.Join(skillDir, "dangling.md")); err != nil {
					t.Fatal(err)
				}
			},
		}
		for name, plant := range cases {
			t.Run(name, func(t *testing.T) {
				skillsRoot, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "bad")
				_ = skillsRoot
				skill := loaded.Sources[0].Skills[0]
				plant(t, skill.Path)
				if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err == nil {
					t.Fatalf("SetEnabled accepted %s", name)
				}
				assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", "bad"))
				_ = userHome
			})
		}
	})

	t.Run("symlinked source skill root copies contents instead of empty dir", func(t *testing.T) {
		realSkill := filepath.Join(t.TempDir(), "real-skill")
		if err := os.MkdirAll(realSkill, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realSkill, "SKILL.md"), []byte("---\nname: linkedroot\ndescription: real\n---\n# real\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(realSkill, "extra.txt"), []byte("payload\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlinkParent := t.TempDir()
		symlinkSkill := filepath.Join(symlinkParent, "linkedroot")
		if err := os.Symlink(realSkill, symlinkSkill); err != nil {
			t.Fatal(err)
		}
		projectRoot := t.TempDir()
		skill := catalog.Skill{
			ID:       "local-shared/linkedroot/linkedroot",
			Name:     "linkedroot",
			SourceID: "local-shared/linkedroot",
			Path:     symlinkSkill,
			Targets:  map[catalog.Client]bool{catalog.ClientCodex: true, catalog.ClientAgy: true},
		}
		manager := NewWithUserHome(projectRoot, t.TempDir(), catalog.Catalog{
			Root:    symlinkParent,
			Clients: client.DefaultRegistry(),
			Sources: []catalog.Source{{ID: skill.SourceID, Path: symlinkParent, Skills: []catalog.Skill{skill}}},
		})
		if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
			t.Fatalf("enable symlinked skill root: %v", err)
		}
		copyDir := filepath.Join(projectRoot, ".agents", "skills", "linkedroot")
		data, err := os.ReadFile(filepath.Join(copyDir, "extra.txt"))
		if err != nil || string(data) != "payload\n" {
			t.Fatalf("copied extra.txt = %q, %v; want payload", data, err)
		}
		if state, err := manager.State(skill, catalog.ClientAgy); err != nil || state != StateEnabled {
			t.Fatalf("state for symlinked root = %q, %v; want enabled", state, err)
		}
	})
}

func TestCanonicalPathNormalizesSymlinkedResourcesAlias(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real-skills")
	aliasRoot := filepath.Join(base, "alias-skills")
	skillDir := filepath.Join(realRoot, "local", "shared", "wiki")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: wiki\ndescription: v1\n---\n# v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	projectRoot := t.TempDir()
	userHome := t.TempDir()

	// Enable via alias path (like macOS /tmp/...).
	aliasCatalog, err := catalog.Load(aliasRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	aliasManager := NewWithUserHome(projectRoot, userHome, aliasCatalog)
	if err := aliasManager.SetEnabled([]catalog.Skill{aliasCatalog.Sources[0].Skills[0]}, catalog.ClientAgy, true); err != nil {
		t.Fatalf("enable via alias: %v", err)
	}

	// Inspect, refresh, retire, and orphan-check via real path (like macOS /private/tmp/...).
	realCatalog, err := catalog.Load(realRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	realManager := NewWithUserHome(projectRoot, userHome, realCatalog)
	realSkill := realCatalog.Sources[0].Skills[0]
	if state, err := realManager.State(realSkill, catalog.ClientAgy); err != nil || state != StateEnabled {
		t.Fatalf("state via real path = %q, %v; want enabled (no alias conflict)", state, err)
	}

	// Edit source and verify RefreshCopies matches canonicalized target.
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: wiki\ndescription: v2\n---\n# v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshed, err := realManager.RefreshCopies(realCatalog.Sources, ScopeProject)
	if err != nil || len(refreshed) != 1 || refreshed[0].Client != catalog.ClientAgy {
		t.Fatalf("RefreshCopies via real path = %#v, %v; want 1 refreshed copy attributed to agy", refreshed, err)
	}
	if state, err := realManager.State(realSkill, catalog.ClientAgy); err != nil || state != StateEnabled {
		t.Fatalf("state after refresh = %q, %v; want enabled", state, err)
	}
}

func TestRetireSourcePreservesStashAcrossDirtyOrMissingSourceAndCommitCleansUp(t *testing.T) {
	_, projectRoot, _, loaded, manager := setupCopyTestCatalog(t, "wiki")
	source := loaded.Sources[0]
	skill := source.Skills[0]
	copyPath := filepath.Join(projectRoot, ".agents", "skills", "wiki")

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}
	origBytes, err := os.ReadFile(filepath.Join(copyPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	// RetireSource removes copy and keeps stash alive.
	retirement, err := manager.RetireSource(source, ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	assertMissing(t, copyPath)

	// Break/dirty the SSOT (delete SKILL.md and write a dirty file) before calling Restore.
	if err := os.Remove(filepath.Join(skill.Path, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill.Path, "dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := retirement.Restore(); err != nil {
		t.Fatalf("Restore with broken/dirty SSOT: %v", err)
	}
	// Commit after Restore must be an idempotent no-op and leave restored copy intact.
	if err := retirement.Commit(); err != nil {
		t.Fatalf("Commit after Restore: %v", err)
	}
	restoredBytes, err := os.ReadFile(filepath.Join(copyPath, "SKILL.md"))
	if err != nil || string(restoredBytes) != string(origBytes) {
		t.Fatalf("restored SKILL.md = %q, %v; want original %q", restoredBytes, err, origBytes)
	}
	assertMissing(t, filepath.Join(copyPath, "dirty.txt"))
	assertNoStagingEntries(t, filepath.Dir(copyPath))

	// A second RetireSource + Commit discards the stash cleanly.
	if err := os.WriteFile(filepath.Join(skill.Path, "SKILL.md"), origBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	retirement2, err := manager.RetireSource(source, ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if err := retirement2.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	assertMissing(t, copyPath)
	assertNoStagingEntries(t, filepath.Dir(copyPath))
}

func TestCorruptUnreadableAndModifiedCopiesDoNotCrashMaintenance(t *testing.T) {
	skillsRoot, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "good", "damaged")
	var good, damaged catalog.Skill
	for _, src := range loaded.Sources {
		for _, s := range src.Skills {
			if s.Name == "good" {
				good = s
			} else {
				damaged = s
			}
		}
	}
	if err := manager.SetEnabled([]catalog.Skill{good, damaged}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}

	// Plant a FIFO inside damaged copy so hashSkillDir returns an unsupported-file-type error.
	damagedCopy := filepath.Join(projectRoot, ".agents", "skills", "damaged")
	fifoPath := filepath.Join(damagedCopy, "pipe.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	if state, err := manager.State(damaged, catalog.ClientAgy); err != nil || state != StateConflict {
		t.Fatalf("FIFO-tampered copy state = %q, %v; want conflict without error", state, err)
	}

	// Edit good's SSOT and verify RefreshCopies refreshes good without aborting on damaged.
	if err := os.WriteFile(filepath.Join(good.Path, "SKILL.md"), []byte("---\nname: good\ndescription: v2\n---\n# v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshed, err := manager.RefreshCopies(loaded.Sources, ScopeProject)
	if err != nil || len(refreshed) != 1 || refreshed[0].Name != "good" {
		t.Fatalf("RefreshCopies with sibling FIFO copy = %#v, %v; want only good refreshed", refreshed, err)
	}

	// Remove FIFO, plant a chmod 000 file: still classified as StateConflict without global error.
	if err := os.Remove(fifoPath); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(damagedCopy, "secret.txt")
	if err := os.WriteFile(unreadable, []byte("locked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })
	if state, err := manager.State(damaged, catalog.ClientAgy); err != nil || state != StateConflict {
		t.Fatalf("chmod 000 copy state = %q, %v; want conflict", state, err)
	}

	// TOCTOU: orphan detected clean, then locally modified before PruneOrphans -> preserved with error.
	if err := os.RemoveAll(good.Path); err != nil {
		t.Fatal(err)
	}
	orphans, err := manager.OrphanedProjectionsAfterRefreshAt([]catalog.Source{{ID: loaded.Sources[0].ID, Path: filepath.Dir(good.Path)}}, ScopeProject)
	if err != nil || len(orphans) != 1 || orphans[0].Name != "good" {
		t.Fatalf("orphans = %#v, %v; want clean good orphan", orphans, err)
	}
	goodCopy := filepath.Join(projectRoot, ".agents", "skills", "good")
	if err := os.WriteFile(filepath.Join(goodCopy, "user-note.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	pruned, pruneErr := manager.PruneOrphans(orphans)
	if pruneErr == nil || len(pruned) != 0 {
		t.Fatalf("PruneOrphans on TOCTOU-modified copy = (%#v, %v); want refusal", pruned, pruneErr)
	}
	if _, err := os.Stat(filepath.Join(goodCopy, "user-note.txt")); err != nil {
		t.Fatalf("TOCTOU-modified orphan lost user note: %v", err)
	}
	// OrphanedModifiedCopiesAt surfaces goodCopy once its source has no live skills.
	modOrphans, err := manager.OrphanedModifiedCopiesAt(nil, ScopeProject)
	if err != nil || len(modOrphans) < 1 {
		t.Fatalf("OrphanedModifiedCopiesAt = %#v, %v; want modified orphan reported", modOrphans, err)
	}

	// Corrupt marker JSON is classified as StateConflict without aborting RetireSource / RetargetSource.
	if err := os.Chmod(unreadable, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(damagedCopy, managedCopyMarkerFile), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(damaged, catalog.ClientAgy); err != nil || state != StateConflict {
		t.Fatalf("corrupt marker copy state = %q, %v; want conflict", state, err)
	}
	var damagedSource catalog.Source
	for _, src := range loaded.Sources {
		if len(src.Skills) > 0 && src.Skills[0].Name == "damaged" {
			damagedSource = src
			break
		}
	}
	if ret, err := manager.RetireSource(damagedSource, ScopeProject); err != nil {
		t.Fatalf("RetireSource with corrupt copy should skip modified/corrupt copy, got: %v", err)
	} else {
		_ = ret.Commit()
	}
	if _, err := os.Stat(damagedCopy); err != nil {
		t.Fatalf("RetireSource removed corrupt copy: %v", err)
	}
	movedDamaged := filepath.Join(skillsRoot, "local", "shared", "damaged-moved")
	if _, err := manager.RetargetSource(damagedSource, movedDamaged, ScopeProject); err != nil {
		t.Fatalf("RetargetSource with corrupt copy should skip it, got: %v", err)
	}

	// Stale managed copy (pointing into skillsRoot outside registered sources, target absent):
	// reports StateDisabled, detected by OrphanedProjectionsAt, and yields to enabling a new provider.
	staleFixture := newStaleFixture(t)
	staleFixture.skill.Targets[catalog.ClientAgy] = true
	staleCopyDir := filepath.Join(staleFixture.projectRoot, ".agents", "skills", staleFixture.skill.Name)
	if err := os.MkdirAll(staleCopyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleCopyDir, "SKILL.md"), []byte("---\nname: tool\ndescription: legacy\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleHash, err := hashSkillDir(staleCopyDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeManagedCopyMarker(staleCopyDir, managedCopyMarker{
		Version:     managedCopyMarkerVersion,
		SourcePath:  canonicalPath(staleFixture.legacyPath),
		ContentHash: staleHash,
	}); err != nil {
		t.Fatal(err)
	}
	if state, err := staleFixture.manager.State(staleFixture.skill, catalog.ClientAgy); err != nil || state != StateDisabled {
		t.Fatalf("state behind stale managed copy = %q, %v; want disabled", state, err)
	}
	staleOrphans, err := staleFixture.manager.OrphanedProjectionsAt(nil, ScopeProject)
	if err != nil || len(staleOrphans) != 1 || staleOrphans[0].Name != staleFixture.skill.Name {
		t.Fatalf("OrphanedProjectionsAt(stale copy) = %#v, %v", staleOrphans, err)
	}
	if err := staleFixture.manager.SetEnabled([]catalog.Skill{staleFixture.skill}, catalog.ClientAgy, true); err != nil {
		t.Fatalf("enabling over stale managed copy failed: %v", err)
	}
	if state, err := staleFixture.manager.State(staleFixture.skill, catalog.ClientAgy); err != nil || state != StateEnabled {
		t.Fatalf("state after replacing stale managed copy = %q, %v; want enabled", state, err)
	}
	_ = userHome
}

func TestProjectRootEqualsUserHomeDeduplicatesOperations(t *testing.T) {
	skillsRoot, homeAndProject, _, loaded, _ := setupCopyTestCatalog(t, "wiki")
	source := loaded.Sources[0]
	skill := source.Skills[0]
	manager := NewWithUserHome(homeAndProject, homeAndProject, loaded)

	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill.Path, "SKILL.md"), []byte("---\nname: wiki\ndescription: v2\n---\n# v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshed, err := manager.RefreshCopies(loaded.Sources, ScopeProject, ScopeGlobal)
	if err != nil || len(refreshed) != 1 {
		t.Fatalf("RefreshCopies(project==home) = %#v, %v", refreshed, err)
	}
	retirement, err := manager.RetireSource(source, ScopeProject, ScopeGlobal)
	if err != nil {
		t.Fatalf("RetireSource(project==home): %v", err)
	}
	if err := retirement.Restore(); err != nil {
		t.Fatalf("Restore(project==home): %v", err)
	}
	movedRoot := filepath.Join(skillsRoot, "local", "shared", "wiki-moved")
	if err := os.Rename(source.Path, movedRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetargetSource(source, movedRoot, ScopeProject, ScopeGlobal); err != nil {
		t.Fatalf("RetargetSource(project==home): %v", err)
	}
}

func TestHealthAtReportsIncompatibleEnabledBeforeStale(t *testing.T) {
	_, projectRoot, userHome, loaded, manager := setupCopyTestCatalog(t, "wiki")
	skill := loaded.Sources[0].Skills[0]
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}
	// Make SSOT stale AND drop agy from skill targets.
	if err := os.WriteFile(filepath.Join(skill.Path, "SKILL.md"), []byte("---\nname: wiki\ndescription: updated\n---\n# updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill.Targets = map[catalog.Client]bool{catalog.ClientCodex: true}
	health, err := manager.HealthAt(skill, catalog.ClientAgy, ScopeProject)
	if err != nil || health.State != StateIncompatibleEnabled {
		t.Fatalf("HealthAt(incompatible+stale) = %+v, %v; want StateIncompatibleEnabled", health, err)
	}
	if err := manager.SetEnabled([]catalog.Skill{skill}, catalog.ClientAgy, false); err != nil {
		t.Fatalf("disable incompatible stale copy: %v", err)
	}
	assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", "wiki"))
	_ = userHome
}

func assertNoStagingEntries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if isStagingEntryName(entry.Name()) {
			t.Fatalf("unexpected leftover staging/stash entry %q in %s", entry.Name(), dir)
		}
	}
}
