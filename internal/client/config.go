package client

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type registryFile struct {
	Version int                       `yaml:"version"`
	Clients map[ID]registryClientFile `yaml:"clients,omitempty"`
}

type registryClientFile struct {
	ProjectSkillsDir    string     `yaml:"projectSkillsDir,omitempty"`
	UserPromptDir       string     `yaml:"userPromptDir,omitempty"`
	UserPromptMode      PromptMode `yaml:"userPromptMode,omitempty"`
	UserPromptEntry     string     `yaml:"userPromptEntry,omitempty"`
	ProjectCommandsDir  string     `yaml:"projectCommandsDir,omitempty"`
	ProjectHooksDir     string     `yaml:"projectHooksDir,omitempty"`
	UserAgentsDir       string     `yaml:"userAgentsDir,omitempty"`
	UserOutputStylesDir string     `yaml:"userOutputStylesDir,omitempty"`
	ProjectMCPFile      string     `yaml:"projectMCPFile,omitempty"`
	ProjectMCPFormat    MCPFormat  `yaml:"projectMCPFormat,omitempty"`
}

func LoadRegistry(path string) (Registry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultRegistry(), nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("open client registry: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var config registryFile
	if err := decoder.Decode(&config); err != nil {
		return Registry{}, fmt.Errorf("decode client registry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents")
		}
		return Registry{}, fmt.Errorf("decode client registry: %w", err)
	}
	if config.Version != 1 {
		return Registry{}, fmt.Errorf("unsupported client registry version: %d", config.Version)
	}

	configured := make(map[ID]Definition, len(config.Clients))
	for id, entry := range config.Clients {
		configured[id] = Definition{
			ProjectSkillsDir:    entry.ProjectSkillsDir,
			UserPromptDir:       entry.UserPromptDir,
			UserPromptMode:      entry.UserPromptMode,
			UserPromptEntry:     entry.UserPromptEntry,
			ProjectCommandsDir:  entry.ProjectCommandsDir,
			ProjectHooksDir:     entry.ProjectHooksDir,
			UserAgentsDir:       entry.UserAgentsDir,
			UserOutputStylesDir: entry.UserOutputStylesDir,
			ProjectMCPFile:      entry.ProjectMCPFile,
			ProjectMCPFormat:    entry.ProjectMCPFormat,
		}
	}
	registry, err := NewRegistry(configured)
	if err != nil {
		return Registry{}, fmt.Errorf("client registry: %w", err)
	}
	return registry, nil
}
