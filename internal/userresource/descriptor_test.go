package userresource

import (
	"path/filepath"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestDescriptorsAreCompleteAndUnique(t *testing.T) {
	want := map[Kind]struct{}{
		KindCommand: {}, KindHook: {}, KindAgent: {}, KindOutputStyle: {},
	}
	seenDirectories := make(map[string]Kind)
	seenCommands := make(map[string]Kind)
	for _, descriptor := range Descriptors() {
		if _, ok := want[descriptor.Kind]; !ok {
			t.Fatalf("unexpected or duplicate descriptor kind %q", descriptor.Kind)
		}
		delete(want, descriptor.Kind)
		if previous, exists := seenDirectories[descriptor.Directory]; exists {
			t.Fatalf("directory %q is shared by %s and %s", descriptor.Directory, previous, descriptor.Kind)
		}
		seenDirectories[descriptor.Directory] = descriptor.Kind
		if previous, exists := seenCommands[descriptor.Command]; exists {
			t.Fatalf("command %q is shared by %s and %s", descriptor.Command, previous, descriptor.Kind)
		}
		seenCommands[descriptor.Command] = descriptor.Kind
		if descriptor.Capability == "" || descriptor.TargetScope == "" || descriptor.BootstrapScope == "" {
			t.Fatalf("descriptor %s is incomplete: %+v", descriptor.Kind, descriptor)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing descriptors: %v", want)
	}
}

func TestDescriptorTargetScopeAndResolverAgree(t *testing.T) {
	registry := client.DefaultRegistry()
	base := t.TempDir()
	for _, descriptor := range Descriptors() {
		clientIDs := registry.IDsFor(descriptor.Capability)
		if len(clientIDs) == 0 {
			t.Fatalf("descriptor %s capability %s has no clients", descriptor.Kind, descriptor.Capability)
		}
		target, err := descriptor.TargetDir(registry, base, clientIDs[0])
		if err != nil {
			t.Fatalf("resolve %s target: %v", descriptor.Kind, err)
		}
		if !filepath.IsAbs(target) || filepath.Clean(target) == filepath.Clean(base) {
			t.Fatalf("descriptor %s target = %q", descriptor.Kind, target)
		}
	}
}
