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
	assertLinkTarget(t, original, ssot)
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
	if _, ok := loaded.Skill("local-shared/tools/portable"); !ok {
		t.Fatal("adopted skill is absent from the reloaded catalog")
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
	assertLinkTarget(t, valid, filepath.Join(skillsRoot, "local", "shared", "valid"))
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
