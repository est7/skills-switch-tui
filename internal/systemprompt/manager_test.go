package systemprompt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestDiscoverAndProjectClaudePromptTree(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writePrompt(t, filepath.Join(root, "claude-prompt", "CLAUDE.md"))
	writePrompt(t, filepath.Join(root, "claude-prompt", "rules", "core.md"))
	writePrompt(t, filepath.Join(root, "claude-prompt", "notes.txt"))
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	group, ok := loaded.Group("claude-prompt")
	if !ok || len(group.Files) != 2 {
		t.Fatalf("unexpected group: %#v, %v", group, ok)
	}
	manager := NewManager(userHome, client.DefaultRegistry())

	if err := manager.SetEnabled([]Group{group}, true); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"CLAUDE.md", filepath.Join("rules", "core.md")} {
		path := filepath.Join(userHome, ".claude", relative)
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("missing projected prompt %s: %v, %v", path, info, err)
		}
	}
	if state, err := manager.State(group); err != nil || state != StateEnabled {
		t.Fatalf("state = %q, %v", state, err)
	}
	if err := manager.SetEnabled([]Group{group}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".claude", "CLAUDE.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CLAUDE.md still exists: %v", err)
	}
}

func TestBuildAndProjectCodexPromptDetectsAndRepairsStaleOutput(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	base := filepath.Join(root, "codex-prompt", "AGENTS.md")
	firstRule := filepath.Join(root, "codex-prompt", "rules", "10-core.md")
	secondRule := filepath.Join(root, "codex-prompt", "rules", "20-testing.md")
	writePromptContents(t, base, "# Codex Base\n")
	writePromptContents(t, secondRule, "## Testing\n")
	writePromptContents(t, firstRule, "## Core\n")
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	group, ok := loaded.Group("codex-prompt")
	if !ok || group.Mode != client.PromptConcat || group.EntryFile != "AGENTS.md" {
		t.Fatalf("unexpected Codex group: %#v", group)
	}
	manager := NewManager(userHome, client.DefaultRegistry())
	if state, err := manager.State(group); err != nil || state != StateDisabled {
		t.Fatalf("initial state = %q, %v", state, err)
	}

	build, err := manager.Build(group)
	if err != nil {
		t.Fatal(err)
	}
	if !build.Changed || build.Path != manager.GeneratedPath(group) || build.Bytes == 0 {
		t.Fatalf("unexpected build result: %#v", build)
	}
	contents, err := os.ReadFile(build.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	baseIndex := strings.Index(text, "BEGIN: AGENTS.md")
	coreIndex := strings.Index(text, "BEGIN: rules/10-core.md")
	testingIndex := strings.Index(text, "BEGIN: rules/20-testing.md")
	if baseIndex < 0 || coreIndex <= baseIndex || testingIndex <= coreIndex {
		t.Fatalf("compiled prompt order is not deterministic:\n%s", text)
	}
	if second, err := manager.Build(group); err != nil || second.Changed {
		t.Fatalf("idempotent build = %#v, %v", second, err)
	}

	if err := manager.SetEnabled([]Group{group}, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(userHome, ".codex", "AGENTS.md")
	if link, err := os.Readlink(target); err != nil || link != build.Path {
		t.Fatalf("Codex prompt link = %q, %v, want %q", link, err, build.Path)
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".codex", "rules")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex source rules were projected directly: %v", err)
	}
	writePromptContents(t, firstRule, "## Core changed\n")
	if state, err := manager.State(group); err != nil || state != StateStale {
		t.Fatalf("state after source edit = %q, %v", state, err)
	}
	if _, err := manager.Build(group); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(group); err != nil || state != StateEnabled {
		t.Fatalf("state after rebuild = %q, %v", state, err)
	}
	if err := manager.SetEnabled([]Group{group}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Codex AGENTS.md still exists: %v", err)
	}
}

func TestDisableConcatPromptWithMissingGeneratedFileRemovesManagedLink(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writePromptContents(t, filepath.Join(root, "codex-prompt", "AGENTS.md"), "# Base\n")
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	group, _ := loaded.Group("codex-prompt")
	manager := NewManager(userHome, client.DefaultRegistry())
	if err := manager.SetEnabled([]Group{group}, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manager.GeneratedPath(group)); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(group); err != nil || state != StateBroken {
		t.Fatalf("state with missing output = %q, %v", state, err)
	}
	if err := manager.SetEnabled([]Group{group}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".codex", "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("broken managed link was not removed: %v", err)
	}
}

func TestEnableConcatPromptMigratesLegacyDirectEntryLink(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	base := filepath.Join(root, "codex-prompt", "AGENTS.md")
	writePromptContents(t, base, "# Base\n")
	writePromptContents(t, filepath.Join(root, "codex-prompt", "rules", "10-core.md"), "## Core\n")
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	group, _ := loaded.Group("codex-prompt")
	manager := NewManager(userHome, client.DefaultRegistry())
	target := filepath.Join(userHome, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, target); err != nil {
		t.Fatal(err)
	}
	if state, err := manager.State(group); err != nil || state != StateStale {
		t.Fatalf("legacy direct link state = %q, %v", state, err)
	}

	if err := manager.SetEnabled([]Group{group}, true); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(target); err != nil || link != manager.GeneratedPath(group) {
		t.Fatalf("migrated prompt link = %q, %v", link, err)
	}
	if state, err := manager.State(group); err != nil || state != StateEnabled {
		t.Fatalf("migrated prompt state = %q, %v", state, err)
	}

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, target); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEnabled([]Group{group}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy prompt link was not disabled: %v", err)
	}
}

func TestBuildRejectsTreePrompt(t *testing.T) {
	root := t.TempDir()
	writePrompt(t, filepath.Join(root, "claude-prompt", "CLAUDE.md"))
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	group, _ := loaded.Group("claude-prompt")
	if _, err := NewManager(t.TempDir(), client.DefaultRegistry()).Build(group); err == nil {
		t.Fatal("tree prompt build unexpectedly succeeded")
	}
}

func TestPromptGroupConflictPreventsPartialProjection(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writePrompt(t, filepath.Join(root, "claude-prompt", "CLAUDE.md"))
	writePrompt(t, filepath.Join(root, "claude-prompt", "rules", "core.md"))
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(userHome, ".claude", "rules", "core.md")
	writePrompt(t, conflict)
	manager := NewManager(userHome, client.DefaultRegistry())

	if err := manager.SetEnabled(loaded.Groups, true); err == nil {
		t.Fatal("prompt conflict unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".claude", "CLAUDE.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("group conflict partially projected CLAUDE.md: %v", err)
	}
	if info, err := os.Lstat(conflict); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("unmanaged conflict was changed: %v, %v", info, err)
	}
}

func TestConfiguredFutureClientPromptAdapter(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writePrompt(t, filepath.Join(root, "pi-prompt", "AGENTS.md"))
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {UserPromptDir: ".pi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Discover(root, registry)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(userHome, registry)
	if err := manager.SetEnabled(loaded.Groups, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(userHome, ".pi", "AGENTS.md")); err != nil {
		t.Fatalf("future client prompt was not projected: %v", err)
	}
}

func TestDisablePreservesTargetReplacedAfterPreflight(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writePrompt(t, filepath.Join(root, "claude-prompt", "CLAUDE.md"))
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(userHome, client.DefaultRegistry())
	if err := manager.SetEnabled(loaded.Groups, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(userHome, ".claude", "CLAUDE.md")
	manager.beforeApply = func(change change) {
		if change.action != removeLink {
			return
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("user-owned\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err = manager.SetEnabled(loaded.Groups, false)
	if err == nil {
		t.Fatal("disable succeeded after the managed link was replaced")
	}
	contents, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("replacement file was removed: %v", readErr)
	}
	if got := string(contents); !strings.Contains(got, "user-owned") {
		t.Fatalf("replacement contents = %q", got)
	}
}

func TestRollbackPreservesTargetsReplacedDuringApply(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writePrompt(t, filepath.Join(root, "claude-prompt", "A.md"))
	writePrompt(t, filepath.Join(root, "claude-prompt", "B.md"))
	loaded, err := Discover(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	firstTarget := filepath.Join(userHome, ".claude", "A.md")
	secondTarget := filepath.Join(userHome, ".claude", "B.md")
	manager := NewManager(userHome, client.DefaultRegistry())
	manager.beforeApply = func(next change) {
		if next.path != secondTarget {
			return
		}
		if err := os.Remove(firstTarget); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(firstTarget, []byte("first user file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(secondTarget, []byte("second user file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err = manager.SetEnabled(loaded.Groups, true)
	if err == nil {
		t.Fatal("enable succeeded after targets changed during apply")
	}
	for path, want := range map[string]string{
		firstTarget:  "first user file",
		secondTarget: "second user file",
	} {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("replacement file %s was removed: %v", path, readErr)
		}
		if !strings.Contains(string(contents), want) {
			t.Fatalf("replacement contents at %s = %q", path, contents)
		}
	}
}

func writePrompt(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePromptContents(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
