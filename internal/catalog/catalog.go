package catalog

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
	"gopkg.in/yaml.v3"
)

type Client = client.ID

const (
	ClientCodex  = client.Codex
	ClientClaude = client.Claude
	ClientGemini = client.Gemini
)

type Skill struct {
	ID                  string
	Name                string
	Description         string
	SourceID            string
	Path                string
	Targets             map[Client]bool
	CompatibilityReason string
}

func (s Skill) Supports(client Client) bool {
	return s.Targets[client]
}

type Source struct {
	ID                string
	Path              string
	Branch            string
	SparsePaths       []string
	DiscoveryPriority []DiscoveryStrategy
	DiscoveryStrategy DiscoveryStrategy
	Archived          bool
	Skills            []Skill
}

type Catalog struct {
	Root    string
	Sources []Source
	Clients client.Registry
	byID    map[string]Skill
}

func (c Catalog) Skill(id string) (Skill, bool) {
	skill, ok := c.byID[id]
	return skill, ok
}

func (c Catalog) Source(id string) (Source, bool) {
	for _, source := range c.Sources {
		if source.ID == id {
			return source, true
		}
	}
	return Source{}, false
}

type configFile struct {
	Version   int                       `yaml:"version"`
	Clients   map[Client]clientConfig   `yaml:"clients,omitempty"`
	Defaults  targetConfig              `yaml:"defaults,omitempty"`
	Sources   map[string]sourceConfig   `yaml:"sources,omitempty"`
	Overrides map[string]overrideConfig `yaml:"overrides,omitempty"`
}

type clientConfig struct {
	ProjectSkillsDir string `yaml:"projectSkillsDir"`
}

type sourceConfig struct {
	Branch            string              `yaml:"branch"`
	SparsePaths       []string            `yaml:"sparsePaths"`
	DiscoveryPriority []DiscoveryStrategy `yaml:"discoveryPriority"`
}

type targetConfig struct {
	Targets []Client `yaml:"targets"`
}

type overrideConfig struct {
	Targets []Client `yaml:"targets"`
	Reason  string   `yaml:"reason"`
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type SourcePolicy struct {
	Branch            string
	SparsePaths       []string
	DiscoveryPriority []DiscoveryStrategy
}

func RegisterSource(root, id string, policy SourcePolicy) error {
	if err := validateVendorSourceID(id); err != nil {
		return err
	}
	configPath := filepath.Join(root, "catalog.yaml")
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	if config.Version == 0 {
		config.Version = 1
	}
	if config.Sources == nil {
		config.Sources = make(map[string]sourceConfig)
	}
	if _, exists := config.Sources[id]; exists {
		return fmt.Errorf("source policy already exists: %s", id)
	}
	config.Sources[id] = sourceConfig{
		Branch:            policy.Branch,
		SparsePaths:       append([]string(nil), policy.SparsePaths...),
		DiscoveryPriority: append([]DiscoveryStrategy(nil), policy.DiscoveryPriority...),
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode catalog config: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create sources root: %w", err)
	}
	temporary, err := os.CreateTemp(root, ".catalog-*.yaml")
	if err != nil {
		return fmt.Errorf("create catalog temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write catalog temporary file: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set catalog permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close catalog temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, configPath); err != nil {
		return fmt.Errorf("replace catalog config: %w", err)
	}
	return nil
}

func ValidateSourceRegistration(root, id string) error {
	if err := validateVendorSourceID(id); err != nil {
		return err
	}
	config, err := loadConfig(filepath.Join(root, "catalog.yaml"))
	if err != nil {
		return err
	}
	if _, exists := config.Sources[id]; exists {
		return fmt.Errorf("source policy already exists: %s", id)
	}
	return nil
}

func validateVendorSourceID(id string) error {
	if !strings.HasPrefix(id, "vendor/") || strings.TrimPrefix(id, "vendor/") == "" {
		return fmt.Errorf("vendor source id must start with vendor/: %s", id)
	}
	return nil
}

func Load(root string) (Catalog, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Catalog{}, fmt.Errorf("resolve sources root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return Catalog{}, fmt.Errorf("stat sources root: %w", err)
	}
	if !info.IsDir() {
		return Catalog{}, fmt.Errorf("sources root is not a directory: %s", absRoot)
	}

	config, err := loadConfig(filepath.Join(absRoot, "catalog.yaml"))
	if err != nil {
		return Catalog{}, err
	}
	configuredClients := make(map[client.ID]string, len(config.Clients))
	for id, definition := range config.Clients {
		configuredClients[id] = definition.ProjectSkillsDir
	}
	clients, err := client.NewRegistry(configuredClients)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalog clients: %w", err)
	}
	defaultTargets, err := targetSet(config.Defaults.Targets, clients)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalog defaults: %w", err)
	}
	if len(defaultTargets) == 0 {
		defaultTargets, _ = targetSet(clients.IDs(), clients)
	}

	sources := make([]Source, 0)
	localRoot := filepath.Join(absRoot, "local")
	if isDirectory(localRoot) {
		source, err := discoverSource("local", localRoot, defaultTargets, config.Overrides, clients)
		if err != nil {
			return Catalog{}, err
		}
		if len(source.Skills) > 0 {
			sources = append(sources, source)
		}
	}

	vendorRoot := filepath.Join(absRoot, "vendor")
	entries, err := os.ReadDir(vendorRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Catalog{}, fmt.Errorf("read vendor sources: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := "vendor/" + entry.Name()
		path := filepath.Join(vendorRoot, entry.Name())
		policy := config.Sources[id]
		source, discoverErr := discoverVendorSource(id, path, policy.DiscoveryPriority, defaultTargets, config.Overrides, clients)
		if discoverErr != nil {
			return Catalog{}, discoverErr
		}
		source.Branch = policy.Branch
		if source.Branch == "" {
			source.Branch = "main"
		}
		source.SparsePaths = append([]string(nil), policy.SparsePaths...)
		source.DiscoveryPriority = normalizedDiscoveryPriority(policy.DiscoveryPriority)
		sources = append(sources, source)
	}

	archiveRoot := filepath.Join(absRoot, "archive")
	archiveEntries, err := os.ReadDir(archiveRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Catalog{}, fmt.Errorf("read archived sources: %w", err)
	}
	for _, entry := range archiveEntries {
		if !entry.IsDir() {
			continue
		}
		id := "archive/" + entry.Name()
		path := filepath.Join(archiveRoot, entry.Name())
		source, discoverErr := discoverSource(id, path, defaultTargets, config.Overrides, clients)
		if discoverErr != nil {
			return Catalog{}, discoverErr
		}
		if len(source.Skills) > 0 {
			source.Archived = true
			sources = append(sources, source)
		}
	}

	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	byID := make(map[string]Skill)
	for _, source := range sources {
		for _, skill := range source.Skills {
			if _, exists := byID[skill.ID]; exists {
				return Catalog{}, fmt.Errorf("duplicate skill id: %s", skill.ID)
			}
			byID[skill.ID] = skill
		}
	}

	return Catalog{Root: absRoot, Sources: sources, Clients: clients, byID: byID}, nil
}

func discoverSource(id, root string, defaults map[Client]bool, overrides map[string]overrideConfig, clients client.Registry) (Source, error) {
	return discoverSourceRoots(id, root, []string{root}, defaults, overrides, clients)
}

func discoverSourceRoots(id, root string, scanRoots []string, defaults map[Client]bool, overrides map[string]overrideConfig, clients client.Registry) (Source, error) {
	source := Source{ID: id, Path: root}
	seenSkillDirs := make(map[string]bool)
	for _, scanRoot := range scanRoots {
		err := filepath.WalkDir(scanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}

			skillDir := filepath.Dir(path)
			if seenSkillDirs[skillDir] {
				return nil
			}
			seenSkillDirs[skillDir] = true
			relativeDir, err := filepath.Rel(root, skillDir)
			if err != nil {
				return err
			}
			relativeID := filepath.ToSlash(relativeDir)
			if relativeID == "." {
				relativeID = filepath.Base(skillDir)
			}
			skillID := id + "/" + relativeID
			frontmatter, err := readFrontmatter(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			name := frontmatter.Name
			if name == "" {
				name = filepath.Base(skillDir)
			}
			targets := cloneTargets(defaults)
			reason := ""
			if override, ok := overrides[skillID]; ok {
				targets, err = targetSet(override.Targets, clients)
				if err != nil {
					return fmt.Errorf("catalog override %s: %w", skillID, err)
				}
				reason = override.Reason
			}
			source.Skills = append(source.Skills, Skill{
				ID:                  skillID,
				Name:                name,
				Description:         frontmatter.Description,
				SourceID:            id,
				Path:                skillDir,
				Targets:             targets,
				CompatibilityReason: reason,
			})
			return nil
		})
		if err != nil {
			return Source{}, fmt.Errorf("discover source %s: %w", id, err)
		}
	}
	sort.Slice(source.Skills, func(i, j int) bool {
		return source.Skills[i].ID < source.Skills[j].ID
	})
	return source, nil
}

func loadConfig(path string) (configFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return configFile{Version: 1}, nil
	}
	if err != nil {
		return configFile{}, fmt.Errorf("read catalog config: %w", err)
	}
	var config configFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		return configFile{}, fmt.Errorf("parse catalog config: %w", err)
	}
	if config.Version != 1 {
		return configFile{}, fmt.Errorf("unsupported catalog version: %d", config.Version)
	}
	return config, nil
}

func readFrontmatter(path string) (skillFrontmatter, error) {
	file, err := os.Open(path)
	if err != nil {
		return skillFrontmatter{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return skillFrontmatter{}, errors.New("missing frontmatter opening delimiter")
	}
	var yamlLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			var frontmatter skillFrontmatter
			if err := yaml.Unmarshal([]byte(strings.Join(yamlLines, "\n")), &frontmatter); err != nil {
				return skillFrontmatter{}, fmt.Errorf("parse frontmatter: %w", err)
			}
			return frontmatter, nil
		}
		yamlLines = append(yamlLines, line)
	}
	if err := scanner.Err(); err != nil {
		return skillFrontmatter{}, err
	}
	return skillFrontmatter{}, errors.New("missing frontmatter closing delimiter")
}

func targetSet(ids []Client, registry client.Registry) (map[Client]bool, error) {
	targets := make(map[Client]bool, len(ids))
	for _, id := range ids {
		if !registry.Has(id) {
			return nil, fmt.Errorf("unknown client %q", id)
		}
		targets[id] = true
	}
	return targets, nil
}

func cloneTargets(targets map[Client]bool) map[Client]bool {
	cloned := make(map[Client]bool, len(targets))
	for client, enabled := range targets {
		cloned[client] = enabled
	}
	return cloned
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
