package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/client"
)

// Discovery resolves every plugin a marketplace declares, including plugins that
// ship no skills, so the planned sparse checkout must stay discoverable.
func TestPlannedSparseCheckoutStaysDiscoverable(t *testing.T) {
	const skillManifest = "---\nname: %s\ndescription: Registered through the marketplace.\n---\n"
	tests := []struct {
		name        string
		build       func(t *testing.T, root string)
		wantSkillID string
	}{
		{
			name: "hooks-only plugin with a manifest",
			build: func(t *testing.T, root string) {
				writeSkill(t, filepath.Join(root, "plugins", "worktrunk", "skills", "worktrunk"), fmt.Sprintf(skillManifest, "worktrunk"))
				writeJSON(t, filepath.Join(root, "plugins", "worktrunk", ".codex-plugin", "plugin.json"), `{"name": "worktrunk"}`)
				writeJSON(t, filepath.Join(root, "plugins", "markers", ".codex-plugin", "plugin.json"), `{"name": "markers"}`)
				writeFile(t, filepath.Join(root, "plugins", "markers", "hooks", "wt.sh"), "#!/bin/sh\n")
				writeMarketplace(t, root, "./plugins/worktrunk", "./plugins/markers")
			},
			wantSkillID: "vendor-shared/fixture/plugins/worktrunk/skills/worktrunk",
		},
		{
			name: "plugin with neither a manifest nor skills",
			build: func(t *testing.T, root string) {
				writeSkill(t, filepath.Join(root, "plugins", "tools", "skills", "tools"), fmt.Sprintf(skillManifest, "tools"))
				writeFile(t, filepath.Join(root, "plugins", "bare", "hooks", "run.sh"), "#!/bin/sh\n")
				writeMarketplace(t, root, "./plugins/tools", "./plugins/bare")
			},
			wantSkillID: "vendor-shared/fixture/plugins/tools/skills/tools",
		},
		{
			name: "root plugin without a manifest",
			build: func(t *testing.T, root string) {
				writeSkill(t, filepath.Join(root, "skills", "rooted"), fmt.Sprintf(skillManifest, "rooted"))
				writeMarketplace(t, root, "./")
			},
			wantSkillID: "vendor-shared/fixture/skills/rooted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fullRoot := filepath.Join(t.TempDir(), "fixture")
			test.build(t, fullRoot)
			writeFile(t, filepath.Join(fullRoot, "src", "main.rs"), "fn main() {}\n")

			plan, err := PlanVendorDiscovery(fullRoot, []DiscoveryStrategy{DiscoveryAgentsMarketplace, DiscoverySkillsDir}, nil)
			if err != nil {
				t.Fatalf("PlanVendorDiscovery() error = %v", err)
			}
			if got, want := plan.Strategy, DiscoveryAgentsMarketplace; got != want {
				t.Fatalf("strategy = %q, want %q", got, want)
			}
			if len(plan.SparsePaths) == 0 {
				t.Fatal("PlanVendorDiscovery() planned a full checkout, want a sparse one")
			}

			sourcesRoot := t.TempDir()
			sparseRoot := filepath.Join(sourcesRoot, "vendor", "shared", "fixture")
			materializeConeCheckout(t, fullRoot, sparseRoot, plan.SparsePaths)
			if _, err := os.Stat(filepath.Join(sparseRoot, "src")); !os.IsNotExist(err) {
				t.Fatalf("sparse checkout kept unrelated source tree (stat err = %v); plan = %v", err, plan.SparsePaths)
			}
			config := "version: 1\nsources:\n  vendor-shared/fixture:\n    discoveryPriority: [agents-marketplace, skills-dir]\n"
			if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}

			loaded, err := Load(sourcesRoot, client.DefaultRegistry())
			if err != nil {
				t.Fatalf("Load() on the planned sparse checkout error = %v; plan = %v", err, plan.SparsePaths)
			}
			source, ok := loaded.Source("vendor-shared/fixture")
			if !ok {
				t.Fatal("fixture source missing from catalog")
			}
			if source.Availability != SourceAvailable {
				t.Fatalf("source availability = %q (%s); plan = %v", source.Availability, source.AvailabilityDetail, plan.SparsePaths)
			}
			if got, want := source.DiscoveryStrategy, DiscoveryAgentsMarketplace; got != want {
				t.Fatalf("discovery strategy = %q, want %q", got, want)
			}
			if _, ok := loaded.Skill(test.wantSkillID); !ok {
				t.Fatalf("skill %q missing from sparse checkout; plan = %v", test.wantSkillID, plan.SparsePaths)
			}
		})
	}
}

func writeMarketplace(t *testing.T, root string, pluginPaths ...string) {
	t.Helper()
	plugins := make([]string, len(pluginPaths))
	for index, path := range pluginPaths {
		plugins[index] = fmt.Sprintf(`{"name": "plugin-%d", "source": {"source": "local", "path": %q}}`, index, path)
	}
	writeJSON(t, filepath.Join(root, ".agents", "plugins", "marketplace.json"),
		`{"name": "fixture", "plugins": [`+strings.Join(plugins, ", ")+`]}`)
}

// materializeConeCheckout copies what `git sparse-checkout set --cone <paths>`
// would leave on disk: every listed directory recursively, plus the files that
// sit directly in the root and in each ancestor of a listed directory.
func materializeConeCheckout(t *testing.T, from, to string, paths []string) {
	t.Helper()
	copyDirectFiles(t, from, to)
	for _, path := range paths {
		parts := strings.Split(path, "/")
		for depth := 1; depth < len(parts); depth++ {
			ancestor := filepath.Join(parts[:depth]...)
			copyDirectFiles(t, filepath.Join(from, ancestor), filepath.Join(to, ancestor))
		}
		copyTree(t, filepath.Join(from, filepath.FromSlash(path)), filepath.Join(to, filepath.FromSlash(path)))
	}
}

func copyDirectFiles(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			copyFile(t, filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name()))
		}
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(from, current)
		if err != nil {
			return err
		}
		copyFile(t, current, filepath.Join(to, relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, to, string(data))
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
