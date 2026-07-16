package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
)

func TestUpdateHardResetsTrackedEditsBeforeUpdatingSubmodule(t *testing.T) {
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
