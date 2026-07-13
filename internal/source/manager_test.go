package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
)

func TestUpdateDoesNotMutateAnySourceWhenPreflightFindsDirtySource(t *testing.T) {
	agentsRoot := t.TempDir()
	git := &recordingGit{responses: map[string]string{
		"clean|status --porcelain":               "",
		"clean|rev-parse HEAD":                   "aaaaaaaa\n",
		"clean|ls-remote origin refs/heads/main": "bbbbbbbb\trefs/heads/main\n",
		"dirty|status --porcelain":               " M SKILL.md\n",
	}}
	manager := Manager{AgentsRoot: agentsRoot, SourcesRoot: filepath.Join(agentsRoot, "sources"), Git: git}
	sources := []catalog.Source{
		{ID: "vendor/clean", Path: "clean", Branch: "main"},
		{ID: "vendor/dirty", Path: "dirty", Branch: "main"},
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
	target := filepath.Join(sourcesRoot, "vendor", "changing")
	writeSourceSkill(t, filepath.Join(target, "skills", "old"))
	writeSourceFile(t, filepath.Join(target, ".claude-plugin", "plugin.json"), `{
  "name": "changing",
  "skills": ["./skills/old"]
}`)
	git := &recordingGit{responses: map[string]string{
		target + "|status --porcelain":                                              "",
		target + "|rev-parse HEAD":                                                  "aaaaaaaa\n",
		target + "|ls-remote origin refs/heads/main":                                "bbbbbbbb\trefs/heads/main\n",
		agentsRoot + "|submodule update --init --remote -- sources/vendor/changing": "",
		target + "|sparse-checkout disable":                                         "",
		target + "|sparse-checkout init --cone":                                     "",
		target + "|sparse-checkout set .claude-plugin skills/new":                   "",
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
	manager := Manager{AgentsRoot: agentsRoot, SourcesRoot: sourcesRoot, Git: git}
	sources := []catalog.Source{{
		ID:     "vendor/changing",
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

func TestAddRegistersMainTrackingSparseSubmodule(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "worktrunk")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://github.com/max-sixty/worktrunk.git sources/vendor/worktrunk": "",
		target + "|sparse-checkout init --cone": "",
		target + "|sparse-checkout set skills":  "",
	}}
	manager := Manager{AgentsRoot: agentsRoot, SourcesRoot: sourcesRoot, Git: git}

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
	for _, fragment := range []string{"vendor/worktrunk", "branch: main", "- skills"} {
		if !strings.Contains(string(config), fragment) {
			t.Fatalf("catalog config does not contain %q:\n%s", fragment, config)
		}
	}
}

func TestAddDerivesSparseCheckoutFromDiscoveryPriority(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	target := filepath.Join(sourcesRoot, "vendor", "mattpocock-skills")
	git := &recordingGit{responses: map[string]string{
		agentsRoot + "|submodule add -b main https://github.com/mattpocock/skills.git sources/vendor/mattpocock-skills": "",
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
	manager := Manager{AgentsRoot: agentsRoot, SourcesRoot: sourcesRoot, Git: git}

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

func TestAddRejectsExistingCatalogPolicyBeforeGitMutation(t *testing.T) {
	agentsRoot := t.TempDir()
	sourcesRoot := filepath.Join(agentsRoot, "sources")
	if err := os.MkdirAll(sourcesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "version: 1\nsources:\n  vendor/worktrunk:\n    branch: main\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	git := &recordingGit{responses: map[string]string{}}
	manager := Manager{AgentsRoot: agentsRoot, SourcesRoot: sourcesRoot, Git: git}

	err := manager.Add(context.Background(), AddRequest{Name: "worktrunk", URL: "https://example.invalid/worktrunk.git"})
	if err == nil {
		t.Fatal("Add() accepted an existing source policy")
	}
	if len(git.calls) != 0 {
		t.Fatalf("Add() called git before catalog preflight: %v", git.calls)
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
