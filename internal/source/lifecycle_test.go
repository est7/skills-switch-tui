package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestLifecycleUpdatePrunesLastSkillFromProjectAndGlobalScopes(t *testing.T) {
	repositoryRoot := t.TempDir()
	skillsRoot := filepath.Join(repositoryRoot, "resources", "skills")
	sourcePath := filepath.Join(skillsRoot, "vendor", "shared", "repo")
	skillPath := filepath.Join(sourcePath, "skills", "one")
	writeSourceSkill(t, skillPath)
	if err := catalog.RegisterSource(skillsRoot, "vendor-shared/repo", catalog.SourcePolicy{Branch: "main"}); err != nil {
		t.Fatal(err)
	}

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	projectLink := filepath.Join(projectRoot, ".agents", "skills", "one")
	globalLink := filepath.Join(userHome, ".agents", "skills", "one")
	for _, link := range []string{projectLink, globalLink} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(skillPath, link); err != nil {
			t.Fatal(err)
		}
	}

	git := &recordingGit{responses: map[string]string{
		sourcePath + "|reset --hard HEAD":                                                  "",
		sourcePath + "|clean -ffdx":                                                        "",
		sourcePath + "|rev-parse HEAD":                                                     "aaaaaaaa\n",
		sourcePath + "|ls-remote origin refs/heads/main":                                   "bbbbbbbb\trefs/heads/main\n",
		repositoryRoot + "|submodule update --init -- resources/skills/vendor/shared/repo": "",
		sourcePath + "|fetch --no-tags origin refs/heads/main":                             "",
		sourcePath + "|reset --hard bbbbbbbb":                                              "",
		sourcePath + "|rev-parse --verify HEAD":                                            "bbbbbbbb\n",
	}}
	git.onCall = func(key string) {
		if strings.HasSuffix(key, "|reset --hard bbbbbbbb") {
			if err := os.RemoveAll(skillPath); err != nil {
				t.Fatalf("remove last upstream Skill: %v", err)
			}
		}
	}
	registry := client.DefaultRegistry()
	manager := Manager{RepositoryRoot: repositoryRoot, SkillsRoot: skillsRoot, Git: git, Clients: registry}
	selected := catalog.Source{
		ID: "vendor-shared/repo", Kind: catalog.SourceVendor, Scope: "shared",
		Path: sourcePath, Branch: "main",
	}

	outcome, err := (Lifecycle{Manager: manager, ProjectRoot: projectRoot, UserHome: userHome}).Update(
		context.Background(), []catalog.Source{selected}, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Pruned) != 2 {
		t.Fatalf("pruned = %#v, want project and global links", outcome.Pruned)
	}
	for _, link := range []string{projectLink, globalLink} {
		if _, statErr := os.Lstat(link); !os.IsNotExist(statErr) {
			t.Fatalf("last-Skill projection survived at %s: %v", link, statErr)
		}
	}
	refreshed, ok := outcome.Catalog.Source(selected.ID)
	if !ok || len(refreshed.Skills) != 0 {
		t.Fatalf("refreshed source = %#v, want a present zero-Skill source", refreshed)
	}
}

func TestLifecycleRemoveRetiresProjectAndGlobalProjections(t *testing.T) {
	repositoryRoot := t.TempDir()
	skillsRoot := filepath.Join(repositoryRoot, "resources", "skills")
	sourcePath := filepath.Join(skillsRoot, "vendor", "shared", "repo")
	skillPath := filepath.Join(sourcePath, "skills", "one")
	writeSourceSkill(t, skillPath)
	if err := catalog.RegisterSource(skillsRoot, "vendor-shared/repo", catalog.SourcePolicy{Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := loaded.Source("vendor-shared/repo")
	if !ok {
		t.Fatal("registered source not discovered")
	}

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	projectLink := filepath.Join(projectRoot, ".agents", "skills", "one")
	globalLink := filepath.Join(userHome, ".agents", "skills", "one")
	for _, link := range []string{projectLink, globalLink} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(skillPath, link); err != nil {
			t.Fatal(err)
		}
	}
	relative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "repo"))
	git := &recordingGit{responses: map[string]string{
		sourcePath + "|status --porcelain":                           "",
		repositoryRoot + "|rm -f -- " + relative:                     "",
		repositoryRoot + "|rev-parse --git-path modules/" + relative: "",
	}}
	manager := Manager{RepositoryRoot: repositoryRoot, SkillsRoot: skillsRoot, Git: git, Clients: loaded.Clients}

	if err := (Lifecycle{Manager: manager, ProjectRoot: projectRoot, UserHome: userHome}).Remove(context.Background(), selected); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{projectLink, globalLink} {
		if _, statErr := os.Lstat(link); !os.IsNotExist(statErr) {
			t.Fatalf("projection survived source removal at %s: %v", link, statErr)
		}
	}
}

func TestLifecycleRemoveRestoresProjectionWhenSourceRemovalFails(t *testing.T) {
	repositoryRoot := t.TempDir()
	skillsRoot := filepath.Join(repositoryRoot, "resources", "skills")
	sourcePath := filepath.Join(skillsRoot, "vendor", "shared", "repo")
	skillPath := filepath.Join(sourcePath, "skills", "one")
	writeSourceSkill(t, skillPath)
	loaded, err := catalog.Load(skillsRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := loaded.Source("vendor-shared/repo")
	if !ok {
		t.Fatal("source not discovered")
	}
	projectRoot := t.TempDir()
	projectLink := filepath.Join(projectRoot, ".agents", "skills", "one")
	if err := os.MkdirAll(filepath.Dir(projectLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillPath, projectLink); err != nil {
		t.Fatal(err)
	}
	manager := Manager{
		RepositoryRoot: repositoryRoot,
		SkillsRoot:     skillsRoot,
		Git:            &recordingGit{responses: map[string]string{}},
		Clients:        loaded.Clients,
	}

	err = (Lifecycle{Manager: manager, ProjectRoot: projectRoot}).Remove(context.Background(), selected)
	if err == nil || !strings.Contains(err.Error(), selected.ID) {
		t.Fatalf("Remove() error = %v, want attributed source failure", err)
	}
	target, readErr := os.Readlink(projectLink)
	if readErr != nil || filepath.Clean(target) != filepath.Clean(skillPath) {
		t.Fatalf("project projection was not restored: target=%q err=%v", target, readErr)
	}
}
