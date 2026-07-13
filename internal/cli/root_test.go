package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnableThenListReportsProjectState(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(sourcesRoot, "local", "worktrunk")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: worktrunk\ndescription: Manage worktrees.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t,
		"--sources", sourcesRoot,
		"--project", projectRoot,
		"enable", "local/worktrunk",
		"--client", "codex",
	); err != nil {
		t.Fatalf("enable command: %v", err)
	}

	output, err := execute(t,
		"--sources", sourcesRoot,
		"--project", projectRoot,
		"list", "--json",
	)
	if err != nil {
		t.Fatalf("list command: %v", err)
	}
	var result struct {
		Skills []struct {
			ID      string            `json:"id"`
			Clients map[string]string `json:"clients"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode list output %q: %v", output, err)
	}
	if got, want := len(result.Skills), 1; got != want {
		t.Fatalf("skill count = %d, want %d", got, want)
	}
	if got, want := result.Skills[0].ID, "local/worktrunk"; got != want {
		t.Fatalf("skill id = %q, want %q", got, want)
	}
	if got, want := result.Skills[0].Clients["codex"], "enabled"; got != want {
		t.Fatalf("codex state = %q, want %q", got, want)
	}
}

func TestConfiguredClientCanBeEnabledWithoutCodeChanges(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")
	config := "version: 1\nclients:\n  pi:\n    projectSkillsDir: .pi/skills\n"
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t,
		"--sources", sourcesRoot,
		"--project", projectRoot,
		"enable", "local/portable",
		"--client", "pi",
	); err != nil {
		t.Fatalf("enable pi: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(projectRoot, ".pi", "skills", "portable")); err != nil {
		t.Fatalf("pi projection was not created: %v", err)
	}
}

func TestChineseLanguageLocalizesHelpAndHumanListHeaders(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")

	help, err := execute(t, "--lang", "zh", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(help, []byte("切换项目级 Agent Skills")) {
		t.Fatalf("Chinese help was not localized:\n%s", help)
	}

	output, err := execute(t,
		"--lang", "zh",
		"--sources", sourcesRoot,
		"--project", projectRoot,
		"list",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte("来源")) {
		t.Fatalf("Chinese list header was not localized:\n%s", output)
	}
	sourceHelp, err := execute(t, "--lang", "zh", "source", "add", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(sourceHelp, []byte("来源发现策略优先级")) {
		t.Fatalf("Chinese discovery priority flag was not localized:\n%s", sourceHelp)
	}
}

func TestSourceListReportsResolvedDiscoveryStrategy(t *testing.T) {
	sourcesRoot := t.TempDir()
	vendorRoot := filepath.Join(sourcesRoot, "vendor", "curated")
	writeCLISkill(t, filepath.Join(vendorRoot, "skills", "registered"), "registered")
	manifestPath := filepath.Join(vendorRoot, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{
  "name": "curated",
  "skills": ["./skills/registered"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `version: 1
sources:
  vendor/curated:
    discoveryPriority: [claude-plugin, skills-dir]
`
	if err := os.WriteFile(filepath.Join(sourcesRoot, "catalog.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := execute(t, "--sources", sourcesRoot, "source", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, []byte(`"discoveryStrategy": "claude-plugin"`)) {
		t.Fatalf("source JSON omitted resolved discovery strategy: %s", output)
	}
	if !bytes.Contains(output, []byte(`"discoveryPriority"`)) {
		t.Fatalf("source JSON omitted discovery priority: %s", output)
	}
}

func TestArchivedSkillsRequireExplicitListingAndCannotBeEnabled(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "archive", "waza", "read"), "waza-read")

	withoutArchive, err := execute(t, "--sources", sourcesRoot, "--project", projectRoot, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	withArchive, err := execute(t, "--sources", sourcesRoot, "--project", projectRoot, "list", "--archive", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(withoutArchive, []byte("archive/waza/read")) {
		t.Fatalf("archive leaked into default list: %s", withoutArchive)
	}
	if !bytes.Contains(withArchive, []byte("archive/waza/read")) {
		t.Fatalf("explicit archive list omitted skill: %s", withArchive)
	}

	_, err = execute(t,
		"--sources", sourcesRoot,
		"--project", projectRoot,
		"enable", "archive/waza/read",
		"--client", "codex",
	)
	if err == nil {
		t.Fatal("enable accepted an archived skill")
	}
	if _, statErr := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "waza-read")); !os.IsNotExist(statErr) {
		t.Fatalf("archived skill projection unexpectedly exists: %v", statErr)
	}
}

func TestShowStatusAndDoctorExposeStableJSON(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")
	base := []string{"--sources", sourcesRoot, "--project", projectRoot}
	if _, err := execute(t, append(base, "enable", "local/portable", "--client", "codex")...); err != nil {
		t.Fatal(err)
	}

	show, err := execute(t, append(base, "show", "local/portable", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(show, []byte(`"codex"`)) || !bytes.Contains(show, []byte(`"enabled"`)) {
		t.Fatalf("show JSON missing projection state: %s", show)
	}

	status, err := execute(t, append(base, "status", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(status, []byte(`"enabled": 1`)) {
		t.Fatalf("status JSON missing enabled count: %s", status)
	}

	doctor, err := execute(t, append(base, "doctor", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doctor, []byte(`"healthy": true`)) {
		t.Fatalf("doctor JSON did not report healthy state: %s", doctor)
	}
}

func TestMultiClientEnableIsAtomicAcrossClientDirectories(t *testing.T) {
	sourcesRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLISkill(t, filepath.Join(sourcesRoot, "local", "portable"), "portable")
	conflict := filepath.Join(projectRoot, ".claude", "skills", "portable")
	if err := os.MkdirAll(conflict, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := execute(t,
		"--sources", sourcesRoot,
		"--project", projectRoot,
		"enable", "local/portable",
		"--client", "codex",
		"--client", "claude",
	)
	if err == nil {
		t.Fatal("enable succeeded despite a Claude conflict")
	}
	if _, statErr := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "portable")); !os.IsNotExist(statErr) {
		t.Fatalf("Codex projection changed before full preflight: %v", statErr)
	}
}

func writeCLISkill(t *testing.T, directory, name string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + name + "\ndescription: test\n---\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func execute(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	var stdout bytes.Buffer
	command := NewRootCommand("test")
	command.SetOut(&stdout)
	command.SetErr(&stdout)
	command.SetArgs(args)
	err := command.Execute()
	return stdout.Bytes(), err
}
