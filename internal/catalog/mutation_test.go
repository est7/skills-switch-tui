package catalog

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

func TestConcurrentSourceRegistrationsDoNotLoseUpdates(t *testing.T) {
	root := t.TempDir()
	ids := []string{"vendor-shared/first", "vendor-shared/second"}
	errorsByIndex := make([]error, len(ids))
	var wait sync.WaitGroup
	for index, id := range ids {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsByIndex[index] = RegisterSource(root, id, SourcePolicy{Branch: "main"})
		}()
	}
	wait.Wait()
	for index, err := range errorsByIndex {
		if err != nil {
			t.Fatalf("RegisterSource(%s): %v", ids[index], err)
		}
	}
	config, err := loadConfig(filepath.Join(root, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, ok := config.Sources[id]; !ok {
			t.Fatalf("concurrent registration lost %s: %#v", id, config.Sources)
		}
	}
}

func TestSourceMutationPreservesCatalogPermissions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "catalog.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RegisterSource(root, "vendor-shared/private", SourcePolicy{Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("catalog mode = %o, want 600", got)
	}
	if _, err := Load(root, client.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
}

func TestLoadKeepsConfiguredSourceWhoseCheckoutIsMissing(t *testing.T) {
	root := t.TempDir()
	if err := RegisterSource(root, "vendor-shared/missing", SourcePolicy{Branch: "release", SkillPaths: []string{"skills/tool"}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, client.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	source, ok := loaded.Source("vendor-shared/missing")
	if !ok {
		t.Fatal("configured source disappeared because its checkout is missing")
	}
	if !source.IsCheckoutMissing() || source.Branch != "release" {
		t.Fatalf("missing source = %#v", source)
	}
	if len(source.DiscoveryPriority) != 0 || len(source.SkillPaths) != 1 {
		t.Fatalf("explicit missing-source discovery policy = priorities %v paths %v", source.DiscoveryPriority, source.SkillPaths)
	}
	if want := filepath.Join(root, "vendor", "shared", "missing"); source.Path != want {
		t.Fatalf("missing source path = %q, want %q", source.Path, want)
	}
}
