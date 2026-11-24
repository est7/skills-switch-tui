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

// TestAddMissingBranchLeavesNoStrandedCheckout pins the failure that stranded a
// half-clone: `git submodule add -b <branch>` clones into .git/modules and only
// then discovers the branch is missing, exiting before the gitlink is staged.
// The checkout it leaves holds nothing but a .git pointer, is discovered as a
// source, and blocks every later add of the same repository.
func TestAddMissingBranchLeavesNoStrandedCheckout(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	base := t.TempDir()
	run := realGitRunner(t, gitBinary)
	remote := filepath.Join(base, "remote")
	writeSourceSkill(t, filepath.Join(remote, "skills", "tool"))
	run(remote, "init", "-q", "-b", "master")
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
	err = manager.Add(context.Background(), AddRequest{Name: "owner/repo", URL: remote, Branch: "main"})
	if err == nil {
		t.Fatal("add of a missing branch unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "Remote branch main") {
		t.Fatalf("add error does not name the missing branch: %v", err)
	}
	target := filepath.Join(sourcesRoot, "vendor", "shared", "owner", "repo")
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("failed add left the checkout behind at %s: %v", target, err)
	}
	if _, err := os.Lstat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatalf("failed add left the owner directory behind: %v", err)
	}
	relative := filepath.FromSlash("resources/skills/vendor/shared/owner/repo")
	if _, err := os.Stat(filepath.Join(agentsRoot, ".git", "modules", relative)); !os.IsNotExist(err) {
		t.Fatalf("failed add left the submodule gitdir behind: %v", err)
	}

	if err := manager.Add(context.Background(), AddRequest{Name: "owner/repo", URL: remote, Branch: "master"}); err != nil {
		t.Fatalf("add with the real branch after the failed add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "skills", "tool", "SKILL.md")); err != nil {
		t.Fatalf("checkout after successful add: %v", err)
	}
}

// TestRemoveCleansOrphanCheckoutLeftByFailedAdd covers the checkout an older
// build stranded: a .git pointer into .git/modules with no gitlink in the parent
// index and no catalog policy. Discovery still lists it, its status shows every
// tracked file as a staged deletion, and `git rm` has nothing to remove. Remove
// must recognise the orphan and delete it instead of guarding "local changes"
// nobody made.
func TestRemoveCleansOrphanCheckoutLeftByFailedAdd(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	base := t.TempDir()
	run := realGitRunner(t, gitBinary)
	remote := filepath.Join(base, "remote")
	writeSourceSkill(t, filepath.Join(remote, "skills", "tool"))
	run(remote, "init", "-q", "-b", "master")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "init")

	agentsRoot := filepath.Join(base, "parent")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")

	relative := "resources/skills/vendor/shared/owner/repo"
	strand := exec.Command(gitBinary, "submodule", "add", "-b", "main", remote, relative)
	strand.Dir = agentsRoot
	if out, err := strand.CombinedOutput(); err == nil {
		t.Fatalf("submodule add of a missing branch unexpectedly succeeded: %s", out)
	}
	target := filepath.Join(agentsRoot, filepath.FromSlash(relative))
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatalf("fixture did not strand a checkout: %v", err)
	}

	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	var orphan catalog.Source
	for _, source := range loaded.Sources {
		if source.ID == "vendor-shared/owner/repo" {
			orphan = source
		}
	}
	if orphan.ID == "" {
		t.Fatalf("stranded checkout was not discovered; sources = %v", loaded.Sources)
	}

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	if err := manager.Remove(context.Background(), orphan); err != nil {
		t.Fatalf("remove orphan checkout: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("orphan checkout survived removal: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatalf("owner directory survived removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsRoot, ".git", "modules", filepath.FromSlash(relative))); !os.IsNotExist(err) {
		t.Fatalf("orphan gitdir survived removal: %v", err)
	}
	if err := manager.Add(context.Background(), AddRequest{Name: "owner/repo", URL: remote, Branch: "master"}); err != nil {
		t.Fatalf("re-add after orphan removal: %v", err)
	}
}

func realGitRunner(t *testing.T, gitBinary string) func(dir string, args ...string) {
	t.Helper()
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

// TestRemoveReachesSourceThatFailedDiscovery is the regression for a vendor
// repository whose skills cannot be scanned — an upstream SKILL.md whose name is
// not a legal projection path. Before per-source isolation, such a repository
// took the whole catalog down with it, and every command including remove failed
// on the same scan, so the only way out was hand-editing git state.
func TestRemoveReachesSourceThatFailedDiscovery(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "skills", "bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "skills", "bad", "SKILL.md"), []byte(`---
name: Bad Name
description: Upstream skill whose name cannot be a projection path.
---
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "init")

	agentsRoot := filepath.Join(base, "parent")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	healthy := filepath.Join(sourcesRoot, "local", "shared", "keep", "good")
	if err := os.MkdirAll(healthy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(healthy, "SKILL.md"), []byte(`---
name: good
description: A local skill that must survive the broken vendor repository.
---
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "add", "-A")
	run(agentsRoot, "commit", "-q", "-m", "init")

	relative := "resources/skills/vendor/shared/acme/broken"
	run(agentsRoot, "submodule", "add", "-b", "main", remote, relative)
	target := filepath.Join(agentsRoot, filepath.FromSlash(relative))
	config := "version: 1\nsources:\n  vendor-shared/acme/broken:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatalf("catalog.Load() error = %v, want the broken repository isolated", err)
	}
	if _, ok := loaded.Skill("local-shared/keep/good"); !ok {
		t.Fatal("healthy local skill lost because a vendor repository failed discovery")
	}
	var broken catalog.Source
	for _, source := range loaded.Sources {
		if source.ID == "vendor-shared/acme/broken" {
			broken = source
		}
	}
	if !broken.IsDiscoveryFailed() {
		t.Fatalf("broken source availability = %q, want discovery-failed", broken.Availability)
	}

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	if err := manager.Remove(context.Background(), broken); err != nil {
		t.Fatalf("Remove() error = %v, want a failed source to stay removable", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("checkout not removed: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "vendor-shared/acme/broken") {
		t.Fatalf("catalog policy not removed:\n%s", updated)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	command := exec.Command(gitBinary, args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func fileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}

func TestAddShallowClonesDepthOneAndRecordsGitmodules(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	// Build a multi-commit upstream with both default branch main (3 commits)
	// and a non-default branch release (2 commits beyond main) plus an extra
	// non-skill file so sparse checkout is exercised alongside shallow cloning.
	remote := filepath.Join(base, "acme", "design-skills")
	writeSourceFile(t, filepath.Join(remote, "skills", "layout", "SKILL.md"), "---\nname: layout\ndescription: v1\n---\n")
	writeSourceFile(t, filepath.Join(remote, "assets", "heavy.txt"), "non-skill payload\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c1")
	writeSourceFile(t, filepath.Join(remote, "skills", "layout", "SKILL.md"), "---\nname: layout\ndescription: v2\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c2")
	writeSourceFile(t, filepath.Join(remote, "skills", "layout", "SKILL.md"), "---\nname: layout\ndescription: v3\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c3")

	otherRemote := filepath.Join(base, "acme", "branch-skills")
	writeSourceFile(t, filepath.Join(otherRemote, "skills", "core", "SKILL.md"), "---\nname: core\ndescription: dev1\n---\n")
	run(otherRemote, "init", "-q", "-b", "dev")
	run(otherRemote, "add", "-A")
	run(otherRemote, "commit", "-q", "-m", "d1")
	writeSourceFile(t, filepath.Join(otherRemote, "skills", "core", "SKILL.md"), "---\nname: core\ndescription: dev2\n---\n")
	run(otherRemote, "add", "-A")
	run(otherRemote, "commit", "-q", "-m", "d2")
	run(otherRemote, "checkout", "-q", "-b", "release")
	writeSourceFile(t, filepath.Join(otherRemote, "skills", "core", "SKILL.md"), "---\nname: core\ndescription: rel1\n---\n")
	run(otherRemote, "add", "-A")
	run(otherRemote, "commit", "-q", "-m", "r1")
	writeSourceFile(t, filepath.Join(otherRemote, "skills", "core", "SKILL.md"), "---\nname: core\ndescription: rel2\n---\n")
	run(otherRemote, "add", "-A")
	run(otherRemote, "commit", "-q", "-m", "r2")
	run(otherRemote, "checkout", "-q", "dev")

	unmanagedRemote := filepath.Join(base, "external-repo")
	writeSourceFile(t, filepath.Join(unmanagedRemote, "README.md"), "unmanaged\n")
	run(unmanagedRemote, "init", "-q", "-b", "main")
	run(unmanagedRemote, "add", "-A")
	run(unmanagedRemote, "commit", "-q", "-m", "u1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", fileURL(unmanagedRemote), "external/unmanaged")
	run(agentsRoot, "commit", "-q", "-m", "add unmanaged submodule")

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	ctx := context.Background()

	if err := manager.Add(ctx, AddRequest{
		Name: "acme/design-skills", URL: fileURL(remote), Branch: "main",
	}); err != nil {
		t.Fatalf("Add(default branch): %v", err)
	}
	if err := manager.Add(ctx, AddRequest{
		Name: "acme/branch-skills", URL: fileURL(otherRemote), Branch: "release",
	}); err != nil {
		t.Fatalf("Add(non-default branch): %v", err)
	}

	for _, tc := range []struct {
		relPath string
		branch  string
		wantSub string
	}{
		{"resources/skills/vendor/shared/acme/design-skills", "main", "description: v3"},
		{"resources/skills/vendor/shared/acme/branch-skills", "release", "description: rel2"},
	} {
		checkout := filepath.Join(agentsRoot, filepath.FromSlash(tc.relPath))
		if got := gitOutput(t, checkout, "rev-parse", "--is-shallow-repository"); got != "true" {
			t.Fatalf("%s is-shallow-repository = %q, want true", tc.relPath, got)
		}
		if got := gitOutput(t, checkout, "rev-list", "--count", "HEAD"); got != "1" {
			t.Fatalf("%s rev-list --count HEAD = %q, want 1", tc.relPath, got)
		}
		if got := gitOutput(t, agentsRoot, "config", "--file", ".gitmodules", "--get", "submodule."+tc.relPath+".shallow"); got != "true" {
			t.Fatalf(".gitmodules shallow for %s = %q, want true", tc.relPath, got)
		}
		if got := gitOutput(t, agentsRoot, "config", "--file", ".gitmodules", "--get", "submodule."+tc.relPath+".branch"); got != tc.branch {
			t.Fatalf(".gitmodules branch for %s = %q, want %q", tc.relPath, got, tc.branch)
		}
		gitdir, err := submoduleGitdir(checkout)
		if err != nil || gitdir == "" {
			t.Fatalf("%s .git is not an absorbed gitdir pointer: gitdir=%q err=%v", tc.relPath, gitdir, err)
		}
		if !strings.HasSuffix(filepath.ToSlash(gitdir), "/.git/modules/"+tc.relPath) {
			t.Fatalf("%s gitdir = %q, want suffix /.git/modules/%s", tc.relPath, gitdir, tc.relPath)
		}
		// Verify sparse checkout excluded non-skill payload on design-skills.
		if tc.branch == "main" {
			if _, statErr := os.Stat(filepath.Join(checkout, "assets", "heavy.txt")); !os.IsNotExist(statErr) {
				t.Fatalf("sparse checkout materialized non-skill file: %v", statErr)
			}
			skillBody, err := os.ReadFile(filepath.Join(checkout, "skills", "layout", "SKILL.md"))
			if err != nil || !strings.Contains(string(skillBody), tc.wantSub) {
				t.Fatalf("layout SKILL.md = %q err=%v, want %q", skillBody, err, tc.wantSub)
			}
		} else {
			skillBody, err := os.ReadFile(filepath.Join(checkout, "skills", "core", "SKILL.md"))
			if err != nil || !strings.Contains(string(skillBody), tc.wantSub) {
				t.Fatalf("core SKILL.md = %q err=%v, want %q", skillBody, err, tc.wantSub)
			}
		}
	}

	// .gitmodules changes must be staged cleanly with unmanaged entry intact.
	if unstaged := gitOutput(t, agentsRoot, "diff", "--", ".gitmodules"); unstaged != "" {
		t.Fatalf(".gitmodules has unstaged changes after Add:\n%s", unstaged)
	}
	if got := gitOutput(t, agentsRoot, "config", "--file", ".gitmodules", "--get", "submodule.external/unmanaged.path"); got != "external/unmanaged" {
		t.Fatalf("unmanaged .gitmodules entry was disturbed: %q", got)
	}
}

func TestUpdateAdvancesShallowSubmoduleWhileStayingShallow(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "acme", "shallow-up")
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v1\n---\n")
	writeSourceFile(t, filepath.Join(remote, "extra", "ignored.txt"), "not a skill\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c1")
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v2\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c2")
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v3\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c3")

	fullRemote := filepath.Join(base, "acme", "full-up")
	writeSourceFile(t, filepath.Join(fullRemote, "skills", "legacy", "SKILL.md"), "---\nname: legacy\ndescription: f1\n---\n")
	run(fullRemote, "init", "-q", "-b", "main")
	run(fullRemote, "add", "-A")
	run(fullRemote, "commit", "-q", "-m", "f1")
	writeSourceFile(t, filepath.Join(fullRemote, "skills", "legacy", "SKILL.md"), "---\nname: legacy\ndescription: f2\n---\n")
	run(fullRemote, "add", "-A")
	run(fullRemote, "commit", "-q", "-m", "f2")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	ctx := context.Background()
	if err := manager.Add(ctx, AddRequest{
		Name: "acme/shallow-up", URL: fileURL(remote), Branch: "main",
		DiscoveryPriority: []catalog.DiscoveryStrategy{catalog.DiscoverySkillsDir},
	}); err != nil {
		t.Fatalf("Add(shallow): %v", err)
	}
	// Existing non-shallow submodule added via plain git submodule add.
	fullRel := "resources/skills/vendor/shared/acme/full-up"
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", fileURL(fullRemote), fullRel)
	run(agentsRoot, "commit", "-q", "-m", "add shallow and full submodules")

	shallowTarget := filepath.Join(sourcesRoot, "vendor", "shared", "acme", "shallow-up")
	fullTarget := filepath.Join(agentsRoot, filepath.FromSlash(fullRel))

	// Advance shallow upstream by 2 commits, adding a second skill.
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v4\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c4")
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v5\n---\n")
	writeSourceFile(t, filepath.Join(remote, "skills", "beta", "SKILL.md"), "---\nname: beta\ndescription: v5\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c5")

	// Advance full upstream by 1 commit.
	writeSourceFile(t, filepath.Join(fullRemote, "skills", "legacy", "SKILL.md"), "---\nname: legacy\ndescription: f3\n---\n")
	run(fullRemote, "add", "-A")
	run(fullRemote, "commit", "-q", "-m", "f3")

	shallowSource := catalog.Source{
		ID: "vendor-shared/acme/shallow-up", Kind: catalog.SourceVendor, Scope: "shared",
		Path: shallowTarget, Branch: "main",
		DiscoveryPriority: []catalog.DiscoveryStrategy{catalog.DiscoverySkillsDir},
	}
	fullSource := catalog.Source{
		ID: "vendor-shared/acme/full-up", Kind: catalog.SourceVendor, Scope: "shared",
		Path: fullTarget, Branch: "main",
	}

	results, err := manager.Update(ctx, []catalog.Source{shallowSource, fullSource}, false)
	if err != nil {
		t.Fatalf("first Update(): %v", err)
	}
	if len(results) != 2 || !results[0].Changed || !results[1].Changed {
		t.Fatalf("first Update() results = %#v, want both changed", results)
	}
	if got := gitOutput(t, shallowTarget, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("shallow submodule after update is-shallow = %q, want true", got)
	}
	if got := gitOutput(t, shallowTarget, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("shallow submodule after update commit count = %q, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(shallowTarget, "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("new upstream skill missing after shallow sparse update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shallowTarget, "extra", "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("sparse checkout leaked non-skill file after shallow update: %v", err)
	}
	// Non-shallow existing submodule keeps its full history.
	if got := gitOutput(t, fullTarget, "rev-parse", "--is-shallow-repository"); got != "false" {
		t.Fatalf("existing non-shallow submodule became shallow: %q", got)
	}
	if got := gitOutput(t, fullTarget, "rev-list", "--count", "HEAD"); got != "3" {
		t.Fatalf("existing non-shallow submodule commit count = %q, want 3", got)
	}

	// Second update without committing the parent gitlink, after upstream rewrites
	// history and prunes the old gitlink commit (c3): Update must still advance
	// cleanly instead of failing on the stale parent gitlink.
	run(remote, "checkout", "-q", "--orphan", "rewritten")
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v6-rewritten\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c6-rewritten")
	run(remote, "branch", "-M", "rewritten", "main")
	run(remote, "reflog", "expire", "--expire=now", "--all")
	run(remote, "gc", "-q", "--prune=now")
	run(shallowTarget, "reflog", "expire", "--expire=now", "--all")
	run(shallowTarget, "gc", "-q", "--prune=now")

	results, err = manager.Update(ctx, []catalog.Source{shallowSource}, false)
	if err != nil {
		t.Fatalf("second Update() after upstream rewrite: %v", err)
	}
	if len(results) != 1 || !results[0].Changed {
		t.Fatalf("second Update() results = %#v, want changed", results)
	}
	if got := gitOutput(t, shallowTarget, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("shallow submodule after rewrite update is-shallow = %q, want true", got)
	}
	if got := gitOutput(t, shallowTarget, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("shallow submodule after rewrite update commit count = %q, want 1", got)
	}
	alphaBody, err := os.ReadFile(filepath.Join(shallowTarget, "skills", "alpha", "SKILL.md"))
	if err != nil || !strings.Contains(string(alphaBody), "v6-rewritten") {
		t.Fatalf("alpha SKILL.md = %q err=%v, want v6-rewritten", alphaBody, err)
	}

	// Fresh clone of the parent repository without initialized submodules:
	// source update must initialize the missing checkout shallowly (honoring
	// .gitmodules shallow=true) and advance it to v7-post-init while staying shallow.
	run(agentsRoot, "add", "resources/skills/vendor/shared/acme/shallow-up")
	run(agentsRoot, "commit", "-q", "-m", "record updated shallow gitlink")
	freshParent := filepath.Join(base, "agents-fresh-clone")
	run(base, "clone", "-q", fileURL(agentsRoot), freshParent)
	freshSources := filepath.Join(freshParent, "resources", "skills")
	freshShallowTarget := filepath.Join(freshSources, "vendor", "shared", "acme", "shallow-up")
	// Advance remote one more commit beyond the fresh clone's gitlink so Update both
	// initializes the missing checkout and advances it to the new tip.
	writeSourceFile(t, filepath.Join(remote, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: v7-post-init\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c7")
	freshManager := Manager{RepositoryRoot: freshParent, SkillsRoot: freshSources, Git: GitCommander{}}
	freshSource := catalog.Source{
		ID: "vendor-shared/acme/shallow-up", Kind: catalog.SourceVendor, Scope: "shared",
		Path: freshShallowTarget, Branch: "main", Availability: catalog.SourceCheckoutMissing,
		DiscoveryPriority: []catalog.DiscoveryStrategy{catalog.DiscoverySkillsDir},
	}
	results, err = freshManager.Update(ctx, []catalog.Source{freshSource}, false)
	if err != nil {
		t.Fatalf("Update(missing shallow checkout): %v", err)
	}
	if len(results) != 1 || !results[0].Changed {
		t.Fatalf("Update(missing shallow checkout) results = %#v, want changed", results)
	}
	if got := gitOutput(t, freshShallowTarget, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("initialized missing shallow checkout is-shallow = %q, want true", got)
	}
	if got := gitOutput(t, freshShallowTarget, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("initialized missing shallow checkout commit count = %q, want 1", got)
	}
	alphaBody, err = os.ReadFile(filepath.Join(freshShallowTarget, "skills", "alpha", "SKILL.md"))
	if err != nil || !strings.Contains(string(alphaBody), "v7-post-init") {
		t.Fatalf("fresh alpha SKILL.md = %q err=%v, want v7-post-init", alphaBody, err)
	}
}

func TestAddShallowRollbackCleansGitmodulesAndModulesDir(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "acme", "repo")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: v1\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c1")
	writeSourceFile(t, filepath.Join(remote, "skills", "tool", "SKILL.md"), "---\nname: tool\ndescription: v2\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c2")

	// Case 1: repo with no prior .gitmodules; discovery fails after shallow clone.
	agentsEmpty := filepath.Join(base, "agents-empty")
	sourcesEmpty := filepath.Join(agentsEmpty, "resources", "skills")
	if err := os.MkdirAll(sourcesEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsEmpty, "init", "-q", "-b", "main")
	run(agentsEmpty, "commit", "-q", "--allow-empty", "-m", "init")
	managerEmpty := Manager{RepositoryRoot: agentsEmpty, SkillsRoot: sourcesEmpty, Git: GitCommander{}}
	rel := "resources/skills/vendor/shared/acme/repo"
	if err := managerEmpty.Add(context.Background(), AddRequest{
		Name: "acme/repo", URL: fileURL(remote), Branch: "main", SkillPaths: []string{"missing"},
	}); err == nil {
		t.Fatal("Add() with missing skill path unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(agentsEmpty, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf("rollback left newly-created empty .gitmodules behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsEmpty, ".git", "modules", filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("rollback left .git/modules gitdir behind: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(sourcesEmpty, "vendor", "shared", "acme")); !os.IsNotExist(err) {
		t.Fatalf("rollback left empty owner directory behind: %v", err)
	}
	if status := gitOutput(t, agentsEmpty, "status", "--porcelain"); status != "" {
		t.Fatalf("rollback left parent repo dirty:\n%s", status)
	}

	// Case 2: repo with a pre-existing unmanaged .gitmodules entry; rollback must
	// drop the failed shallow entry while preserving the unmanaged one.
	unmanaged := filepath.Join(base, "unmanaged")
	writeSourceFile(t, filepath.Join(unmanaged, "README.md"), "keep\n")
	run(unmanaged, "init", "-q", "-b", "main")
	run(unmanaged, "add", "-A")
	run(unmanaged, "commit", "-q", "-m", "u1")

	agentsManaged := filepath.Join(base, "agents-managed")
	sourcesManaged := filepath.Join(agentsManaged, "resources", "skills")
	if err := os.MkdirAll(sourcesManaged, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsManaged, "init", "-q", "-b", "main")
	run(agentsManaged, "commit", "-q", "--allow-empty", "-m", "init")
	run(agentsManaged, "submodule", "add", "-q", "-b", "main", fileURL(unmanaged), "external/keep")
	run(agentsManaged, "commit", "-q", "-m", "add unmanaged")

	managerManaged := Manager{RepositoryRoot: agentsManaged, SkillsRoot: sourcesManaged, Git: GitCommander{}}
	if err := managerManaged.Add(context.Background(), AddRequest{
		Name: "acme/repo", URL: fileURL(remote), Branch: "main", SkillPaths: []string{"missing"},
	}); err == nil {
		t.Fatal("Add() with missing skill path unexpectedly succeeded")
	}
	modulesBytes, err := os.ReadFile(filepath.Join(agentsManaged, ".gitmodules"))
	if err != nil {
		t.Fatalf("read .gitmodules after rollback: %v", err)
	}
	modulesText := string(modulesBytes)
	if !strings.Contains(modulesText, "external/keep") {
		t.Fatalf("rollback dropped unmanaged .gitmodules entry:\n%s", modulesText)
	}
	if strings.Contains(modulesText, "acme/repo") || strings.Contains(modulesText, "shallow = true") {
		t.Fatalf("rollback left failed shallow entry in .gitmodules:\n%s", modulesText)
	}
	if _, err := os.Stat(filepath.Join(agentsManaged, ".git", "modules", filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("rollback left .git/modules gitdir behind: %v", err)
	}

	// Re-adding cleanly succeeds and produces a depth-1 shallow submodule.
	if err := managerManaged.Add(context.Background(), AddRequest{
		Name: "acme/repo", URL: fileURL(remote), Branch: "main",
	}); err != nil {
		t.Fatalf("re-add after rollback failed: %v", err)
	}
	checkout := filepath.Join(agentsManaged, filepath.FromSlash(rel))
	if got := gitOutput(t, checkout, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("re-added submodule is-shallow = %q, want true", got)
	}
	if got := gitOutput(t, checkout, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("re-added submodule commit count = %q, want 1", got)
	}

	// Case 3: git submodule add fails after clone (path ignored by .gitignore);
	// rollback must remove the unabsorbed cloned worktree and empty owner dir.
	agentsIgnored := filepath.Join(base, "agents-ignored")
	sourcesIgnored := filepath.Join(agentsIgnored, "resources", "skills")
	if err := os.MkdirAll(sourcesIgnored, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceFile(t, filepath.Join(agentsIgnored, ".gitignore"), "resources/skills/\n")
	run(agentsIgnored, "init", "-q", "-b", "main")
	run(agentsIgnored, "add", ".gitignore")
	run(agentsIgnored, "commit", "-q", "-m", "ignore skills")
	managerIgnored := Manager{RepositoryRoot: agentsIgnored, SkillsRoot: sourcesIgnored, Git: GitCommander{}}
	if err := managerIgnored.Add(context.Background(), AddRequest{
		Name: "acme/repo", URL: fileURL(remote), Branch: "main",
	}); err == nil {
		t.Fatal("Add() into gitignored path unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(sourcesIgnored, "vendor", "shared", "acme")); !os.IsNotExist(err) {
		t.Fatalf("rollback left partial clone/owner dir after submodule add failure: %v", err)
	}

	// Case 4: staging .gitmodules fails after shallow=true was written; rollback
	// must unset the unstaged shallow entry so `git rm -f` succeeds in real git.
	agentsUnstaged := filepath.Join(base, "agents-unstaged")
	sourcesUnstaged := filepath.Join(agentsUnstaged, "resources", "skills")
	if err := os.MkdirAll(sourcesUnstaged, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsUnstaged, "init", "-q", "-b", "main")
	run(agentsUnstaged, "commit", "-q", "--allow-empty", "-m", "init")
	failStageGit := scriptedGit{refuse: func(arguments []string) bool {
		return len(arguments) == 3 && arguments[0] == "add" && arguments[1] == "--" && arguments[2] == ".gitmodules"
	}}
	managerUnstaged := Manager{RepositoryRoot: agentsUnstaged, SkillsRoot: sourcesUnstaged, Git: failStageGit}
	if err := managerUnstaged.Add(context.Background(), AddRequest{
		Name: "acme/repo", URL: fileURL(remote), Branch: "main",
	}); err == nil {
		t.Fatal("Add() succeeded despite refused .gitmodules staging")
	}
	if _, err := os.Lstat(filepath.Join(agentsUnstaged, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf("rollback after unstaged .gitmodules left .gitmodules behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsUnstaged, ".git", "modules", filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("rollback after unstaged .gitmodules left .git/modules behind: %v", err)
	}
	if status := gitOutput(t, agentsUnstaged, "status", "--porcelain"); status != "" {
		t.Fatalf("rollback after unstaged .gitmodules left repo dirty:\n%s", status)
	}
}

func TestShallowSubmoduleSupportsMigrateAndRemove(t *testing.T) {
	run := gitRunner(t)
	base := t.TempDir()

	remote := filepath.Join(base, "acme", "tools")
	writeSourceFile(t, filepath.Join(remote, "skills", "one", "SKILL.md"), "---\nname: one\ndescription: v1\n---\n")
	run(remote, "init", "-q", "-b", "main")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c1")
	writeSourceFile(t, filepath.Join(remote, "skills", "one", "SKILL.md"), "---\nname: one\ndescription: v2\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c2")

	unmanaged := filepath.Join(base, "external-keep")
	writeSourceFile(t, filepath.Join(unmanaged, "README.md"), "keep\n")
	run(unmanaged, "init", "-q", "-b", "main")
	run(unmanaged, "add", "-A")
	run(unmanaged, "commit", "-q", "-m", "u1")

	agentsRoot := filepath.Join(base, "agents")
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(agentsRoot, "init", "-q", "-b", "main")
	run(agentsRoot, "commit", "-q", "--allow-empty", "-m", "init")
	run(agentsRoot, "submodule", "add", "-q", "-b", "main", fileURL(unmanaged), "external/keep")
	run(agentsRoot, "commit", "-q", "-m", "add unmanaged")

	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}}
	ctx := context.Background()
	// Add under legacy bare name so source migrate moves it under acme/tools.
	if err := manager.Add(ctx, AddRequest{Name: "tools", URL: fileURL(remote), Branch: "main"}); err != nil {
		t.Fatalf("Add(bare shallow): %v", err)
	}

	loaded, err := catalog.Load(sourcesRoot, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := Lifecycle{
		Manager:     Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: GitCommander{}, Clients: loaded.Clients},
		ProjectRoot: t.TempDir(),
	}
	outcome, err := lifecycle.Migrate(ctx, loaded.Sources, false)
	if err != nil {
		t.Fatalf("Migrate(shallow): %v", err)
	}
	if len(outcome.Migrations) != 1 || outcome.Migrations[0].Status != MigrationMoved {
		t.Fatalf("migrations = %#v, want moved", outcome.Migrations)
	}
	moved := outcome.Migrations[0].TargetPath
	if got := gitOutput(t, moved, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("migrated shallow submodule is-shallow = %q, want true", got)
	}
	if got := gitOutput(t, moved, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("migrated shallow submodule commit count = %q, want 1", got)
	}
	modulesBytes, err := os.ReadFile(filepath.Join(agentsRoot, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modulesBytes), "shallow = true") || !strings.Contains(string(modulesBytes), "vendor/shared/acme/tools") {
		t.Fatalf(".gitmodules after migrate missing shallow/path:\n%s", modulesBytes)
	}

	// Update after migrate still advances and stays shallow.
	writeSourceFile(t, filepath.Join(remote, "skills", "one", "SKILL.md"), "---\nname: one\ndescription: v3\n---\n")
	run(remote, "add", "-A")
	run(remote, "commit", "-q", "-m", "c3")
	migratedSource, ok := outcome.Catalog.Source("vendor-shared/acme/tools")
	if !ok {
		t.Fatal("migrated source missing from reloaded catalog")
	}
	results, err := manager.Update(ctx, []catalog.Source{migratedSource}, false)
	if err != nil {
		t.Fatalf("Update(migrated shallow): %v", err)
	}
	if len(results) != 1 || !results[0].Changed {
		t.Fatalf("Update() results = %#v, want changed", results)
	}
	if got := gitOutput(t, moved, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("updated migrated shallow submodule commit count = %q, want 1", got)
	}

	// Remove cleans up the shallow submodule while preserving the unmanaged entry.
	if err := manager.Remove(ctx, migratedSource); err != nil {
		t.Fatalf("Remove(shallow): %v", err)
	}
	if _, err := os.Lstat(moved); !os.IsNotExist(err) {
		t.Fatalf("removed shallow checkout survived: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(moved)); !os.IsNotExist(err) {
		t.Fatalf("empty owner directory survived removal: %v", err)
	}
	modulesBytes, err = os.ReadFile(filepath.Join(agentsRoot, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modulesBytes), "external/keep") {
		t.Fatalf("Remove disturbed unmanaged .gitmodules entry:\n%s", modulesBytes)
	}
	if strings.Contains(string(modulesBytes), "acme/tools") || strings.Contains(string(modulesBytes), "shallow = true") {
		t.Fatalf("Remove left shallow submodule entry in .gitmodules:\n%s", modulesBytes)
	}
}
