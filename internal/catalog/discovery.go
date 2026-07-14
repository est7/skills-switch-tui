package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/est7/skills-switch-tui/internal/client"
)

type DiscoveryStrategy string

const (
	DiscoveryExplicit          DiscoveryStrategy = "explicit"
	DiscoveryAgentsMarketplace DiscoveryStrategy = "agents-marketplace"
	DiscoveryClaudeMarketplace DiscoveryStrategy = "claude-marketplace"
	DiscoveryCodexPlugin       DiscoveryStrategy = "codex-plugin"
	DiscoveryClaudePlugin      DiscoveryStrategy = "claude-plugin"
	DiscoverySkillsDir         DiscoveryStrategy = "skills-dir"
	// DiscoveryRootWalk is the terminal fallback used by managed sources that
	// opt into fallbackToRoot: no manifest declared discoverable skills, so the
	// source root itself is walked as the skill tree. It is not user-selectable.
	DiscoveryRootWalk DiscoveryStrategy = "root-walk"
)

var defaultVendorDiscoveryPriority = []DiscoveryStrategy{
	DiscoveryAgentsMarketplace,
	DiscoveryClaudeMarketplace,
	DiscoveryCodexPlugin,
	DiscoveryClaudePlugin,
	DiscoverySkillsDir,
}

type pluginManifest struct {
	Skills manifestSkills `json:"skills"`
}

// manifestSkills decodes a plugin manifest "skills" field, which real manifests
// write either as an array of paths (["./skills/a", ...]) or as a single string
// pointing at a skills directory ("./skills/"). An absent field (present=false)
// falls back to the conventional skills/ dir, while an explicit empty array stays
// authoritatively empty.
type manifestSkills struct {
	present bool
	paths   []string
}

func (m *manifestSkills) UnmarshalJSON(data []byte) error {
	m.present = true
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		m.paths = list
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return fmt.Errorf(`manifest "skills" must be a string or an array of strings`)
	}
	if single != "" {
		m.paths = []string{single}
	}
	return nil
}

type marketplaceManifest struct {
	Plugins []marketplacePlugin `json:"plugins"`
}

type marketplacePlugin struct {
	Name   string          `json:"name"`
	Source json.RawMessage `json:"source"`
}

type marketplaceSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type DiscoveryPlan struct {
	Strategy    DiscoveryStrategy
	SparsePaths []string
}

func normalizedDiscoveryPriority(configured []DiscoveryStrategy) []DiscoveryStrategy {
	if len(configured) == 0 {
		return append([]DiscoveryStrategy(nil), defaultVendorDiscoveryPriority...)
	}
	return append([]DiscoveryStrategy(nil), configured...)
}

func ValidateDiscoveryPriority(priority []DiscoveryStrategy) error {
	seen := make(map[DiscoveryStrategy]bool)
	for _, strategy := range priority {
		switch strategy {
		case DiscoveryAgentsMarketplace,
			DiscoveryClaudeMarketplace,
			DiscoveryCodexPlugin,
			DiscoveryClaudePlugin,
			DiscoverySkillsDir:
		default:
			return fmt.Errorf("unknown discovery strategy %q", strategy)
		}
		if seen[strategy] {
			return fmt.Errorf("duplicate discovery strategy %q", strategy)
		}
		seen[strategy] = true
	}
	return nil
}

func PlanVendorDiscovery(root string, configuredPriority []DiscoveryStrategy, skillPaths []string) (DiscoveryPlan, error) {
	if len(skillPaths) > 0 {
		if len(configuredPriority) > 0 {
			return DiscoveryPlan{}, errors.New("skillPaths and discoveryPriority are mutually exclusive")
		}
		roots, err := explicitSkillRoots(root, skillPaths)
		if err != nil {
			return DiscoveryPlan{}, fmt.Errorf("plan explicit discovery: %w", err)
		}
		paths, err := sparsePathsForDiscovery(root, DiscoveryExplicit, roots)
		if err != nil {
			return DiscoveryPlan{}, fmt.Errorf("plan sparse checkout for explicit discovery: %w", err)
		}
		return DiscoveryPlan{Strategy: DiscoveryExplicit, SparsePaths: paths}, nil
	}
	priority := normalizedDiscoveryPriority(configuredPriority)
	if err := ValidateDiscoveryPriority(priority); err != nil {
		return DiscoveryPlan{}, err
	}
	for _, strategy := range priority {
		roots, matched, err := discoveryRoots(root, strategy)
		if err != nil {
			return DiscoveryPlan{}, fmt.Errorf("plan discovery with %s: %w", strategy, err)
		}
		if !matched {
			continue
		}
		paths, err := sparsePathsForDiscovery(root, strategy, roots)
		if err != nil {
			return DiscoveryPlan{}, fmt.Errorf("plan sparse checkout for %s: %w", strategy, err)
		}
		return DiscoveryPlan{Strategy: strategy, SparsePaths: paths}, nil
	}
	return DiscoveryPlan{}, nil
}

// fallbackMode controls what happens when the manifest strategies do not yield
// skills. Both modes root-walk when no manifest matched at all (so a manifest-less
// repo of flat skills, e.g. github.com/android/skills, discovers every SKILL.md);
// they differ only on a manifest that matched but declared no skills.
type fallbackMode int

const (
	// fallbackWhenNoManifest root-walks only when no manifest matched. A manifest
	// that matched but is empty (an explicit plugin `skills: []`) stays
	// authoritatively empty. Used by vendor repositories.
	fallbackWhenNoManifest fallbackMode = iota
	// fallbackAlways additionally root-walks when a matched manifest declared no
	// skills, matching the historical "collect every SKILL.md" behavior of local
	// groups.
	fallbackAlways
)

// discoverManagedSource resolves a single managed source rooted at root using the
// shared discovery pipeline, which recognizes these layouts in priority order:
// explicit --skill-path (container-aware, via explicitSkillRoots); marketplace
// (.agents/plugins or .claude-plugin marketplace.json); plugin-dir (.codex-plugin
// or .claude-plugin plugin.json); skills-dir (a top-level skills/); and a flat
// root-walk fallback governed by fallback. Explicit --skill-path or
// --discovery-priority still scopes a repo that needs it.
func discoverManagedSource(
	id string,
	root string,
	configuredPriority []DiscoveryStrategy,
	skillPaths []string,
	defaults map[Client]bool,
	overrides map[string]overrideConfig,
	clients client.Registry,
	fallback fallbackMode,
) (Source, error) {
	if len(skillPaths) > 0 {
		if len(configuredPriority) > 0 {
			return Source{}, fmt.Errorf("discover source %s: skillPaths and discoveryPriority are mutually exclusive", id)
		}
		roots, err := explicitSkillRoots(root, skillPaths)
		if err != nil {
			return Source{}, fmt.Errorf("discover source %s with explicit paths: %w", id, err)
		}
		source, err := discoverSourceRoots(id, root, roots, defaults, overrides, clients, false)
		if err != nil {
			return Source{}, err
		}
		source.DiscoveryStrategy = DiscoveryExplicit
		return source, nil
	}
	priority := normalizedDiscoveryPriority(configuredPriority)
	if err := ValidateDiscoveryPriority(priority); err != nil {
		return Source{}, fmt.Errorf("discover source %s: %w", id, err)
	}
	for _, strategy := range priority {
		roots, matched, err := discoveryRoots(root, strategy)
		if err != nil {
			return Source{}, fmt.Errorf("discover source %s with %s: %w", id, strategy, err)
		}
		if !matched {
			continue
		}
		source, err := discoverSourceRoots(id, root, roots, defaults, overrides, clients, false)
		if err != nil {
			return Source{}, err
		}
		if fallback == fallbackAlways && len(source.Skills) == 0 {
			// A manifest matched but declared no discoverable skills; fall back to
			// treating the root itself as the skill tree. Vendor sources use
			// fallbackWhenNoManifest, so an explicit empty `skills: []` returns here
			// and stays authoritatively empty.
			break
		}
		source.DiscoveryStrategy = strategy
		source.DiscoveryPriority = priority
		return source, nil
	}
	// No manifest matched: walk the root and collect every SKILL.md.
	source, err := discoverSourceRoots(id, root, []string{root}, defaults, overrides, clients, false)
	if err != nil {
		return Source{}, err
	}
	source.DiscoveryStrategy = DiscoveryRootWalk
	return source, nil
}

func explicitSkillRoots(root string, skillPaths []string) ([]string, error) {
	roots := make([]string, 0, len(skillPaths))
	seen := make(map[string]bool)
	for _, declared := range skillPaths {
		resolved, err := resolveDeclaredPath(root, root, declared)
		if err != nil {
			return nil, fmt.Errorf("skill path %q: %w", declared, err)
		}
		// A path that directly holds SKILL.md is a single skill root. Otherwise it
		// is a container — a plugin directory or a skills/ tree, as a GitHub/GitLab
		// tree URL commonly points at — whose SKILL.md files are found by walking
		// it. Only a subtree with no SKILL.md at all is an error.
		if _, err := os.Stat(filepath.Join(resolved, "SKILL.md")); err != nil {
			if !containsSkillManifest(resolved) {
				return nil, fmt.Errorf("skill path %q contains no SKILL.md", declared)
			}
		}
		if !seen[resolved] {
			seen[resolved] = true
			roots = append(roots, resolved)
		}
	}
	return roots, nil
}

// containsSkillManifest reports whether dir holds a SKILL.md anywhere beneath it.
func containsSkillManifest(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && entry.Name() == "SKILL.md" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func discoveryRoots(root string, strategy DiscoveryStrategy) ([]string, bool, error) {
	switch strategy {
	case DiscoveryAgentsMarketplace:
		return marketplaceRoots(
			root,
			filepath.Join(root, ".agents", "plugins", "marketplace.json"),
			filepath.Join(".codex-plugin", "plugin.json"),
		)
	case DiscoveryClaudeMarketplace:
		return marketplaceRoots(
			root,
			filepath.Join(root, ".claude-plugin", "marketplace.json"),
			filepath.Join(".claude-plugin", "plugin.json"),
		)
	case DiscoveryCodexPlugin:
		return pluginManifestRoots(root, root, filepath.Join(root, ".codex-plugin", "plugin.json"))
	case DiscoveryClaudePlugin:
		return pluginManifestRoots(root, root, filepath.Join(root, ".claude-plugin", "plugin.json"))
	case DiscoverySkillsDir:
		skillsRoot := filepath.Join(root, "skills")
		if !isDirectory(skillsRoot) {
			return nil, false, nil
		}
		return []string{skillsRoot}, true, nil
	default:
		return nil, false, fmt.Errorf("unknown discovery strategy %q", strategy)
	}
}

func marketplaceRoots(sourceRoot, manifestPath, pluginManifestRelativePath string) ([]string, bool, error) {
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	var manifest marketplaceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", manifestPath, err)
	}

	roots := make([]string, 0)
	seen := make(map[string]bool)
	for _, plugin := range manifest.Plugins {
		sourcePath, err := decodeMarketplaceSource(plugin.Source)
		if err != nil {
			return nil, true, fmt.Errorf("plugin %q source: %w", plugin.Name, err)
		}
		pluginRoot, err := resolveDeclaredPath(sourceRoot, sourceRoot, sourcePath)
		if err != nil {
			return nil, true, fmt.Errorf("plugin %q source path %q: %w", plugin.Name, sourcePath, err)
		}
		pluginRoots, manifestFound, err := pluginManifestRoots(
			sourceRoot,
			pluginRoot,
			filepath.Join(pluginRoot, pluginManifestRelativePath),
		)
		if err != nil {
			return nil, true, fmt.Errorf("plugin %q: %w", plugin.Name, err)
		}
		if !manifestFound {
			defaultSkillsRoot := filepath.Join(pluginRoot, "skills")
			if isDirectory(defaultSkillsRoot) {
				pluginRoots = []string{defaultSkillsRoot}
			}
		}
		for _, root := range pluginRoots {
			if !seen[root] {
				seen[root] = true
				roots = append(roots, root)
			}
		}
	}
	return roots, true, nil
}

func decodeMarketplaceSource(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("source is missing")
	}
	var path string
	if err := json.Unmarshal(raw, &path); err == nil {
		if path == "" {
			return "", errors.New("path is empty")
		}
		return path, nil
	}
	var source marketplaceSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", fmt.Errorf("decode source: %w", err)
	}
	if source.Source != "" && source.Source != "local" {
		return "", fmt.Errorf("unsupported source type %q", source.Source)
	}
	if source.Path == "" {
		return "", errors.New("path is empty")
	}
	return source.Path, nil
}

func pluginManifestRoots(sourceRoot, pluginRoot, manifestPath string) ([]string, bool, error) {
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	var manifest pluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	if !manifest.Skills.present {
		skillsRoot := filepath.Join(pluginRoot, "skills")
		info, err := os.Stat(skillsRoot)
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil
		}
		if err != nil {
			return nil, true, fmt.Errorf("inspect default skills path: %w", err)
		}
		if !info.IsDir() {
			return nil, true, errors.New("default skills path is not a directory")
		}
		return []string{skillsRoot}, true, nil
	}
	roots := make([]string, 0, len(manifest.Skills.paths))
	for _, declared := range manifest.Skills.paths {
		resolved, err := resolveDeclaredPath(sourceRoot, pluginRoot, declared)
		if err != nil {
			return nil, true, fmt.Errorf("skill path %q: %w", declared, err)
		}
		roots = append(roots, resolved)
	}
	return roots, true, nil
}

func resolveDeclaredPath(sourceRoot, base, declared string) (string, error) {
	if declared == "" {
		return "", errors.New("path is empty")
	}
	resolved := declared
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, filepath.FromSlash(declared))
	}
	resolved = filepath.Clean(resolved)
	relative, err := filepath.Rel(sourceRoot, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("path escapes source root")
	}
	realSourceRoot, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve source root: %w", err)
	}
	realResolved, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	realRelative, err := filepath.Rel(realSourceRoot, realResolved)
	if err != nil {
		return "", err
	}
	if realRelative == ".." || len(realRelative) > 3 && realRelative[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("symlink target escapes source root")
	}
	info, err := os.Stat(realResolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return resolved, nil
}

func sparsePathsForDiscovery(sourceRoot string, strategy DiscoveryStrategy, roots []string) ([]string, error) {
	paths := make(map[string]bool)
	requiresFullRoot := false
	add := func(path string) error {
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relative == "." {
			if strategy == DiscoveryExplicit {
				requiresFullRoot = true
				return nil
			}
			return errors.New("discovery requires the entire source root")
		}
		if relative == ".." || len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator) {
			return errors.New("discovery path escapes source root")
		}
		paths[filepath.ToSlash(relative)] = true
		return nil
	}

	switch strategy {
	case DiscoveryAgentsMarketplace:
		if err := add(filepath.Join(sourceRoot, ".agents", "plugins")); err != nil {
			return nil, err
		}
	case DiscoveryClaudeMarketplace, DiscoveryClaudePlugin:
		if err := add(filepath.Join(sourceRoot, ".claude-plugin")); err != nil {
			return nil, err
		}
	case DiscoveryCodexPlugin:
		if err := add(filepath.Join(sourceRoot, ".codex-plugin")); err != nil {
			return nil, err
		}
	}

	for _, root := range roots {
		if err := add(root); err != nil {
			return nil, err
		}
		manifestDirectory := ".claude-plugin"
		if strategy == DiscoveryAgentsMarketplace {
			manifestDirectory = ".codex-plugin"
		}
		if strategy == DiscoveryAgentsMarketplace || strategy == DiscoveryClaudeMarketplace {
			for current := filepath.Dir(root); current != sourceRoot; current = filepath.Dir(current) {
				manifestPath := filepath.Join(current, manifestDirectory, "plugin.json")
				if _, err := os.Stat(manifestPath); err == nil {
					if err := add(filepath.Dir(manifestPath)); err != nil {
						return nil, err
					}
					break
				}
				if filepath.Dir(current) == current {
					break
				}
			}
		}
	}
	if requiresFullRoot {
		return nil, nil
	}

	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}
