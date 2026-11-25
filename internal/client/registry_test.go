package client

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRegistryExtendsBuiltinsWithConfiguredClient(t *testing.T) {
	registry, err := NewRegistry(map[ID]Definition{
		"pi": {ProjectSkillsDir: ".pi/skills", UserPromptDir: ".pi"},
	})
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := []ID{Codex, Claude, Agy, "pi"}
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
	promptDir, err := registry.UserPromptTargetDir("/tmp/home", "pi")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/home", ".pi"); promptDir != want {
		t.Fatalf("UserPromptTargetDir() = %q, want %q", promptDir, want)
	}
	if _, _, err := registry.MCPProjectFile("/tmp/project", "pi"); err == nil {
		t.Fatal("Pi unexpectedly has an MCP adapter")
	}
}

func TestRegistryFiltersClientsByResourceCapability(t *testing.T) {
	registry, err := NewRegistry(map[ID]Definition{
		"mcp-only":   {ProjectMCPFile: ".mcp-only.json", ProjectMCPFormat: MCPClaudeJSON},
		"skill-only": {ProjectSkillsDir: ".skill-only/skills"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.IDsFor(CapabilityProjectSkills); !reflect.DeepEqual(got, []ID{Codex, Claude, Agy, "skill-only"}) {
		t.Fatalf("project Skill clients = %v", got)
	}
	if got := registry.IDsFor(CapabilityMCP); !reflect.DeepEqual(got, []ID{Codex, Claude, Agy, "mcp-only"}) {
		t.Fatalf("MCP clients = %v", got)
	}
	if err := registry.Require("mcp-only", CapabilitySkills); err == nil {
		t.Fatal("MCP-only client unexpectedly accepted as a Skill adapter")
	}
}

func TestRegistryRejectsProjectEscapingPath(t *testing.T) {
	if _, err := NewRegistry(map[ID]Definition{"pi": {ProjectSkillsDir: "../shared/skills"}}); err == nil {
		t.Fatal("NewRegistry() accepted a path outside the project")
	}
	if _, err := NewRegistry(map[ID]Definition{"pi": {UserPromptDir: "../shared/prompts"}}); err == nil {
		t.Fatal("NewRegistry() accepted a prompt path outside the user home")
	}
	if _, err := NewRegistry(map[ID]Definition{"pi": {UserSkillsDir: "../shared/skills"}}); err == nil {
		t.Fatal("NewRegistry() accepted a Skill path outside the user home")
	}
}

func TestRegistryExposesBuiltinUserSkillAdapters(t *testing.T) {
	registry := DefaultRegistry()
	for id, relative := range map[ID]string{
		Codex:  filepath.Join(".agents", "skills"),
		Claude: filepath.Join(".claude", "skills"),
		Agy:    filepath.Join(".gemini", "config", "skills"),
	} {
		got, err := registry.UserSkillsTargetDir("/tmp/home", id)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/tmp/home", relative); got != want {
			t.Fatalf("%s user Skill dir = %q, want %q", id, got, want)
		}
	}
	mode, err := registry.ProjectSkillsMode(Agy)
	if err != nil || mode != SkillModeCopy {
		t.Fatalf("Agy project skills mode = %q, %v; want %q", mode, err, SkillModeCopy)
	}
	mode, err = registry.ProjectSkillsMode(Codex)
	if err != nil || mode != SkillModeSymlink {
		t.Fatalf("Codex project skills mode = %q, %v; want %q", mode, err, SkillModeSymlink)
	}
}

func TestRegistryExposesBuiltinPromptAndMCPAdapters(t *testing.T) {
	registry := DefaultRegistry()
	for id, relative := range map[ID]string{
		Codex:  ".codex",
		Claude: ".claude",
		Agy:    filepath.Join(".gemini", "config"),
	} {
		promptDir, err := registry.UserPromptTargetDir("/tmp/home", id)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/tmp/home", relative); promptDir != want {
			t.Fatalf("%s prompt dir = %q, want %q", id, promptDir, want)
		}
	}
	mode, entry, err := registry.UserPromptAdapter(Codex)
	if err != nil || mode != PromptConcat || entry != "AGENTS.md" {
		t.Fatalf("Codex prompt adapter = %q %q, %v", mode, entry, err)
	}
	mode, entry, err = registry.UserPromptAdapter(Claude)
	if err != nil || mode != PromptTree || entry != "" {
		t.Fatalf("Claude prompt adapter = %q %q, %v", mode, entry, err)
	}
	mode, entry, err = registry.UserPromptAdapter(Agy)
	if err != nil || mode != PromptConcat || entry != "AGENTS.md" {
		t.Fatalf("Agy prompt adapter = %q %q, %v", mode, entry, err)
	}
	mcpFile, format, err := registry.MCPProjectFile("/tmp/project", Codex)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/project", ".codex", "config.toml"); mcpFile != want || format != MCPCodexTOML {
		t.Fatalf("Codex MCP adapter = %q %q, want %q %q", mcpFile, format, want, MCPCodexTOML)
	}
	mcpFile, format, err = registry.MCPProjectFile("/tmp/project", Agy)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/project", ".agents", "mcp_config.json"); mcpFile != want || format != MCPAgyJSON {
		t.Fatalf("Agy MCP adapter = %q %q, want %q %q", mcpFile, format, want, MCPAgyJSON)
	}
	agentsDir, err := registry.UserAgentsTargetDir("/tmp/home", Agy)
	if err != nil || agentsDir != filepath.Join("/tmp/home", ".gemini", "config", "agents") {
		t.Fatalf("Agy user agents dir = %q, %v", agentsDir, err)
	}
}

func TestRegistryValidatesConcatPromptAdapter(t *testing.T) {
	if _, err := NewRegistry(map[ID]Definition{"pi": {UserPromptDir: ".pi", UserPromptMode: PromptConcat}}); err == nil {
		t.Fatal("concat prompt adapter without an entry was accepted")
	}
	registry, err := NewRegistry(map[ID]Definition{"pi": {
		UserPromptDir: ".pi", UserPromptMode: PromptConcat, UserPromptEntry: "AGENTS.md",
	}})
	if err != nil {
		t.Fatal(err)
	}
	mode, entry, err := registry.UserPromptAdapter("pi")
	if err != nil || mode != PromptConcat || entry != "AGENTS.md" {
		t.Fatalf("Pi prompt adapter = %q %q, %v", mode, entry, err)
	}
}

func TestRegistryExposesBuiltinCommandAndHookAdapters(t *testing.T) {
	registry := DefaultRegistry()
	commandDir, err := registry.ProjectCommandsTargetDir("/tmp/project", Claude)
	if err != nil || commandDir != filepath.Join("/tmp/project", ".claude", "commands") {
		t.Fatalf("ProjectCommandsTargetDir() = %q, %v", commandDir, err)
	}
	hookDir, err := registry.ProjectHooksTargetDir("/tmp/project", Codex)
	if err != nil || hookDir != filepath.Join("/tmp/project", ".codex", "hooks") {
		t.Fatalf("ProjectHooksTargetDir() = %q, %v", hookDir, err)
	}
}

func TestRegistryValidatesProjectSkillsModeAndCanonicalizesRelativePaths(t *testing.T) {
	if _, err := NewRegistry(map[ID]Definition{
		"badmode": {ProjectSkillsDir: ".bad/skills", ProjectSkillsMode: SkillMode("hardlink")},
	}); err == nil {
		t.Fatal("NewRegistry accepted unknown projectSkillsMode")
	}
	if _, err := NewRegistry(map[ID]Definition{
		"nodir": {UserSkillsDir: ".nodir/skills", ProjectSkillsMode: SkillModeCopy},
	}); err == nil {
		t.Fatal("NewRegistry accepted projectSkillsMode without projectSkillsDir")
	}

	registry, err := NewRegistry(map[ID]Definition{
		Agy:      {ProjectSkillsMode: SkillModeSymlink},
		"custom": {ProjectSkillsDir: "./.agents//skills/", UserSkillsDir: "./.custom/skills/"},
	})
	if err != nil {
		t.Fatalf("NewRegistry override: %v", err)
	}
	if mode, err := registry.ProjectSkillsMode(Agy); err != nil || mode != SkillModeSymlink {
		t.Fatalf("overridden agy mode = %q, %v; want symlink", mode, err)
	}
	def, ok := registry.Definition("custom")
	if !ok || def.ProjectSkillsDir != ".agents/skills" || def.UserSkillsDir != ".custom/skills" {
		t.Fatalf("normalized custom definition = %+v, %v", def, ok)
	}
	for _, d := range registry.Definitions() {
		if d.ID == "custom" && d.ProjectSkillsDir != ".agents/skills" {
			t.Fatalf("Definitions() did not preserve normalized path: %+v", d)
		}
	}
}
