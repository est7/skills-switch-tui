package projection

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestAdoptSkillsMovesContentToCatalogAndReprojectsOriginal(t *testing.T) {
	projectRoot := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectRoot, ".agents", "skills", "portable")
	writeAdoptionSkill(t, original, "portable")
	if err := os.WriteFile(filepath.Join(original, "script.sh"), []byte("#!/bin/sh\necho portable\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("script.sh", filepath.Join(original, "script-link")); err != nil {
		t.Fatal(err)
	}

	manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)
	results, err := manager.AdoptSkills([]string{original}, "shared", "tools")
	if err != nil {
		t.Fatal(err)
	}
	ssot := filepath.Join(skillsRoot, "local", "shared", "tools", "portable")
	want := []Adoption{{Path: original, SkillID: "local-shared/tools/portable", SSOTPath: ssot, Status: AdoptionAdopted}}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("AdoptSkills() = %#v, want %#v", results, want)
	}
	entry, err := inspectEntry(original)
	if err != nil || entry.kind != entryManagedCopy || entry.resolvedTarget != canonicalPath(ssot) {
		t.Fatalf("adopted .agents/skills projection = %+v, %v; want managed copy targeting %s", entry, err, canonicalPath(ssot))
	}
	if got, err := os.Readlink(filepath.Join(ssot, "script-link")); err != nil || got != "script.sh" {
		t.Fatalf("copied internal symlink = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(ssot, "script.sh")); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("copied mode = %v, %v", info, err)
	}
	loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	adoptedSkill, ok := loaded.Skill("local-shared/tools/portable")
	if !ok {
		t.Fatal("adopted skill is absent from the reloaded catalog")
	}
	reloadedManager := NewWithUserHome(projectRoot, t.TempDir(), loaded)
	for _, clientID := range []catalog.Client{catalog.ClientCodex, catalog.ClientAgy} {
		if state, err := reloadedManager.State(adoptedSkill, clientID); err != nil || state != StateEnabled {
			t.Fatalf("%s state after adopt = %q, %v; want enabled", clientID, state, err)
		}
	}
	discovered, err := manager.DiscoverUnmanagedSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range discovered {
		if skill.Path == original {
			t.Fatal("adopted projection is still reported as unmanaged")
		}
	}
}

func TestAdoptSkillsRefusesIneligibleAndCollidingPaths(t *testing.T) {
	projectRoot := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)
	outside := filepath.Join(t.TempDir(), "outside")
	writeAdoptionSkill(t, outside, "outside")
	managedTarget := filepath.Join(t.TempDir(), "managed")
	writeAdoptionSkill(t, managedTarget, "linked")
	linked := filepath.Join(projectRoot, ".agents", "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(linked), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(managedTarget, linked); err != nil {
		t.Fatal(err)
	}
	colliding := filepath.Join(projectRoot, ".agents", "skills", "colliding")
	writeAdoptionSkill(t, colliding, "colliding")
	collisionSSOT := filepath.Join(skillsRoot, "local", "shared", "colliding")
	if err := os.MkdirAll(collisionSSOT, 0o755); err != nil {
		t.Fatal(err)
	}

	results, err := manager.AdoptSkills([]string{linked, outside, colliding}, "shared", "")
	if err == nil {
		t.Fatal("refused adoptions must return an aggregate error")
	}
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
	for _, result := range results {
		if result.Status != AdoptionRefused || result.Reason == "" {
			t.Fatalf("refusal = %#v", result)
		}
	}
	assertLinkTarget(t, linked, managedTarget)
	if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); err != nil {
		t.Fatalf("outside skill changed: %v", err)
	}
	if info, err := os.Lstat(colliding); err != nil || !info.IsDir() {
		t.Fatalf("colliding original changed: %v, %v", info, err)
	}
}

func TestAdoptSkillsRestoresOriginalAndRemovesSSOTAfterPostBackupFailure(t *testing.T) {
	projectRoot := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectRoot, ".agents", "skills", "fragile")
	writeAdoptionSkill(t, original, "fragile")
	if err := os.WriteFile(filepath.Join(original, "payload.bin"), []byte{0, 1, 2, 3}, 0o640); err != nil {
		t.Fatal(err)
	}
	want := adoptionTreeSnapshot(t, original)
	manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)
	manager.beforeAdoptLink = func(path, backupPath, ssotPath string) error {
		if _, err := os.Stat(backupPath); err != nil {
			t.Fatalf("backup not present at injection seam: %v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("original still present at injection seam: %v", err)
		}
		return errors.New("injected post-backup failure")
	}

	results, err := manager.AdoptSkills([]string{original}, "shared", "")
	if err == nil || !strings.Contains(err.Error(), "injected post-backup failure") {
		t.Fatalf("adopt error = %v", err)
	}
	if len(results) != 1 || results[0].Status != AdoptionFailed {
		t.Fatalf("results = %#v", results)
	}
	if got := adoptionTreeSnapshot(t, original); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored tree differs:\n got %#v\nwant %#v", got, want)
	}
	assertMissing(t, original+".adopting-backup")
	assertMissing(t, filepath.Join(skillsRoot, "local", "shared", "fragile"))
}

func TestAdoptSkillsContinuesAfterPerItemFailure(t *testing.T) {
	projectRoot := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	fragile := filepath.Join(projectRoot, ".agents", "skills", "fragile")
	writeAdoptionSkill(t, fragile, "fragile")
	valid := filepath.Join(projectRoot, ".agents", "skills", "valid")
	writeAdoptionSkill(t, valid, "valid")
	manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)
	manager.beforeAdoptLink = func(path, _, _ string) error {
		if path == fragile {
			return errors.New("injected first-item failure")
		}
		return nil
	}

	results, err := manager.AdoptSkills([]string{fragile, valid}, "shared", "")
	if err == nil {
		t.Fatal("mixed outcomes must return an aggregate error")
	}
	if got := []AdoptionStatus{results[0].Status, results[1].Status}; !reflect.DeepEqual(got, []AdoptionStatus{AdoptionFailed, AdoptionAdopted}) {
		t.Fatalf("statuses = %#v", got)
	}
	if info, statErr := os.Lstat(fragile); statErr != nil || !info.IsDir() {
		t.Fatalf("failed first item was not restored: %v, %v", info, statErr)
	}
	validSSOT := filepath.Join(skillsRoot, "local", "shared", "valid")
	if entry, entryErr := inspectEntry(valid); entryErr != nil || entry.kind != entryManagedCopy || entry.resolvedTarget != canonicalPath(validSSOT) {
		t.Fatalf("adopted second item = %+v, %v; want managed copy targeting %s", entry, entryErr, canonicalPath(validSSOT))
	}
}

func TestAdoptSkillsReportsStrandedPathsWhenRestoreIsBlocked(t *testing.T) {
	projectRoot := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectRoot, ".agents", "skills", "blocked")
	writeAdoptionSkill(t, original, "blocked")
	manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)
	manager.beforeAdoptLink = func(path, _, _ string) error {
		if err := os.WriteFile(path, []byte("concurrent"), 0o600); err != nil {
			t.Fatal(err)
		}
		return errors.New("injected failure with occupied original")
	}

	results, err := manager.AdoptSkills([]string{original}, "shared", "")
	if err == nil || len(results) != 1 || results[0].Status != AdoptionStranded {
		t.Fatalf("stranded adoption = %#v, %v", results, err)
	}
	backup := original + ".adopting-backup"
	ssot := filepath.Join(skillsRoot, "local", "shared", "blocked")
	for _, path := range []string{original, backup, ssot} {
		if !strings.Contains(results[0].Reason, path) {
			t.Fatalf("stranded reason %q omits %s", results[0].Reason, path)
		}
	}
	if info, statErr := os.Lstat(backup); statErr != nil || !info.IsDir() {
		t.Fatalf("stranded backup missing: %v, %v", info, statErr)
	}
	assertMissing(t, ssot)
}

func TestAdoptSkillsMatchesClientModeForCopyOnlySymlinkOnlyAndGlobalDirs(t *testing.T) {
	projectRoot := t.TempDir()
	userHome := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"copyonly": {ProjectSkillsDir: ".copyonly/skills", ProjectSkillsMode: client.SkillModeCopy},
	})
	if err != nil {
		t.Fatal(err)
	}
	copyOnlySkill := filepath.Join(projectRoot, ".copyonly", "skills", "standalone")
	claudeSkill := filepath.Join(projectRoot, ".claude", "skills", "symlinked")
	agyGlobalSkill := filepath.Join(userHome, ".gemini", "config", "skills", "globaled")
	writeAdoptionSkill(t, copyOnlySkill, "standalone")
	writeAdoptionSkill(t, claudeSkill, "symlinked")
	writeAdoptionSkill(t, agyGlobalSkill, "globaled")

	manager := NewWithUserHome(projectRoot, userHome, catalog.Catalog{Root: skillsRoot, Clients: registry})
	results, err := manager.AdoptSkills([]string{copyOnlySkill, claudeSkill, agyGlobalSkill}, "shared", "")
	if err != nil {
		t.Fatalf("AdoptSkills: %v", err)
	}
	for _, r := range results {
		if r.Status != AdoptionAdopted {
			t.Fatalf("adoption result = %#v, want adopted", r)
		}
	}

	// Standalone copy-mode dir -> managed copy, enabled for copyonly.
	copyEntry, err := inspectEntry(copyOnlySkill)
	if err != nil || copyEntry.kind != entryManagedCopy || copyEntry.resolvedTarget != canonicalPath(results[0].SSOTPath) {
		t.Fatalf("copyonly adopted projection = %+v, %v; want managed copy", copyEntry, err)
	}
	loaded, err := catalog.Load(skillsRoot, registry)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := NewWithUserHome(projectRoot, userHome, loaded)
	standalone, ok := loaded.Skill("local-shared/standalone/standalone")
	if !ok {
		t.Fatal("standalone skill missing from catalog")
	}
	if state, err := reloaded.State(standalone, client.ID("copyonly")); err != nil || state != StateEnabled {
		t.Fatalf("copyonly state after adopt = %q, %v; want enabled", state, err)
	}

	// Symlink-only project dir and global dir -> symlinks.
	assertLinkTarget(t, claudeSkill, results[1].SSOTPath)
	assertLinkTarget(t, agyGlobalSkill, results[2].SSOTPath)
}

func TestAdoptSkillsCopyModeRollsBackOnInvalidSymlinkMaterialization(t *testing.T) {
	projectRoot := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(projectRoot, ".agents", "skills", "escaping")
	writeAdoptionSkill(t, original, "escaping")
	if err := os.Symlink("/etc/hosts", filepath.Join(original, "abs-link")); err != nil {
		t.Fatal(err)
	}
	want := adoptionTreeSnapshot(t, original)

	manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)
	results, err := manager.AdoptSkills([]string{original}, "shared", "")
	if err == nil || len(results) != 1 || results[0].Status != AdoptionFailed {
		t.Fatalf("AdoptSkills(escaping symlink) = (%#v, %v); want AdoptionFailed with rollback", results, err)
	}
	if got := adoptionTreeSnapshot(t, original); !reflect.DeepEqual(got, want) {
		t.Fatalf("original tree not restored after copy-mode adopt failure:\n got %#v\nwant %#v", got, want)
	}
	assertMissing(t, original+".adopting-backup")
	assertMissing(t, filepath.Join(skillsRoot, "local", "shared", "escaping"))
}

func TestAdoptSkillsRespectsScopeOnSharedAgentsDirAndCanonicalizesPath(t *testing.T) {
	t.Run("codex scope on .agents/skills adopts as symlink keeping agy incompatible", func(t *testing.T) {
		projectRoot := t.TempDir()
		userHome := t.TempDir()
		skillsRoot := filepath.Join(t.TempDir(), "skills")
		if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(projectRoot, ".agents", "skills", "codex-tool")
		writeAdoptionSkill(t, original, "codex-tool")
		manager := adoptionManager(projectRoot, userHome, skillsRoot)

		results, err := manager.AdoptSkills([]string{original}, "codex", "")
		if err != nil || len(results) != 1 || results[0].Status != AdoptionAdopted {
			t.Fatalf("AdoptSkills(--scope codex) = %#v, %v", results, err)
		}
		ssot := filepath.Join(skillsRoot, "local", "codex", "codex-tool")
		assertLinkTarget(t, original, ssot)

		loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
		if err != nil {
			t.Fatal(err)
		}
		skill, ok := loaded.Skill("local-codex-only/codex-tool/codex-tool")
		if !ok {
			t.Fatal("adopted codex-only skill missing from catalog")
		}
		reloaded := NewWithUserHome(projectRoot, userHome, loaded)
		if state, err := reloaded.State(skill, catalog.ClientCodex); err != nil || state != StateEnabled {
			t.Fatalf("codex state = %q, %v; want enabled", state, err)
		}
		if state, err := reloaded.State(skill, catalog.ClientAgy); err != nil || state != StateIncompatible {
			t.Fatalf("agy state = %q, %v; want incompatible", state, err)
		}
	})

	t.Run("agy scope on shared .agents/skills is refused in preflight", func(t *testing.T) {
		projectRoot := t.TempDir()
		skillsRoot := filepath.Join(t.TempDir(), "skills")
		if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(projectRoot, ".agents", "skills", "agy-only")
		writeAdoptionSkill(t, original, "agy-only")
		manager := adoptionManager(projectRoot, t.TempDir(), skillsRoot)

		results, err := manager.AdoptSkills([]string{original}, "agy", "")
		if err == nil || len(results) != 1 || results[0].Status != AdoptionRefused {
			t.Fatalf("AdoptSkills(--scope agy on .agents/skills) = %#v, %v; want refused", results, err)
		}
		for _, want := range []string{"codex", "--scope shared"} {
			if !strings.Contains(results[0].Reason, want) {
				t.Fatalf("refusal reason %q missing %q", results[0].Reason, want)
			}
		}
		if info, statErr := os.Lstat(original); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			t.Fatalf("original dir altered after refusal: %v, %v", info, statErr)
		}
		assertMissing(t, filepath.Join(skillsRoot, "local", "agy", "agy-only"))
	})

	t.Run("standalone copy-mode client scope adopts as managed copy", func(t *testing.T) {
		projectRoot := t.TempDir()
		skillsRoot := filepath.Join(t.TempDir(), "skills")
		if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		registry, err := client.NewRegistry(map[client.ID]client.Definition{
			"copyonly": {ProjectSkillsDir: ".copyonly/skills", ProjectSkillsMode: client.SkillModeCopy},
		})
		if err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(projectRoot, ".copyonly", "skills", "solo")
		writeAdoptionSkill(t, original, "solo")
		manager := NewWithUserHome(projectRoot, t.TempDir(), catalog.Catalog{Root: skillsRoot, Clients: registry})

		results, err := manager.AdoptSkills([]string{original}, "copyonly", "")
		if err != nil || len(results) != 1 || results[0].Status != AdoptionAdopted {
			t.Fatalf("AdoptSkills(--scope copyonly) = %#v, %v", results, err)
		}
		entry, err := inspectEntry(original)
		if err != nil || entry.kind != entryManagedCopy || entry.resolvedTarget != canonicalPath(results[0].SSOTPath) {
			t.Fatalf("copyonly scoped adopt entry = %+v, %v; want managed copy", entry, err)
		}
	})

	t.Run("symlinked project root alias matches discovered unmanaged skill", func(t *testing.T) {
		base := t.TempDir()
		realProject := filepath.Join(base, "real-project")
		aliasProject := filepath.Join(base, "alias-project")
		if err := os.MkdirAll(realProject, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(realProject, aliasProject); err != nil {
			t.Fatal(err)
		}
		skillsRoot := filepath.Join(t.TempDir(), "skills")
		if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		writeAdoptionSkill(t, filepath.Join(realProject, ".agents", "skills", "aliased"), "aliased")

		// Manager uses realProject while caller passes aliasProject path (mirrors macOS /tmp vs /private/tmp).
		manager := adoptionManager(realProject, t.TempDir(), skillsRoot)
		aliasSkillPath := filepath.Join(aliasProject, ".agents", "skills", "aliased")
		results, err := manager.AdoptSkills([]string{aliasSkillPath}, "shared", "")
		if err != nil || len(results) != 1 || results[0].Status != AdoptionAdopted {
			t.Fatalf("AdoptSkills(aliased path) = %#v, %v; want adopted", results, err)
		}
	})
}

func adoptionManager(projectRoot, userHome, skillsRoot string) Manager {
	return NewWithUserHome(projectRoot, userHome, catalog.Catalog{Root: skillsRoot, Clients: client.DefaultRegistry()})
}

func writeAdoptionSkill(t *testing.T, directory, name string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o751); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + name + "\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(contents), 0o640); err != nil {
		t.Fatal(err)
	}
}

type adoptionSnapshotEntry struct {
	Path string
	Mode os.FileMode
	Data string
}

func adoptionTreeSnapshot(t *testing.T, root string) []adoptionSnapshotEntry {
	t.Helper()
	var result []adoptionSnapshotEntry
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := adoptionSnapshotEntry{Path: relative, Mode: info.Mode()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			item.Data, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(path)
			item.Data = string(data)
		}
		result = append(result, item)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
