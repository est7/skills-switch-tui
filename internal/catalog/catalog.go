package catalog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
	"gopkg.in/yaml.v3"
)

type Client = client.ID

type SourceKind string

const (
	SourceLocal    SourceKind = "local"
	SourceArchived SourceKind = "archived"
	SourceVendor   SourceKind = "vendor"
)

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
	MetadataIssue       string
}

func (s Skill) Supports(client Client) bool {
	return s.Targets[client]
}

type Source struct {
	ID                string
	Kind              SourceKind
	Scope             string
	Path              string
	Branch            string
	SkillPaths        []string
	SparsePaths       []string
	DiscoveryPriority []DiscoveryStrategy
	DiscoveryStrategy DiscoveryStrategy
	Skills            []Skill
}

func (s Source) IsArchived() bool {
	return s.Kind == SourceArchived
}

func (s Source) IsVendor() bool {
	return s.Kind == SourceVendor
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
	Defaults  targetConfig              `yaml:"defaults,omitempty"`
	Sources   map[string]sourceConfig   `yaml:"sources,omitempty"`
	Overrides map[string]overrideConfig `yaml:"overrides,omitempty"`
}

type sourceConfig struct {
	Branch            string              `yaml:"branch"`
	SkillPaths        []string            `yaml:"skillPaths,omitempty"`
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

var skillNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type SourcePolicy struct {
	Branch            string
	SkillPaths        []string
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
		SkillPaths:        append([]string(nil), policy.SkillPaths...),
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

func UnregisterSource(root, id string) error {
	if err := validateVendorSourceID(id); err != nil {
		return err
	}
	configPath := filepath.Join(root, "catalog.yaml")
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	if _, exists := config.Sources[id]; !exists {
		return fmt.Errorf("source policy does not exist: %s", id)
	}
	delete(config.Sources, id)
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode catalog config: %w", err)
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
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync catalog temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close catalog temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, configPath); err != nil {
		return fmt.Errorf("replace catalog config: %w", err)
	}
	return nil
}

// RemoveLocalResource deletes a local group or Skill directory rooted under
// <root>/local. It refuses any target outside the local tree and refuses to
// remove a scope root (e.g. local/shared), which would drop an entire client
// scope. Vendor and archived sources are managed elsewhere and are never
// removable through this path.
func RemoveLocalResource(root, target string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve sources root: %w", err)
	}
	localRoot := filepath.Join(absRoot, string(SourceLocal))
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve delete target: %w", err)
	}
	relative, err := filepath.Rel(localRoot, absTarget)
	if err != nil {
		return fmt.Errorf("resolve delete target: %w", err)
	}
	relative = filepath.ToSlash(relative)
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return fmt.Errorf("refusing to remove path outside local resources: %s", target)
	}
	if len(strings.Split(relative, "/")) < 2 {
		return fmt.Errorf("refusing to remove local scope root: %s", target)
	}
	info, err := os.Lstat(absTarget)
	if err != nil {
		return fmt.Errorf("inspect delete target: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("delete target is not a directory: %s", target)
	}
	return os.RemoveAll(absTarget)
}

// ScaffoldLocalSkill writes a minimal, discoverable SKILL.md skeleton for a new
// local skill and returns its directory. With an empty group the skill becomes a
// standalone group named after itself (skills/local/<scope>/<name>/SKILL.md);
// with a group it is nested (skills/local/<scope>/<group>/<name>/SKILL.md). It
// fails if a SKILL.md already exists at the target.
func ScaffoldLocalSkill(skillsRoot, scope, group, name, description string) (string, error) {
	if scope == "" {
		scope = "shared"
	}
	if !skillNamePattern.MatchString(scope) {
		return "", fmt.Errorf("invalid scope %q", scope)
	}
	if !skillNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid skill name %q", name)
	}
	if group != "" && !skillNamePattern.MatchString(group) {
		return "", fmt.Errorf("invalid group name %q", group)
	}
	segments := []string{skillsRoot, string(SourceLocal), scope}
	if group != "" {
		segments = append(segments, group)
	}
	segments = append(segments, name)
	skillDir := filepath.Join(segments...)
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillFile); err == nil {
		return "", fmt.Errorf("skill already exists: %s", skillFile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect skill target: %w", err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create skill directory: %w", err)
	}
	if strings.TrimSpace(description) == "" {
		description = fmt.Sprintf("Describe what %s does.", name)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n\nDocument the skill workflow here.\n", name, description, name)
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write SKILL.md: %w", err)
	}
	return skillDir, nil
}

func validateVendorSourceID(id string) error {
	namespace, name, found := strings.Cut(id, "/")
	if !found || name == "" || !strings.HasPrefix(namespace, "vendor-") {
		return fmt.Errorf("invalid vendor source id: %s", id)
	}
	return nil
}

func Load(root string, clients client.Registry) (Catalog, error) {
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
	defaultTargets, err := targetSet(config.Defaults.Targets, clients)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalog defaults: %w", err)
	}
	if len(defaultTargets) == 0 {
		defaultTargets, _ = targetSet(clients.IDs(), clients)
	}
	if err := validateNoLegacySourceRoots(absRoot); err != nil {
		return Catalog{}, err
	}

	sources := make([]Source, 0)
	localSources, err := discoverLocalSources(absRoot, defaultTargets, config.Overrides, clients)
	if err != nil {
		return Catalog{}, err
	}
	sources = append(sources, localSources...)
	vendorSources, err := discoverVendorSources(absRoot, defaultTargets, config, clients)
	if err != nil {
		return Catalog{}, err
	}
	sources = append(sources, vendorSources...)
	archivedSources, err := discoverArchivedSources(absRoot, defaultTargets, config.Overrides, clients)
	if err != nil {
		return Catalog{}, err
	}
	sources = append(sources, archivedSources...)

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

func discoverLocalSources(root string, defaults map[Client]bool, overrides map[string]overrideConfig, clients client.Registry) ([]Source, error) {
	localRoot := filepath.Join(root, string(SourceLocal))
	scopeEntries, err := readDirectories(localRoot, "local scopes")
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0)
	for _, scopeEntry := range scopeEntries {
		scope := scopeEntry.Name()
		targets, err := targetsForScope(scope, defaults, clients)
		if err != nil {
			return nil, fmt.Errorf("local scope: %w", err)
		}
		scopeRoot := filepath.Join(localRoot, scope)
		groups, err := readDirectories(scopeRoot, "local groups")
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			id := ScopedSourceID(SourceLocal, scope, group.Name())
			path := filepath.Join(scopeRoot, group.Name())
			source, discoverErr := discoverManagedSource(id, path, nil, nil, targets, overrides, clients, true, true)
			if discoverErr != nil {
				return nil, discoverErr
			}
			if len(source.Skills) > 0 {
				source.Kind = SourceLocal
				source.Scope = scope
				sources = append(sources, source)
			}
		}
	}
	return sources, nil
}

func discoverVendorSources(root string, defaults map[Client]bool, config configFile, clients client.Registry) ([]Source, error) {
	vendorRoot := filepath.Join(root, string(SourceVendor))
	scopeEntries, err := readDirectories(vendorRoot, "vendor scopes")
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0)
	for _, scopeEntry := range scopeEntries {
		scope := scopeEntry.Name()
		targets, err := targetsForScope(scope, defaults, clients)
		if err != nil {
			return nil, fmt.Errorf("vendor scope: %w", err)
		}
		scopeRoot := filepath.Join(vendorRoot, scope)
		repositories, err := readDirectories(scopeRoot, "vendor repositories")
		if err != nil {
			return nil, err
		}
		for _, repository := range repositories {
			id := ScopedSourceID(SourceVendor, scope, repository.Name())
			path := filepath.Join(scopeRoot, repository.Name())
			policy := config.Sources[id]
			source, discoverErr := discoverManagedSource(id, path, policy.DiscoveryPriority, policy.SkillPaths, targets, config.Overrides, clients, false, true)
			if discoverErr != nil {
				return nil, discoverErr
			}
			source.Kind = SourceVendor
			source.Scope = scope
			source.Branch = policy.Branch
			if source.Branch == "" {
				source.Branch = "main"
			}
			source.SparsePaths = append([]string(nil), policy.SparsePaths...)
			source.SkillPaths = append([]string(nil), policy.SkillPaths...)
			if len(policy.SkillPaths) == 0 {
				source.DiscoveryPriority = normalizedDiscoveryPriority(policy.DiscoveryPriority)
			}
			sources = append(sources, source)
		}
	}
	return sources, nil
}

func discoverArchivedSources(root string, defaults map[Client]bool, overrides map[string]overrideConfig, clients client.Registry) ([]Source, error) {
	archivedRoot := filepath.Join(root, string(SourceArchived))
	scopeEntries, err := readDirectories(archivedRoot, "archived scopes")
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0)
	for _, scopeEntry := range scopeEntries {
		scope := scopeEntry.Name()
		targets, err := targetsForScope(scope, defaults, clients)
		if err != nil {
			return nil, fmt.Errorf("archived scope: %w", err)
		}
		scopeRoot := filepath.Join(archivedRoot, scope)
		collections, err := readDirectories(scopeRoot, "archived collections")
		if err != nil {
			return nil, err
		}
		for _, collection := range collections {
			id := ScopedSourceID(SourceArchived, scope, collection.Name())
			path := filepath.Join(scopeRoot, collection.Name())
			source, discoverErr := discoverArchivedSource(id, path, targets, overrides, clients)
			if discoverErr != nil {
				return nil, discoverErr
			}
			if len(source.Skills) > 0 {
				source.Kind = SourceArchived
				source.Scope = scope
				sources = append(sources, source)
			}
		}
	}
	return sources, nil
}

func validateNoLegacySourceRoots(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read sources root: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if name == "archive" || name == "archive-raw" || strings.HasPrefix(name, "local-") || strings.HasPrefix(name, "vendor-") || strings.HasPrefix(name, "archived-") {
			return fmt.Errorf("legacy source root %q is unsupported; use the kind/scope directory matrix", name)
		}
	}
	return nil
}

func readDirectories(root, label string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	directories := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry)
		}
	}
	return directories, nil
}

func targetsForScope(scope string, defaults map[Client]bool, clients client.Registry) (map[Client]bool, error) {
	if scope == "shared" {
		return defaults, nil
	}
	clientID := Client(scope)
	if !clients.Has(clientID) {
		return nil, fmt.Errorf("unknown client %q", scope)
	}
	return map[Client]bool{clientID: true}, nil
}

func ScopedSourceID(kind SourceKind, scope, name string) string {
	var namespace string
	if scope == "shared" {
		namespace = string(kind) + "-shared"
	} else {
		namespace = string(kind) + "-" + scope + "-only"
	}
	if name == "" {
		return namespace
	}
	return namespace + "/" + name
}

func discoverArchivedSource(id, root string, defaults map[Client]bool, overrides map[string]overrideConfig, clients client.Registry) (Source, error) {
	return discoverSourceRoots(id, root, []string{root}, defaults, overrides, clients, true)
}

func discoverSourceRoots(id, root string, scanRoots []string, defaults map[Client]bool, overrides map[string]overrideConfig, clients client.Registry, tolerateMetadataIssues bool) (Source, error) {
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
			frontmatter, metadataErr := readFrontmatter(path)
			metadataIssue := ""
			if metadataErr != nil {
				if !tolerateMetadataIssues {
					return fmt.Errorf("read %s: %w", path, metadataErr)
				}
				metadataIssue = metadataErr.Error()
			}
			name := frontmatter.Name
			if name == "" {
				name = filepath.Base(skillDir)
			}
			if !skillNamePattern.MatchString(name) {
				nameErr := fmt.Errorf("invalid skill name %q", name)
				if !tolerateMetadataIssues {
					return fmt.Errorf("read %s: %w", path, nameErr)
				}
				if metadataIssue != "" {
					metadataIssue += "; "
				}
				metadataIssue += nameErr.Error()
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
				MetadataIssue:       metadataIssue,
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
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config configFile
	if err := decoder.Decode(&config); err != nil {
		return configFile{}, fmt.Errorf("parse catalog config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents")
		}
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
