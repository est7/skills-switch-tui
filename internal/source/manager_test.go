package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestUpdateDoesNotMutateAnySourceWhenPreflightFindsDirtySource(t *testing.T) {
	agentsRoot := t.TempDir()
	git := &recordingGit{responses: map[string]string{
		"clean|status --porcelain":               "",
		"clean|rev-parse HEAD":                   "aaaaaaaa\n",
		"clean|ls-remote origin refs/heads/main": "bbbbbbbb\trefs/heads/main\n",
		"dirty|status --porcelain":               " M SKILL.md\n",
	}}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: filepath.Join(agentsRoot, "sources"), Git: git}
	sources := []catalog.Source{
		{ID: "vendor-shared/clean", Kind: catalog.SourceVendor, Scope: "shared", Path: "clean", Branch: "main"},
		{ID: "vendor-shared/dirty", Kind: catalog.SourceVendor, Scope: "shared", Path: "dirty", Branch: "main"},
	}

	_, err := manager.Update(context.Background(), sources, false)
	var dirty *DirtyError
	if !errors.As(err, &dirty) {
		t.Fatalf("Update() error = %v, want DirtyError", err)
	}
	for _, call := range git.calls {
		if strings.Contains(call, "submodule update") {
			t.Fatalf("Update() mutated sources after failed preflight: %s", call)
		}
	}
}

func TestUpdateRecomputesDerivedSparsePathsAfterManifestChanges(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "changing")
	writeSourceSkill(t, filepath.Join(target, "skills", "old"))
	writeSourceFile(t, filepath.Join(target, ".claude-plugin", "plugin.json"), `{
  "name": "changing",
  "skills": ["./skills/old"]
}`)
	git := &recordingGit{responses: map[string]string{
		target + "|status --porcelain":               "",
		target + "|rev-parse HEAD":                   "aaaaaaaa\n",
		target + "|ls-remote origin refs/heads/main": "bbbbbbbb\trefs/heads/main\n",
		agentsRoot + "|submodule update --init --remote -- sources/vendor/shared/changing": "",
		target + "|sparse-checkout disable":                                                "",
		target + "|sparse-checkout init --cone":                                            "",
		target + "|sparse-checkout set .claude-plugin skills/new":                          "",
	}}
	git.onCall = func(key string) {
		if !strings.Contains(key, "|submodule update ") {
			return
		}
		writeSourceSkill(t, filepath.Join(target, "skills", "new"))
		writeSourceFile(t, filepath.Join(target, ".claude-plugin", "plugin.json"), `{
  "name": "changing",
  "skills": ["./skills/new"]
}`)
	}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}
	sources := []catalog.Source{{
		ID:     "vendor-shared/changing",
		Kind:   catalog.SourceVendor,
		Scope:  "shared",
		Path:   target,
		Branch: "main",
		DiscoveryPriority: []catalog.DiscoveryStrategy{
			catalog.DiscoveryClaudePlugin,
			catalog.DiscoverySkillsDir,
		},
	}}

	if _, err := manager.Update(context.Background(), sources, false); err != nil {
		t.Fatal(err)
	}
	var recomputed bool
	for _, call := range git.calls {
		if call == "sparse-checkout set .claude-plugin skills/new" {
			recomputed = true
		}
	}
	if !recomputed {
		t.Fatalf("Update() did not apply manifest-derived sparse paths: %v", git.calls)
	}
}

func TestAddRejectsUnknownClientScopeBeforeGitMutation(t *testing.T) {
	agentsRoot := t.TempDir()
	git := &recordingGit{responses: map[string]string{}}
	manager := Manager{
		RepositoryRoot: agentsRoot,
		SkillsRoot:     filepath.Join(agentsRoot, "sources"),
		Git:            git,
		Clients:        client.DefaultRegistry(),
	}

	err := manager.Add(context.Background(), AddRequest{
		Name:  "pi-tools",
		URL:   "https://example.invalid/pi-tools.git",
		Scope: "pi",
	})
	if err == nil || !strings.Contains(err.Error(), `unknown client "pi"`) {
		t.Fatalf("Add() error = %v, want unknown client rejection", err)
	}
	if len(git.calls) != 0 {
		t.Fatalf("Add() called git before client validation: %v", git.calls)
	}
}

func TestRemoveRejectsDirtyVendorBeforeGitMutation(t *testing.T) {
	agentsRoot := t.TempDir()
	target := filepath.Join(agentsRoot, "resources", "skills", "vendor", "shared", "dirty")
	git := &recordingGit{responses: map[string]string{
		target + "|status --porcelain": " M SKILL.md\n",
	}}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: filepath.Join(agentsRoot, "resources", "skills"), Git: git}

	err := manager.Remove(context.Background(), catalog.Source{
		ID: "vendor-shared/dirty", Kind: catalog.SourceVendor, Scope: "shared", Path: target,
	})
	var dirty *DirtyError
	if !errors.As(err, &dirty) {
		t.Fatalf("Remove() error = %v, want DirtyError", err)
	}
	if len(git.calls) != 1 || git.calls[0] != "status --porcelain" {
		t.Fatalf("dirty remove mutated git: %v", git.calls)
	}
}

func TestRemoveUsesGitRMAndUnregistersCatalogPolicy(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "clean")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "version: 1\nsources:\n  vendor-shared/clean:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	git := &recordingGit{responses: map[string]string{
		target + "|status --porcelain":                                "",
		agentsRoot + "|rm -f -- resources/skills/vendor/shared/clean": "",
	}}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}

	if err := manager.Remove(context.Background(), catalog.Source{
		ID: "vendor-shared/clean", Kind: catalog.SourceVendor, Scope: "shared", Path: target,
	}); err != nil {
		t.Fatal(err)
	}
	if len(git.calls) != 2 || git.calls[1] != "rm -f -- resources/skills/vendor/shared/clean" {
		t.Fatalf("unexpected remove calls: %v", git.calls)
	}
	updated, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "vendor-shared/clean") {
		t.Fatalf("catalog policy was not removed:\n%s", updated)
	}
}

func TestAddRegistersMainTrackingSparseSubmodule(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://github.com/max-sixty/worktrunk.git sources/vendor/shared/worktrunk": "",
		target + "|sparse-checkout init --cone": "",
		target + "|sparse-checkout set skills":  "",
	}}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}

	if err := manager.Add(context.Background(), AddRequest{
		Name:        "worktrunk",
		URL:         "https://github.com/max-sixty/worktrunk.git",
		Branch:      "main",
		SparsePaths: []string{"skills"},
	}); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"vendor-shared/worktrunk", "branch: main", "- skills"} {
		if !strings.Contains(string(config), fragment) {
			t.Fatalf("catalog config does not contain %q:\n%s", fragment, config)
		}
	}
}

func TestAddDerivesRepositoryRootForNestedResourceCatalog(t *testing.T) {
	repositoryRoot := t.TempDir()
	sourcesRoot := filepath.Join(repositoryRoot, "resources", "skills")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "worktrunk")
	git := &recordingGit{responses: map[string]string{
		sourcesRoot + "|rev-parse --show-toplevel": repositoryRoot + "\n",
		repositoryRoot + "|submodule add -b main https://github.com/max-sixty/worktrunk.git resources/skills/vendor/shared/worktrunk": "",
		target + "|sparse-checkout init --cone": "",
		target + "|sparse-checkout set skills":  "",
	}}
	git.onCall = func(key string) {
		if strings.Contains(key, "|submodule add ") {
			writeSourceSkill(t, filepath.Join(target, "skills", "worktrunk"))
		}
	}
	manager := Manager{SkillsRoot: sourcesRoot, Git: git, Clients: client.DefaultRegistry()}

	if err := manager.Add(context.Background(), AddRequest{
		Name:        "worktrunk",
		URL:         "https://github.com/max-sixty/worktrunk.git",
		SparsePaths: []string{"skills"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAddDerivesSparseCheckoutFromDiscoveryPriority(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "mattpocock-skills")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://github.com/mattpocock/skills.git sources/vendor/shared/mattpocock-skills": "",
		target + "|sparse-checkout init --cone":                          "",
		target + "|sparse-checkout set .claude-plugin skills/registered": "",
	}}
	git.onCall = func(key string) {
		if !strings.Contains(key, "|submodule add ") {
			return
		}
		writeSourceSkill(t, filepath.Join(target, "skills", "registered"))
		writeSourceSkill(t, filepath.Join(target, "skills", "unregistered"))
		writeSourceFile(t, filepath.Join(target, ".claude-plugin", "plugin.json"), `{
  "name": "mattpocock-skills",
  "skills": ["./skills/registered"]
}`)
	}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}

	if err := manager.Add(context.Background(), AddRequest{
		Name:   "mattpocock-skills",
		URL:    "https://github.com/mattpocock/skills.git",
		Branch: "main",
		DiscoveryPriority: []catalog.DiscoveryStrategy{
			catalog.DiscoveryClaudePlugin,
			catalog.DiscoverySkillsDir,
		},
	}); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"discoveryPriority:", "- claude-plugin", "- skills-dir"} {
		if !strings.Contains(string(config), fragment) {
			t.Fatalf("catalog config does not contain %q:\n%s", fragment, config)
		}
	}
}

func TestAddRegistersExplicitSkillPathsAndChecksOutOnlyThosePaths(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "spellbook")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://github.com/majiayu000/spellbook.git sources/vendor/shared/spellbook": "",
		target + "|sparse-checkout init --cone":               "",
		target + "|sparse-checkout set skills/codebase-audit": "",
	}}
	git.onCall = func(key string) {
		if strings.Contains(key, "|submodule add ") {
			writeSourceSkill(t, filepath.Join(target, "skills", "codebase-audit"))
		}
	}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}

	if err := manager.Add(context.Background(), AddRequest{
		Name:       "spellbook",
		URL:        "https://github.com/majiayu000/spellbook.git",
		Branch:     "main",
		SkillPaths: []string{"skills/codebase-audit"},
	}); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"skillPaths:", "- skills/codebase-audit"} {
		if !strings.Contains(string(config), fragment) {
			t.Fatalf("catalog config does not contain %q:\n%s", fragment, config)
		}
	}
}

func TestAddRejectsExistingCatalogPolicyBeforeGitMutation(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "version: 1\nsources:\n  vendor-shared/worktrunk:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	git := &recordingGit{responses: map[string]string{}}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git}

	err := manager.Add(context.Background(), AddRequest{Name: "worktrunk", URL: "https://example.invalid/worktrunk.git"})
	if err == nil {
		t.Fatal("Add() accepted an existing source policy")
	}
	if len(git.calls) != 0 {
		t.Fatalf("Add() called git before catalog preflight: %v", git.calls)
	}
}

func TestAddRollsBackSubmoduleWhenDiscoveryFails(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "resources", "skills")
	target := filepath.Join(sourcesRoot, "vendor", "shared", "empty")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://example.invalid/empty.git resources/skills/vendor/shared/empty": "",
		agentsRoot + "|rm -f -- resources/skills/vendor/shared/empty":                                                "",
	}}
	git.onCall = func(key string) {
		if strings.Contains(key, "|submodule add ") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git, Clients: client.DefaultRegistry()}

	err := manager.Add(context.Background(), AddRequest{
		Name: "empty", URL: "https://example.invalid/empty.git", SparsePaths: []string{"skills"},
	})
	if err == nil {
		t.Fatal("empty source unexpectedly succeeded")
	}
	if len(git.calls) != 3 || git.calls[2] != "rm -f -- resources/skills/vendor/shared/empty" {
		t.Fatalf("failed add did not roll back submodule: %v", git.calls)
	}
}

func TestAddRegistersClientScopedVendorSource(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "pi", "pi-tools")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://example.invalid/pi-tools.git sources/vendor/pi/pi-tools": "",
		target + "|sparse-checkout init --cone": "",
		target + "|sparse-checkout set skills":  "",
	}}
	git.onCall = func(key string) {
		if strings.Contains(key, "|submodule add ") {
			writeSourceSkill(t, filepath.Join(target, "skills", "pi-tool"))
		}
	}
	registry, err := client.NewRegistry(map[client.ID]client.Definition{
		"pi": {ProjectSkillsDir: ".pi/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := Manager{RepositoryRoot: agentsRoot, SkillsRoot: sourcesRoot, Git: git, Clients: registry}

	if err := manager.Add(context.Background(), AddRequest{
		Name:   "pi-tools",
		URL:    "https://example.invalid/pi-tools.git",
		Branch: "main",
		Scope:  "pi",
	}); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(sourcesRoot, "catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "vendor-pi-only/pi-tools") {
		t.Fatalf("catalog config omitted scoped logical source id:\n%s", config)
	}
}

type recordingGit struct {
	responses map[string]string
	calls     []string
	onCall    func(key string)
}

func (g *recordingGit) Output(_ context.Context, directory string, arguments ...string) ([]byte, error) {
	key := directory + "|" + strings.Join(arguments, " ")
	g.calls = append(g.calls, strings.Join(arguments, " "))
	if g.onCall != nil {
		g.onCall(key)
	}
	response, ok := g.responses[key]
	if !ok {
		return nil, errors.New("unexpected git call: " + key)
	}
	return []byte(response), nil
}

func writeSourceSkill(t *testing.T, directory string) {
	t.Helper()
	writeSourceFile(t, filepath.Join(directory, "SKILL.md"), "---\nname: "+filepath.Base(directory)+"\ndescription: test\n---\n")
}

func writeSourceFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
