package source

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

// gitRunner skips the test when git is unavailable and returns a helper that
// runs git in a directory with a hermetic identity, failing the test on error.
// File-protocol submodules are enabled so a temporary directory can stand in for
// a remote.
func gitRunner(t *testing.T) func(dir string, args ...string) {
	t.Helper()
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	return func(dir string, args ...string) {
		t.Helper()
		command := exec.Command(gitBinary, args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
}

func TestUpdateHardResetsTrackedEditsBeforeUpdatingSubmodule(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote")
	skillFile := filepath.Join(remote, "skills", "tool", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("remote v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v1")

	parent := filepath.Join(base, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	run(parent, "init", "-q", "-b", "main")
	run(parent, "commit", "-q", "--allow-empty", "-m", "init")
	relative := filepath.FromSlash("resources/skills/vendor/shared/repo")
	run(parent, "submodule", "add", "-q", "-b", "main", remote, relative)
	run(parent, "commit", "-q", "-am", "add submodule")
	target := filepath.Join(parent, relative)
	targetSkill := filepath.Join(target, "skills", "tool", "SKILL.md")
	if err := os.WriteFile(targetSkill, []byte("local tracked edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("remote v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v2")

	manager := Manager{RepositoryRoot: parent, SkillsRoot: filepath.Join(parent, "resources", "skills"), Git: GitCommander{}}
	results, err := manager.Update(context.Background(), []catalog.Source{{
		ID: "vendor-shared/repo", Kind: catalog.SourceVendor, Scope: "shared", Path: target, Branch: "main",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Changed {
		t.Fatalf("Update() results = %#v, want one changed source", results)
	}
	contents, err := os.ReadFile(targetSkill)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "remote v2\n"; got != want {
		t.Fatalf("updated Skill = %q, want %q", got, want)
	}
}

func TestUpdateCleansUntrackedAndIgnoredSkillsFromReadOnlySource(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	base := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		command := exec.Command(gitBinary, args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		out, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), runErr, out)
		}
		return strings.TrimSpace(string(out))
	}

	remote := filepath.Join(base, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "skills", "remote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "skills", "remote", "SKILL.md"), []byte("remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "init")

	parent := filepath.Join(base, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	run(parent, "init", "-q", "-b", "main")
	run(parent, "commit", "-q", "--allow-empty", "-m", "init")
	relative := filepath.FromSlash("resources/skills/vendor/shared/repo")
	run(parent, "submodule", "add", "-q", "-b", "main", remote, relative)
	run(parent, "commit", "-q", "-am", "add submodule")
	target := filepath.Join(parent, relative)

	localOnly := filepath.Join(target, "skills", "local-only", "SKILL.md")
	ignored := filepath.Join(target, "ignored", "skill", "SKILL.md")
	for _, path := range []string{localOnly, ignored} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("local pollution\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	manager := Manager{RepositoryRoot: parent, SkillsRoot: filepath.Join(parent, "resources", "skills"), Git: GitCommander{}}
	results, err := manager.Update(context.Background(), []catalog.Source{{
		ID: "vendor-shared/repo", Kind: catalog.SourceVendor, Scope: "shared", Path: target, Branch: "main",
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Changed {
		t.Fatalf("Update() results = %#v, want one clean unchanged source", results)
	}
	for _, path := range []string{localOnly, ignored} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("read-only source pollution survived update at %s: %v", path, statErr)
		}
	}
	if status := run(target, "status", "--porcelain", "--ignored"); status != "" {
		t.Fatalf("read-only source remains dirty after update:\n%s", status)
	}
}

// TestAddRollbackCleansSubmoduleGitdirForReAdd proves that when an add fails
// after cloning and rolls back, the leftover .git/modules gitdir is removed so a
// subsequent add of the same path is not refused as an existing local repo.
func TestAddRollbackCleansSubmoduleGitdirForReAdd(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	base := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command(gitBinary, args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}

	remote := filepath.Join(base, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "skills", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "skills", "tool", "SKILL.md"), []byte("---\nname: tool\ndescription: t\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "init")

	agentsRoot := filepath.Join(base, "parent")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}

	// First add fails: the skill path does not exist, so discovery errors after
	// the clone and the rollback runs.
	if err := manager.Add(context.Background(), AddRequest{
		Name: "repo", URL: remote, Branch: "main", SkillPaths: []string{"does-not-exist"},
	}); err == nil {
		t.Fatal("add with a missing skill path unexpectedly succeeded")
	}
	relative := filepath.FromSlash("resources/skills/vendor/shared/repo")
	if _, err := os.Stat(filepath.Join(agentsRoot, ".git", "modules", relative)); !os.IsNotExist(err) {
		t.Fatalf("rollback left the submodule gitdir behind: %v", err)
	}

	// Re-adding the same path now succeeds instead of failing with "a git
	// directory ... is found locally".
	if err := manager.Add(context.Background(), AddRequest{
		Name: "repo", URL: remote, Branch: "main",
	}); err != nil {
		t.Fatalf("re-add after rollback failed: %v", err)
	}
}

// TestRemoveHandlesStagedSubmodule exercises the real git binary to prove the
// fix for a submodule that was `git submodule add`-ed but not yet committed: its
// own worktree is clean (so it passes the dirty preflight) while its gitlink is
// staged in the parent index. Plain `git rm` refuses such a path; `git rm -f`
// removes it. The recording-git tests only pin the command string and cannot
// observe this refusal, so this integration test is the meaningful regression.
func TestRemoveHandlesStagedSubmodule(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	// Allow local file:// submodules and keep the environment hermetic.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	base := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command(gitBinary, args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}

	// A remote repository to be used as the submodule.
	remote := filepath.Join(base, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	run(remote, "init", "-q", "-b", "main")
	run(remote, "commit", "-q", "--allow-empty", "-m", "init")

	// The parent repository holding the resource catalog.
	agentsRoot := filepath.Join(base, "parent")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")

	// Add the submodule but do NOT commit it: gitlink is staged in the index.
	relative := "resources/skills/vendor/shared/staged"
	run(agentsRoot, "submodule", "add", "-b", "main", remote, relative)
	target := filepath.Join(agentsRoot, filepath.FromSlash(relative))

	config := "version: 1\nsources:\n  vendor-shared/staged:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	if err := manager.Remove(context.Background(), catalog.Source{
		ID: "vendor-shared/staged", Kind: catalog.SourceVendor, Scope: "shared", Path: target,
	}); err != nil {
		t.Fatalf("Remove staged submodule: %v", err)
	}

	// The gitlink must be gone from the index.
	staged := exec.Command(gitBinary, "ls-files", "--stage")
	staged.Dir = agentsRoot
	out, err := staged.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files: %v: %s", err, out)
	}
	if strings.Contains(string(out), "vendor/shared/staged") {
		t.Fatalf("gitlink still staged after remove:\n%s", out)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("submodule worktree not removed: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "vendor-shared/staged") {
		t.Fatalf("catalog policy not removed:\n%s", updated)
	}
}

// TestAddSeparatesRepositoriesThatShareARepositoryName is the regression for two
// remotes named "skills" under different owners: each occupies its own
// owner/repo path, both stay discoverable, and re-adding a remote that is
// already tracked is refused by remote identity rather than by path collision.
func TestAddSeparatesRepositoriesThatShareARepositoryName(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	newRemote := func(name, skill string) string {
		remote := filepath.Join(base, name)
		writeSourceFile(t, filepath.Join(remote, "skills", skill, "SKILL.md"),
			"---\nname: "+skill+"\ndescription: test\n---\n")
		run(remote, "init", "-q", "-b", "main")
		run(remote, "add", "-A")
		run(remote, "commit", "-q", "-m", "v1")
		return remote
	}
	lencx := newRemote("lencx-skills", "lencx-tool")
	markdownViewer := newRemote("markdown-viewer-skills", "markdown-tool")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	ctx := context.Background()
	for _, added := range []struct{ name, url string }{
		{"lencx/skills", lencx},
		{"markdown-viewer/skills", markdownViewer},
	} {
		if err := manager.Add(ctx, AddRequest{Name: added.name, URL: added.url, Branch: "main", Scope: "shared"}); err != nil {
			t.Fatalf("Add(%s): %v", added.name, err)
		}
		checkout := filepath.Join(sourcesRoot, "vendor", "shared", filepath.FromSlash(added.name))
		if _, err := os.Stat(filepath.Join(checkout, "skills")); err != nil {
			t.Fatalf("checkout %s missing: %v", checkout, err)
		}
	}

	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	for _, id := range []string{"vendor-shared/lencx/skills", "vendor-shared/markdown-viewer/skills"} {
		source, ok := loaded.Source(id)
		if !ok {
			t.Fatalf("source %s was not discovered", id)
		}
		if len(source.Skills) != 1 {
			t.Fatalf("source %s skills = %#v, want exactly one", id, source.Skills)
		}
	}

	// The same remote under a name that does not collide on disk is still a
	// duplicate, and the failure names where it is already registered.
	err = manager.Add(ctx, AddRequest{Name: "mirror/skills", URL: lencx, Branch: "main", Scope: "shared"})
	if err == nil {
		t.Fatal("re-adding a tracked remote must fail")
	}
	if !strings.Contains(err.Error(), "vendor-shared/lencx/skills") {
		t.Fatalf("Add() error = %v, want the existing source id", err)
	}
	if _, statErr := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "mirror")); !os.IsNotExist(statErr) {
		t.Fatalf("refused add left an owner directory behind: %v", statErr)
	}

	// Removing the last repository under an owner takes the owner level with it.
	if err := manager.Remove(ctx, catalog.Source{
		ID: "vendor-shared/lencx/skills", Kind: catalog.SourceVendor, Scope: "shared",
		Path: filepath.Join(sourcesRoot, "vendor", "shared", "lencx", "skills"),
	}); err != nil {
		t.Fatalf("Remove(): %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "lencx")); !os.IsNotExist(err) {
		t.Fatalf("empty owner directory survived removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "markdown-viewer", "skills")); err != nil {
		t.Fatalf("unrelated source disturbed by removal: %v", err)
	}
}

// TestMigrateMovesLegacyCheckoutUnderItsOwner covers the whole migration: the
// checkout moves under the owner its remote names, .gitmodules and the catalog
// registration follow it, per-Skill overrides survive the rename, and the
// projections a user enabled are repointed at the new path instead of dangling.
func TestMigrateMovesLegacyCheckoutUnderItsOwner(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	// The remote's last two path segments are what names the source, so the
	// mirror is laid out the way a real owner/repo remote reads.
	remote := filepath.Join(base, "lencx", "skills")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: test\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	// The pre-owner-level layout: a bare repository name under the scope root.
	legacyRelative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "skills"))
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", remote, legacyRelative)
	legacyPath := filepath.Join(agentsRoot, filepath.FromSlash(legacyRelative))
	// Sources are checked out sparsely, which turns on extensions.worktreeConfig
	// and pins core.worktree in config.worktree. `git mv` does not rewrite that
	// file, so a migration that ignores it leaves an unusable checkout.
	run(legacyPath, "sparse-checkout", "init", "--cone")
	run(legacyPath, "sparse-checkout", "set", "skills")
	config := "version: 1\nsources:\n  vendor-shared/skills:\n    branch: main\n" +
		"overrides:\n  vendor-shared/skills/skills/tool:\n    targets: [claude]\n    reason: t\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Source("vendor-shared/skills"); !ok {
		t.Fatal("legacy source was not discovered")
	}

	// A projection the user enabled, which must survive the move.
	projectRoot := t.TempDir()
	projectLink := filepath.Join(projectRoot, ".claude", "skills", "tool")
	if err := os.MkdirAll(filepath.Dir(projectLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(legacyPath, "skills", "tool"), projectLink); err != nil {
		t.Fatal(err)
	}
	// A link this tool never created — no Skill carries that name — points into
	// the same checkout. Migration must not adopt and rewrite it.
	aliasLink := filepath.Join(projectRoot, ".claude", "skills", "my-alias")
	aliasTarget := filepath.Join(legacyPath, "skills", "tool")
	if err := os.Symlink(aliasTarget, aliasLink); err != nil {
		t.Fatal(err)
	}

	lifecycle := Lifecycle{
		Manager:     Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}, Clients: loaded.Clients},
		ProjectRoot: projectRoot,
	}
	planned, err := lifecycle.Migrate(context.Background(), loaded.Sources, true)
	if err != nil {
		t.Fatalf("Migrate(dry-run): %v", err)
	}
	if len(planned.Migrations) != 1 || !planned.Migrations[0].Actionable() {
		t.Fatalf("planned migrations = %#v, want one actionable move", planned.Migrations)
	}
	if _, err := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "skills", "skills", "tool")); err != nil {
		t.Fatalf("dry run moved the checkout: %v", err)
	}

	outcome, err := lifecycle.Migrate(context.Background(), loaded.Sources, false)
	if err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	migrated := outcome.Migrations[0]
	if migrated.TargetID != "vendor-shared/lencx/skills" {
		t.Fatalf("target id = %q, want the owner-qualified id", migrated.TargetID)
	}
	if _, err := os.Stat(filepath.Join(migrated.TargetPath, "skills", "tool", "SKILL.md")); err != nil {
		t.Fatalf("checkout did not move: %v", err)
	}
	if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy checkout path survived: %v", err)
	}

	// .gitmodules tracks the new path, so a later update still resolves.
	modules, err := os.ReadFile(filepath.Join(agentsRoot, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modules), "vendor/shared/lencx/skills") {
		t.Fatalf(".gitmodules was not rewritten:\n%s", modules)
	}
	if strings.Contains(string(modules), "path = resources/skills/vendor/shared/skills\n") {
		t.Fatalf(".gitmodules still tracks the old path:\n%s", modules)
	}

	// Registration and overrides moved together.
	updated, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vendor-shared/lencx/skills:", "vendor-shared/lencx/skills/skills/tool:"} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("catalog.yaml missing %q:\n%s", want, updated)
		}
	}
	// The rename must move the keys, not copy them.
	for _, stale := range []string{"vendor-shared/skills:", "vendor-shared/skills/skills/tool:"} {
		if strings.Contains(string(updated), stale) {
			t.Fatalf("catalog.yaml still holds %q:\n%s", stale, updated)
		}
	}

	// The enabled projection points into the new checkout, not at a dead path.
	target, err := os.Readlink(projectLink)
	if err != nil {
		t.Fatalf("projection was dropped: %v", err)
	}
	want := filepath.Join(migrated.TargetPath, "skills", "tool")
	if filepath.Clean(target) != filepath.Clean(want) {
		t.Fatalf("projection target = %q, want %q", target, want)
	}
	if _, err := os.Stat(filepath.Join(projectLink, "SKILL.md")); err != nil {
		t.Fatalf("repointed projection does not resolve: %v", err)
	}
	if alias, readErr := os.Readlink(aliasLink); readErr != nil || alias != aliasTarget {
		t.Fatalf("user-owned link was rewritten: target=%q err=%v", alias, readErr)
	}
	if _, ok := outcome.Catalog.Source("vendor-shared/lencx/skills"); !ok {
		t.Fatal("migrated source is not discoverable in the reloaded catalog")
	}
	// The moved checkout is still a working submodule, so a later update runs
	// against it rather than a broken gitdir pointer.
	run(migrated.TargetPath, "status", "--porcelain")
}

// TestMigrateStagesASourceNestedUnderItsOwnOwner covers a legacy source named
// after what turns out to be its own owner: the destination sits inside the
// source, which `git mv` refuses outright, so the move stages through a sibling.
func TestMigrateStagesASourceNestedUnderItsOwnOwner(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "legacy", "target")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: test\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	legacyRelative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "legacy"))
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", remote, legacyRelative)
	legacyPath := filepath.Join(agentsRoot, filepath.FromSlash(legacyRelative))
	run(legacyPath, "sparse-checkout", "init", "--cone")
	run(legacyPath, "sparse-checkout", "set", "skills")
	config := "version: 1\nsources:\n  vendor-shared/legacy:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := Lifecycle{Manager: Manager{
		RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}, Clients: loaded.Clients,
	}}
	outcome, err := lifecycle.Migrate(context.Background(), loaded.Sources, false)
	if err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	if len(outcome.Migrations) != 1 || outcome.Migrations[0].Status != MigrationMoved {
		t.Fatalf("migrations = %#v, want one moved source", outcome.Migrations)
	}
	moved := filepath.Join(sourcesRoot, "vendor", "shared", "legacy", "target")
	if _, err := os.Stat(filepath.Join(moved, "skills", "tool", "SKILL.md")); err != nil {
		t.Fatalf("nested destination was not populated: %v", err)
	}
	run(moved, "status", "--porcelain")
	if _, ok := outcome.Catalog.Source("vendor-shared/legacy/target"); !ok {
		t.Fatalf("migrated source missing from catalog: %#v", outcome.Catalog.Sources)
	}
	// The staging sibling is transient and must not survive the move.
	entries, err := os.ReadDir(filepath.Join(sourcesRoot, "vendor", "shared"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), catalog.StagingPrefix) {
			t.Fatalf("staging directory survived: %s", entry.Name())
		}
	}

	// The reverse move is what a rollback runs, and its destination is an
	// ancestor of the source — the other direction `git mv` refuses.
	legacyRoot := filepath.Join(sourcesRoot, "vendor", "shared", "legacy")
	if err := lifecycle.Manager.MigrateSource(context.Background(), Migration{
		SourceID: "vendor-shared/legacy/target", TargetID: "vendor-shared/legacy",
		Path: moved, TargetPath: legacyRoot, Status: MigrationPlanned,
	}); err != nil {
		t.Fatalf("reverse MigrateSource(): %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyRoot, "skills", "tool", "SKILL.md")); err != nil {
		t.Fatalf("reverse move did not restore the checkout: %v", err)
	}
	run(legacyRoot, "status", "--porcelain")
}

// TestMoveCheckoutLeavesNothingMovedWhenTheRepairFails proves the caller can
// trust a move failure: a `git mv` that succeeded but could not repoint the
// gitdir is undone rather than reported as an untouched checkout.
func TestMoveCheckoutLeavesNothingMovedWhenTheRepairFails(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "owner", "repo")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: test\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	legacyRelative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "repo"))
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", remote, legacyRelative)
	legacyPath := filepath.Join(agentsRoot, filepath.FromSlash(legacyRelative))
	run(legacyPath, "sparse-checkout", "init", "--cone")
	run(legacyPath, "sparse-checkout", "set", "skills")
	config := "version: 1\nsources:\n  vendor-shared/repo:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only the repair at the destination fails, so the move back can still leave
	// a fully usable checkout behind.
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: refusesWorktreeRepairAt("/owner/")}
	targetPath := filepath.Join(sourcesRoot, "vendor", "shared", "owner", "repo")
	err := manager.MigrateSource(context.Background(), Migration{
		SourceID: "vendor-shared/repo", TargetID: "vendor-shared/owner/repo",
		Path: legacyPath, TargetPath: targetPath, Status: MigrationPlanned,
	})
	if err == nil {
		t.Fatal("a move whose repair fails must fail")
	}
	if status := MigrationStatusOf(err); status != MigrationFailed {
		t.Fatalf("status = %q, want %q", status, MigrationFailed)
	}
	if _, statErr := os.Stat(filepath.Join(legacyPath, "skills", "tool", "SKILL.md")); statErr != nil {
		t.Fatalf("checkout was not left where it started: %v", statErr)
	}
	if _, statErr := os.Stat(targetPath); !os.IsNotExist(statErr) {
		t.Fatalf("checkout was left at the destination: %v", statErr)
	}
	// .gitmodules must not describe a move that was undone.
	modules, readErr := os.ReadFile(filepath.Join(agentsRoot, ".gitmodules"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(modules), "vendor/shared/owner/repo") {
		t.Fatalf(".gitmodules still records the undone move:\n%s", modules)
	}
	// The restored checkout still resolves its gitdir and its sparse config.
	run(legacyPath, "status", "--porcelain")
	run(legacyPath, "sparse-checkout", "list")
}

// scriptedGit runs real Git except where refuse says otherwise, so a test can
// fail one specific step of a relocation and observe what the code does with the
// rest of it.
type scriptedGit struct {
	GitCommander
	refuse func(arguments []string) bool
}

func (g scriptedGit) Output(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	if g.refuse != nil && g.refuse(arguments) {
		return nil, errors.New("simulated git failure")
	}
	return g.GitCommander.Output(ctx, directory, arguments...)
}

// refusesWorktreeRepairAt fails the config write that repoints a moved
// submodule's worktree whenever the new location contains marker.
func refusesWorktreeRepairAt(marker string) scriptedGit {
	return scriptedGit{refuse: func(arguments []string) bool {
		return isWorktreeRepair(arguments) && strings.Contains(arguments[len(arguments)-1], marker)
	}}
}

func isWorktreeRepair(arguments []string) bool {
	return len(arguments) > 0 && arguments[0] == "config" && slices.Contains(arguments, "core.worktree")
}

// TestMigrateSourceRestoresAUsableCheckoutWhenRegistrationFails proves the
// rollback leaves a working checkout: `git mv` alone would move the worktree back
// while leaving config.worktree pointing at the destination it never reached.
func TestMigrateSourceRestoresAUsableCheckoutWhenRegistrationFails(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "owner", "repo")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: test\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	legacyRelative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "repo"))
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", remote, legacyRelative)
	legacyPath := filepath.Join(agentsRoot, filepath.FromSlash(legacyRelative))
	run(legacyPath, "sparse-checkout", "init", "--cone")
	run(legacyPath, "sparse-checkout", "set", "skills")
	// The destination ID is already registered, so the rename after the move
	// fails and the checkout has to come back.
	config := "version: 1\nsources:\n  vendor-shared/repo:\n    branch: main\n  vendor-shared/owner/repo:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	targetPath := filepath.Join(sourcesRoot, "vendor", "shared", "owner", "repo")
	err := manager.MigrateSource(context.Background(), Migration{
		SourceID: "vendor-shared/repo", TargetID: "vendor-shared/owner/repo",
		Path: legacyPath, TargetPath: targetPath, Status: MigrationPlanned,
	})
	if err == nil {
		t.Fatal("migrating onto a registered id must fail")
	}
	// The checkout came back, and the reported status has to say so rather than
	// read as an untouched source.
	if status := MigrationStatusOf(err); status != MigrationRolledBack {
		t.Fatalf("status = %q, want %q", status, MigrationRolledBack)
	}
	if _, statErr := os.Stat(filepath.Join(legacyPath, "skills", "tool", "SKILL.md")); statErr != nil {
		t.Fatalf("checkout was not restored: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "owner")); !os.IsNotExist(statErr) {
		t.Fatalf("owner directory survived the rollback: %v", statErr)
	}
	// The restored checkout must still resolve its gitdir.
	run(legacyPath, "status", "--porcelain")
	run(legacyPath, "sparse-checkout", "list")
}

// TestMigrateSourcePrunesTheOwnerDirectoryWhenTheMoveFails keeps a failed move
// from leaving an empty owner behind, which discovery would then report as a
// source of its own.
func TestMigrateSourcePrunesTheOwnerDirectoryWhenTheMoveFails(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()
	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	untracked := filepath.Join(sourcesRoot, "vendor", "shared", "repo")
	writeSourceFile(t, filepath.Join(untracked, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: t\n---\n")
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	// An untracked directory cannot be moved by `git mv`.
	err := manager.MigrateSource(context.Background(), Migration{
		SourceID: "vendor-shared/repo", TargetID: "vendor-shared/owner/repo",
		Path:       untracked,
		TargetPath: filepath.Join(sourcesRoot, "vendor", "shared", "owner", "repo"),
		Status:     MigrationPlanned,
	})
	if err == nil {
		t.Fatal("moving an untracked checkout must fail")
	}
	if _, statErr := os.Stat(filepath.Join(sourcesRoot, "vendor", "shared", "owner")); !os.IsNotExist(statErr) {
		t.Fatalf("empty owner directory survived a failed move: %v", statErr)
	}
}

// TestRelocateReportsRecoveryThatAnInnerStepCouldNotMake covers the state a
// layered rollback produces: a step gives up mid-move, the layer above it puts
// the checkout back anyway, and the run must report "nothing moved" rather than
// leaving a user chasing a checkout that is already home.
func TestRelocateReportsRecoveryThatAnInnerStepCouldNotMake(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "legacy", "target")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: test\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "v1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	legacyRelative := filepath.ToSlash(filepath.Join("resources", "skills", "vendor", "shared", "legacy"))
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", remote, legacyRelative)
	legacyPath := filepath.Join(agentsRoot, filepath.FromSlash(legacyRelative))
	run(legacyPath, "sparse-checkout", "init", "--cone")
	run(legacyPath, "sparse-checkout", "set", "skills")
	config := "version: 1\nsources:\n  vendor-shared/legacy:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	// The destination repair always fails, and the staged repair fails on its
	// second call — the one inside the failed step's own rollback. Only the
	// outermost recovery, back to the original path, is allowed to succeed.
	stagedRepairs := 0
	git := scriptedGit{refuse: func(arguments []string) bool {
		if !isWorktreeRepair(arguments) {
			return false
		}
		worktree := arguments[len(arguments)-1]
		if strings.Contains(worktree, "legacy/target") {
			return true
		}
		if strings.Contains(worktree, catalog.StagingPrefix) {
			// One repair writes both config and config.worktree, so the staging
			// move out is the first pair and the failed step's own rollback is
			// the second.
			stagedRepairs++
			return stagedRepairs > 2
		}
		return false
	}}

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}
	err := manager.MigrateSource(context.Background(), Migration{
		SourceID: "vendor-shared/legacy", TargetID: "vendor-shared/legacy/target",
		Path:       legacyPath,
		TargetPath: filepath.Join(sourcesRoot, "vendor", "shared", "legacy", "target"),
		Status:     MigrationPlanned,
	})
	if err == nil {
		t.Fatal("a relocation whose repairs fail must fail")
	}
	if status := MigrationStatusOf(err); status != MigrationFailed {
		t.Fatalf("status = %q, want %q: the checkout is back where it started", status, MigrationFailed)
	}
	if _, statErr := os.Stat(filepath.Join(legacyPath, "skills", "tool", "SKILL.md")); statErr != nil {
		t.Fatalf("checkout was not restored: %v", statErr)
	}
	// Restored means usable, not merely present.
	run(legacyPath, "status", "--porcelain")
	run(legacyPath, "sparse-checkout", "list")
	entries, readErr := os.ReadDir(filepath.Join(sourcesRoot, "vendor", "shared"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), catalog.StagingPrefix) {
			t.Fatalf("staging directory survived: %s", entry.Name())
		}
	}
}
