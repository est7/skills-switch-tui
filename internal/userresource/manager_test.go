package userresource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestDiscoverScopedCommandsAndProjectSharedAndClientOnlyFiles(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writeResource(t, filepath.Join(root, "shared", "remember.md"))
	writeResource(t, filepath.Join(root, "gemini-only", "android", "generate.toml"))

	catalog, err := Discover(root, KindCommand, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	shared, ok := catalog.Resource("shared/remember.md")
	if !ok {
		t.Fatal("shared command was not discovered")
	}
	gemini, ok := catalog.Resource("gemini-only/android/generate.toml")
	if !ok {
		t.Fatal("Gemini-only command was not discovered")
	}
	manager := NewManager(userHome, client.DefaultRegistry())
	if err := manager.Apply([]Operation{
		{Resource: shared, Client: client.Claude, Enabled: true},
		{Resource: shared, Client: client.Codex, Enabled: true},
		{Resource: gemini, Client: client.Gemini, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(userHome, ".claude", "commands", "remember.md"),
		filepath.Join(userHome, ".codex", "prompts", "remember.md"),
		filepath.Join(userHome, ".gemini", "commands", "android", "generate.toml"),
	} {
		if _, err := os.Readlink(path); err != nil {
			t.Fatalf("missing command projection %s: %v", path, err)
		}
	}
	if state, err := manager.State(gemini, client.Claude); err != nil || state != StateIncompatible {
		t.Fatalf("Gemini command state for Claude = %q, %v", state, err)
	}
}

func TestHookConflictPreventsPartialProjection(t *testing.T) {
	root := t.TempDir()
	userHome := t.TempDir()
	writeResource(t, filepath.Join(root, "shared", "first.sh"))
	writeResource(t, filepath.Join(root, "shared", "second.sh"))
	catalog, err := Discover(root, KindHook, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(userHome, ".claude", "hooks", "second.sh")
	writeResource(t, conflict)
	operations := make([]Operation, 0, len(catalog.Resources))
	for _, resource := range catalog.Resources {
		operations = append(operations, Operation{Resource: resource, Client: client.Claude, Enabled: true})
	}

	err = NewManager(userHome, client.DefaultRegistry()).Apply(operations)
	if err == nil {
		t.Fatal("hook conflict unexpectedly succeeded")
	}
	if _, err := os.Lstat(filepath.Join(userHome, ".claude", "hooks", "first.sh")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hook conflict left a partial projection: %v", err)
	}
}

func TestDiscoverRejectsUnknownClientScope(t *testing.T) {
	root := t.TempDir()
	writeResource(t, filepath.Join(root, "unknown-only", "hook.sh"))
	if _, err := Discover(root, KindHook, client.DefaultRegistry()); err == nil {
		t.Fatal("unknown hook scope was accepted")
	}
}

func TestDiscoverRejectsBareClientScope(t *testing.T) {
	root := t.TempDir()
	writeResource(t, filepath.Join(root, "claude", "hook.sh"))
	if _, err := Discover(root, KindHook, client.DefaultRegistry()); err == nil {
		t.Fatal("bare client scope was accepted")
	}
}

func writeResource(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
