package linktransaction

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteAndRestoreAllActions(t *testing.T) {
	root := t.TempDir()
	createPath := filepath.Join(root, "created", "link")
	removePath := filepath.Join(root, "removed")
	replacePath := filepath.Join(root, "replaced")
	oldRemove := filepath.Join(root, "old-remove")
	oldReplace := filepath.Join(root, "old-replace")
	newCreate := filepath.Join(root, "new-create")
	newReplace := filepath.Join(root, "new-replace")
	if err := os.Symlink(oldRemove, removePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldReplace, replacePath); err != nil {
		t.Fatal(err)
	}

	applied, err := (Engine{Label: "test"}).Execute([]Change{
		Create(createPath, newCreate),
		Remove(removePath, oldRemove),
		Replace(replacePath, oldReplace, newReplace),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTarget(t, createPath, newCreate)
	assertMissing(t, removePath)
	assertTarget(t, replacePath, newReplace)

	if err := applied.Restore(); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, createPath)
	assertMissing(t, filepath.Dir(createPath))
	assertTarget(t, removePath, oldRemove)
	assertTarget(t, replacePath, oldReplace)
}

func TestRollbackPreservesConcurrentlyChangedTarget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	engine := Engine{
		Label: "test",
		beforeApply: func(next Change) {
			if next.path != second {
				return
			}
			if err := os.Remove(first); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(first, []byte("user-owned\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(second, []byte("blocks transaction\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	_, err := engine.Execute([]Change{
		Create(first, filepath.Join(root, "source-one")),
		Create(second, filepath.Join(root, "source-two")),
	})
	if err == nil || !strings.Contains(err.Error(), "preserve concurrently changed target") {
		t.Fatalf("Execute error = %v, want concurrent-change rollback error", err)
	}
	contents, readErr := os.ReadFile(first)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(contents) != "user-owned\n" {
		t.Fatalf("first contents = %q", contents)
	}
}

func TestRemovePreservesTargetChangedBeforeApply(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "managed")
	expected := filepath.Join(root, "source")
	if err := os.Symlink(expected, path); err != nil {
		t.Fatal(err)
	}
	engine := Engine{
		Label: "test",
		beforeApply: func(next Change) {
			if next.path != path {
				return
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("user-owned\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}

	if _, err := engine.Execute([]Change{Remove(path, expected)}); err == nil {
		t.Fatal("Remove succeeded after target changed")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "user-owned\n" {
		t.Fatalf("replacement contents = %q", contents)
	}
}

func TestEquivalentTargetMatchesRelativeLink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "links", "item")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "sources", "item")
	relative, err := filepath.Rel(filepath.Dir(path), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, path); err != nil {
		t.Fatal(err)
	}
	_, err = (Engine{MatchTarget: EquivalentTarget}).Execute([]Change{Remove(path, relative)})
	if err != nil {
		t.Fatal(err)
	}
	assertMissing(t, path)
}

func TestExecuteRejectsChangeNotBuiltByConstructor(t *testing.T) {
	_, err := (Engine{Label: "test"}).Execute([]Change{{}})
	if err == nil || !strings.Contains(err.Error(), "must be created with Create, Remove, or Replace") {
		t.Fatalf("Execute error = %v, want invalid Change error", err)
	}
}

func TestExecuteRejectsIncompleteConstructedChanges(t *testing.T) {
	tests := []struct {
		name   string
		change Change
	}{
		{name: "create path", change: Create("", "/source")},
		{name: "create target", change: Create("/link", "")},
		{name: "remove expected target", change: Remove("/link", "")},
		{name: "replace expected target", change: Replace("/link", "", "/new")},
		{name: "replace new target", change: Replace("/link", "/old", "")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := (Engine{Label: "test"}).Execute([]Change{test.change}); err == nil {
				t.Fatalf("Execute(%s) succeeded", test.name)
			}
		})
	}
}

func assertTarget(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("target %s = %q, want %q", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat(%s) error = %v, want not exist", path, err)
	}
}
