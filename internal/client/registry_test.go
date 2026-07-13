package client

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegistryExtendsBuiltinsWithConfiguredClient(t *testing.T) {
	registry, err := NewRegistry(map[ID]string{
		"pi": ".pi/skills",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := []ID{Codex, Claude, Gemini, "pi"}
	if got := registry.IDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("IDs() = %v, want %v", got, wantIDs)
	}
	got, err := registry.TargetDir("/tmp/project", "pi")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/project", ".pi", "skills"); got != want {
		t.Fatalf("TargetDir() = %q, want %q", got, want)
	}
}

func TestRegistryRejectsProjectEscapingPath(t *testing.T) {
	if _, err := NewRegistry(map[ID]string{"pi": "../shared/skills"}); err == nil {
		t.Fatal("NewRegistry() accepted a path outside the project")
	}
}
