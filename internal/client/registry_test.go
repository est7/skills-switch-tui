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

func TestRegistryRejectsProjectEscapingPath(t *testing.T) {
	if _, err := NewRegistry(map[ID]Definition{"pi": {ProjectSkillsDir: "../shared/skills"}}); err == nil {
		t.Fatal("NewRegistry() accepted a path outside the project")
	}
	if _, err := NewRegistry(map[ID]Definition{"pi": {UserPromptDir: "../shared/prompts"}}); err == nil {
		t.Fatal("NewRegistry() accepted a prompt path outside the user home")
	}
}

func TestRegistryExposesBuiltinPromptAndMCPAdapters(t *testing.T) {
	registry := DefaultRegistry()
	for id, relative := range map[ID]string{
		Codex:  ".codex",
		Claude: ".claude",
		Gemini: ".gemini",
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
	mcpFile, format, err := registry.MCPProjectFile("/tmp/project", Codex)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/project", ".codex", "config.toml"); mcpFile != want || format != MCPCodexTOML {
		t.Fatalf("Codex MCP adapter = %q %q, want %q %q", mcpFile, format, want, MCPCodexTOML)
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
	commandDir, err := registry.ProjectCommandsTargetDir("/tmp/project", Gemini)
	if err != nil || commandDir != filepath.Join("/tmp/project", ".gemini", "commands") {
		t.Fatalf("ProjectCommandsTargetDir() = %q, %v", commandDir, err)
	}
	hookDir, err := registry.ProjectHooksTargetDir("/tmp/project", Codex)
	if err != nil || hookDir != filepath.Join("/tmp/project", ".codex", "hooks") {
		t.Fatalf("ProjectHooksTargetDir() = %q, %v", hookDir, err)
	}
}
