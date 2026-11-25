package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/projection"
)

func TestLifecycleUpdateRefreshesCleanManagedCopyAndPrunesRemovedCopy(t *testing.T) {
	repositoryRoot := t.TempDir()
	skillsRoot := filepath.Join(repositoryRoot, "resources", "skills")
	sourcePath := filepath.Join(skillsRoot, "vendor", "shared", "repo")
	liveSkill := filepath.Join(sourcePath, "skills", "live")
	removedSkill := filepath.Join(sourcePath, "skills", "removed")
	writeSourceSkill(t, liveSkill)
	writeSourceSkill(t, removedSkill)
	if err := catalog.RegisterSource(skillsRoot, "vendor-shared/repo", catalog.SourcePolicy{Branch: "main"}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	registry := client.DefaultRegistry()
	loaded, err := catalog.Load(skillsRoot, registry)
	if err != nil {
		t.Fatal(err)
	}
	proj := projection.NewWithUserHome(projectRoot, userHome, loaded)
	if err := proj.SetEnabled(loaded.Sources[0].Skills, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}

	git := &recordingGit{responses: map[string]string{
		sourcePath + "|reset --hard HEAD":                      "",
		sourcePath + "|clean -ffdx":                            "",
		sourcePath + "|rev-parse HEAD":                         "aaaaaaaa\n",
		sourcePath + "|ls-remote origin refs/heads/main":       "bbbbbbbb\trefs/heads/main\n",
		sourcePath + "|rev-parse --is-shallow-repository":      "false\n",
		sourcePath + "|fetch --no-tags origin refs/heads/main": "",
		sourcePath + "|reset --hard bbbbbbbb":                  "",
		sourcePath + "|rev-parse --verify HEAD":                "bbbbbbbb\n",
	}}
	git.onCall = func(key string) {
		if strings.HasSuffix(key, "|reset --hard bbbbbbbb") {
			if err := os.WriteFile(filepath.Join(liveSkill, "SKILL.md"), []byte("---\nname: live\ndescription: v2\n---\n# v2\n"), 0o644); err != nil {
				t.Fatalf("update live skill: %v", err)
			}
			if err := os.RemoveAll(removedSkill); err != nil {
				t.Fatalf("remove upstream skill: %v", err)
			}
		}
	}

	manager := Manager{RepositoryRoot: repositoryRoot, SkillsRoot: skillsRoot, Git: git, Clients: registry}
	selected := catalog.Source{
		ID: "vendor-shared/repo", Kind: catalog.SourceVendor, Scope: "shared",
		Path: sourcePath, Branch: "main",
	}
	outcome, err := (Lifecycle{Manager: manager, ProjectRoot: projectRoot, UserHome: userHome}).Update(
		context.Background(), []catalog.Source{selected}, false,
	)
	if err != nil {
		t.Fatalf("Lifecycle.Update: %v", err)
	}
	if len(outcome.Pruned) != 1 || outcome.Pruned[0].Name != "removed" {
		t.Fatalf("pruned = %#v, want removed managed copy", outcome.Pruned)
	}
	if len(outcome.Refreshed) != 1 || outcome.Refreshed[0].Name != "live" || outcome.Refreshed[0].Client != catalog.ClientAgy || outcome.Refreshed[0].Source != "vendor-shared/repo" {
		t.Fatalf("refreshed = %#v, want live refreshed copy attributed to agy", outcome.Refreshed)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "removed")); !os.IsNotExist(err) {
		t.Fatalf("removed copy survived update: %v", err)
	}
	liveCopied, err := os.ReadFile(filepath.Join(projectRoot, ".agents", "skills", "live", "SKILL.md"))
	if err != nil || !strings.Contains(string(liveCopied), "# v2") {
		t.Fatalf("live managed copy was not refreshed: %q, %v", liveCopied, err)
	}
	reloadedProj := projection.NewWithUserHome(projectRoot, userHome, outcome.Catalog)
	live, ok := outcome.Catalog.Skill("vendor-shared/repo/skills/live")
	if !ok {
		t.Fatal("live skill missing after reload")
	}
	if state, err := reloadedProj.State(live, catalog.ClientAgy); err != nil || state != projection.StateEnabled {
		t.Fatalf("live agy state after update = %q, %v; want enabled", state, err)
	}
}

func TestLifecycleRemoveRestoresOriginalCopyFromStashOnDirtyFailure(t *testing.T) {
	repositoryRoot := t.TempDir()
	skillsRoot := filepath.Join(repositoryRoot, "resources", "skills")
	sourcePath := filepath.Join(skillsRoot, "vendor", "shared", "repo")
	skillDir := filepath.Join(sourcePath, "skills", "live")
	writeSourceSkill(t, skillDir)
	if err := catalog.RegisterSource(skillsRoot, "vendor-shared/repo", catalog.SourcePolicy{Branch: "main"}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	registry := client.DefaultRegistry()
	loaded, err := catalog.Load(skillsRoot, registry)
	if err != nil {
		t.Fatal(err)
	}
	proj := projection.NewWithUserHome(projectRoot, userHome, loaded)
	if err := proj.SetEnabled(loaded.Sources[0].Skills, catalog.ClientAgy, true); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(projectRoot, ".agents", "skills", "live")
	origSkillMD, err := os.ReadFile(filepath.Join(copyPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	// Simulate dirty vendor checkout (missing SKILL.md + untracked file) causing Remove to fail.
	if err := os.Remove(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	relative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "repo"))
	dirtyGit := &recordingGit{responses: map[string]string{
		repositoryRoot + "|ls-files --stage -- " + relative: "160000 0000000000000000000000000000000000000000 0\t" + relative + "\n",
		sourcePath + "|status --porcelain":                  "?? skills/live/dirty.txt\n",
	}}
	manager := Manager{RepositoryRoot: repositoryRoot, SkillsRoot: skillsRoot, Git: dirtyGit, Clients: registry}
	lifecycle := Lifecycle{Manager: manager, ProjectRoot: projectRoot, UserHome: userHome}

	removeErr := lifecycle.Remove(context.Background(), loaded.Sources[0])
	if removeErr == nil || !strings.Contains(removeErr.Error(), "dirty") {
		t.Fatalf("Lifecycle.Remove on dirty checkout = %v, want dirty-checkout error", removeErr)
	}
	// Exact original managed copy must be restored from stash (no dirty file, no missing SKILL.md error).
	gotSkillMD, err := os.ReadFile(filepath.Join(copyPath, "SKILL.md"))
	if err != nil || string(gotSkillMD) != string(origSkillMD) {
		t.Fatalf("restored SKILL.md = %q, %v; want original %q", gotSkillMD, err, origSkillMD)
	}
	if _, err := os.Lstat(filepath.Join(copyPath, "dirty.txt")); !os.IsNotExist(err) {
		t.Fatalf("dirty file leaked into restored copy: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(copyPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".skills-switch-stash-") {
			t.Fatalf("stash survived restore: %s", entry.Name())
		}
	}

	// Now succeed on a clean removal and confirm defer retirement.Commit() cleans up the stash.
	cleanGit := &recordingGit{responses: map[string]string{
		repositoryRoot + "|ls-files --stage -- " + relative:          "160000 0000000000000000000000000000000000000000 0\t" + relative + "\n",
		sourcePath + "|status --porcelain":                           "",
		repositoryRoot + "|rm -f -- " + relative:                     "",
		repositoryRoot + "|rev-parse --git-path modules/" + relative: "",
	}}
	lifecycle.Manager.Git = cleanGit
	if err := lifecycle.Remove(context.Background(), loaded.Sources[0]); err != nil {
		t.Fatalf("Lifecycle.Remove(clean): %v", err)
	}
	if _, err := os.Lstat(copyPath); !os.IsNotExist(err) {
		t.Fatalf("managed copy survived clean removal: %v", err)
	}
	entries, err = os.ReadDir(filepath.Dir(copyPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".skills-switch-stash-") {
			t.Fatalf("stash survived clean removal Commit(): %s", entry.Name())
		}
	}
}
