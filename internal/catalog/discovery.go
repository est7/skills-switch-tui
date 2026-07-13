package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/est7/skills-switch-tui/internal/client"
)

type DiscoveryStrategy string

const (
	DiscoveryAgentsMarketplace DiscoveryStrategy = "agents-marketplace"
	DiscoveryClaudeMarketplace DiscoveryStrategy = "claude-marketplace"
	DiscoveryCodexPlugin       DiscoveryStrategy = "codex-plugin"
	DiscoveryClaudePlugin      DiscoveryStrategy = "claude-plugin"
	DiscoverySkillsDir         DiscoveryStrategy = "skills-dir"
)

var defaultVendorDiscoveryPriority = []DiscoveryStrategy{
	DiscoveryAgentsMarketplace,
	DiscoveryClaudeMarketplace,
	DiscoveryCodexPlugin,
	DiscoveryClaudePlugin,
	DiscoverySkillsDir,
}

type pluginManifest struct {
	Skills *[]string `json:"skills"`
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

func PlanVendorDiscovery(root string, configuredPriority []DiscoveryStrategy) (DiscoveryPlan, error) {
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

func discoverVendorSource(
	id string,
	root string,
	configuredPriority []DiscoveryStrategy,
	defaults map[Client]bool,
	overrides map[string]overrideConfig,
	clients client.Registry,
) (Source, error) {
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
		source, err := discoverSourceRoots(id, root, roots, defaults, overrides, clients)
		if err != nil {
			return Source{}, err
		}
		source.DiscoveryStrategy = strategy
		source.DiscoveryPriority = priority
		return source, nil
	}
	return Source{ID: id, Path: root, DiscoveryPriority: priority}, nil
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
	if manifest.Skills == nil {
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
	roots := make([]string, 0, len(*manifest.Skills))
	for _, declared := range *manifest.Skills {
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
	add := func(path string) error {
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relative == "." {
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

	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}
