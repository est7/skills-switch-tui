package catalog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
	"gopkg.in/yaml.v3"

	"github.com/est7/skills-switch-tui/internal/filelock"
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
	Availability      SourceAvailability
	// AvailabilityDetail carries why a source is unavailable when the availability
	// value alone does not say. Empty for SourceAvailable.
	AvailabilityDetail string
}

type SourceAvailability string

const (
	SourceAvailable       SourceAvailability = ""
	SourceCheckoutMissing SourceAvailability = "checkout-missing"
	// SourceDiscoveryFailed marks a source whose checkout exists but could not be
	// scanned: unreadable frontmatter, an unusable manifest, or a skill name that
	// cannot be a projection path. The source keeps its identity and policy so it
	// stays listable and removable, and exposes no skills, so nothing unvalidated
	// reaches projection.
	SourceDiscoveryFailed SourceAvailability = "discovery-failed"
)

func (s Source) IsCheckoutMissing() bool {
	return s.Availability == SourceCheckoutMissing
}

// IsDiscoveryFailed reports a source whose checkout is present but could not be
// scanned. Its skills are absent from the catalog by design, so callers that
// iterate Skills need no special case; callers that report health do.
func (s Source) IsDiscoveryFailed() bool {
	return s.Availability == SourceDiscoveryFailed
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

// StagingPrefix marks a directory a source relocation is moving a checkout
// through. It is reserved: discovery skips it, and no source name can start with
// it, so a run interrupted mid-move never leaves a phantom source behind.
const StagingPrefix = ".migrating-"

// vendorSourceNamePattern accepts the scope-relative path a vendor checkout
// occupies: owner/repo, or a bare repository name for a remote with no owner
// segment and for sources registered before the owner level existed.
var vendorSourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)?$`)

type SourcePolicy struct {
	Branch            string
	SkillPaths        []string
	SparsePaths       []string
	DiscoveryPriority []DiscoveryStrategy
}

func RegisterSource(root, id string, policy SourcePolicy) error {
	configPath := filepath.Join(root, "catalog.yaml")
	return filelock.WithExclusive(configPath, func() error {
		return registerSourceLocked(root, id, policy)
	})
}

func registerSourceLocked(root, id string, policy SourcePolicy) error {
	if err := validateVendorSourceID(id); err != nil {
		return err
	}
	return mutateConfigLocked(root, func(config *configFile) error {
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
		return nil
	})
}

// mutateConfigLocked applies a mutation to catalog.yaml and replaces the file
// atomically, preserving its permissions. Callers hold the configuration lock.
func mutateConfigLocked(root string, mutate func(*configFile) error) error {
	configPath := filepath.Join(root, "catalog.yaml")
	mode, err := configPermissions(configPath)
	if err != nil {
		return err
	}
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	if config.Version == 0 {
		config.Version = 1
	}
	if err := mutate(&config); err != nil {
		return err
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
	if err := temporary.Chmod(mode); err != nil {
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

// IsSourceRegistered reports whether catalog.yaml carries a policy for id.
func IsSourceRegistered(root, id string) (bool, error) {
	if err := validateVendorSourceID(id); err != nil {
		return false, err
	}
	config, err := loadConfig(filepath.Join(root, "catalog.yaml"))
	if err != nil {
		return false, err
	}
	_, exists := config.Sources[id]
	return exists, nil
}

func UnregisterSource(root, id string) error {
	configPath := filepath.Join(root, "catalog.yaml")
	return filelock.WithExclusive(configPath, func() error {
		return unregisterSourceLocked(root, id)
	})
}

func unregisterSourceLocked(root, id string) error {
	if err := validateVendorSourceID(id); err != nil {
		return err
	}
	return mutateConfigLocked(root, func(config *configFile) error {
		if _, exists := config.Sources[id]; !exists {
			return fmt.Errorf("source policy does not exist: %s", id)
		}
		delete(config.Sources, id)
		return nil
	})
}

// RenameSource moves a vendor source's registration to a new ID, carrying the
// per-Skill overrides that are keyed by the old source ID with it. Skill IDs are
// the source ID plus the Skill's path inside the checkout, so a source that
// moves would otherwise silently drop every compatibility override its Skills
// carry.
func RenameSource(root, oldID, newID string) error {
	configPath := filepath.Join(root, "catalog.yaml")
	return filelock.WithExclusive(configPath, func() error {
		return renameSourceLocked(root, oldID, newID)
	})
}

func renameSourceLocked(root, oldID, newID string) error {
	if err := validateVendorSourceID(oldID); err != nil {
		return err
	}
	if err := validateVendorSourceID(newID); err != nil {
		return err
	}
	if oldID == newID {
		return fmt.Errorf("source policy rename needs a different id: %s", oldID)
	}
	return mutateConfigLocked(root, func(config *configFile) error {
		policy, exists := config.Sources[oldID]
		if !exists {
			return fmt.Errorf("source policy does not exist: %s", oldID)
		}
		if _, taken := config.Sources[newID]; taken {
			return fmt.Errorf("source policy already exists: %s", newID)
		}
		delete(config.Sources, oldID)
		config.Sources[newID] = policy
		// Collect before mutating: a new ID can nest under the old one — a source
		// named after what turns out to be its own owner — and a key rewritten
		// into that nest may be visited again by the same range and rewritten
		// twice.
		type renamedOverride struct {
			from, to string
			config   overrideConfig
		}
		renamed := make([]renamedOverride, 0)
		for skillID, override := range config.Overrides {
			suffix, scoped := strings.CutPrefix(skillID, oldID+"/")
			if !scoped {
				continue
			}
			renamed = append(renamed, renamedOverride{from: skillID, to: newID + "/" + suffix, config: override})
		}
		// Unregistering a source leaves its overrides behind, so the destination
		// can hold overrides with no registration to guard them. Refuse rather
		// than overwrite: both sides are user decisions about compatibility.
		// A key this same rename is moving away is not such a collision — with a
		// new ID nested under the old one, one override's destination is another
		// override's old key, and both move in this transaction.
		moving := make(map[string]bool, len(renamed))
		for _, override := range renamed {
			moving[override.from] = true
		}
		for _, override := range renamed {
			existing, taken := config.Overrides[override.to]
			if !taken || moving[override.to] || sameOverride(existing, override.config) {
				continue
			}
			return fmt.Errorf("override already exists for %s: remove it before renaming %s", override.to, oldID)
		}
		for _, override := range renamed {
			delete(config.Overrides, override.from)
		}
		for _, override := range renamed {
			config.Overrides[override.to] = override.config
		}
		return nil
	})
}

// sameOverride compares two overrides by meaning. Targets become a set at
// runtime, so neither ordering nor a repeated client changes the decision an
// override expresses.
func sameOverride(left, right overrideConfig) bool {
	if left.Reason != right.Reason {
		return false
	}
	return maps.Equal(clientSet(left.Targets), clientSet(right.Targets))
}

func clientSet(targets []Client) map[Client]bool {
	set := make(map[Client]bool, len(targets))
	for _, target := range targets {
		set[target] = true
	}
	return set
}

func configPermissions(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0o644, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat catalog config: %w", err)
	}
	return info.Mode().Perm(), nil
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

type LocalSkillTarget struct {
	ID   string
	Path string
}

// ResolveLocalSkillTarget validates a local catalog location and returns the
// canonical skill ID and directory. An empty group denotes the standalone
// group named after the skill, matching ScaffoldLocalSkill.
func ResolveLocalSkillTarget(skillsRoot, scope, group, name string) (LocalSkillTarget, error) {
	if scope == "" {
		scope = "shared"
	}
	if !skillNamePattern.MatchString(scope) {
		return LocalSkillTarget{}, fmt.Errorf("invalid scope %q", scope)
	}
	if !skillNamePattern.MatchString(name) {
		return LocalSkillTarget{}, fmt.Errorf("invalid skill name %q", name)
	}
	if group != "" && !skillNamePattern.MatchString(group) {
		return LocalSkillTarget{}, fmt.Errorf("invalid group name %q", group)
	}
	segments := []string{skillsRoot, string(SourceLocal), scope}
	groupName := group
	if group != "" {
		segments = append(segments, group)
	} else {
		groupName = name
	}
	segments = append(segments, name)
	return LocalSkillTarget{
		ID:   ScopedSourceID(SourceLocal, scope, groupName) + "/" + name,
		Path: filepath.Join(segments...),
	}, nil
}

// ScaffoldLocalSkill writes a minimal, discoverable SKILL.md skeleton for a new
// local skill and returns its directory. With an empty group the skill becomes a
// standalone group named after itself (skills/local/<scope>/<name>/SKILL.md);
// with a group it is nested (skills/local/<scope>/<group>/<name>/SKILL.md). It
// fails if a SKILL.md already exists at the target.
func ScaffoldLocalSkill(skillsRoot, scope, group, name, description string) (string, error) {
	target, err := ResolveLocalSkillTarget(skillsRoot, scope, group, name)
	if err != nil {
		return "", err
	}
	skillDir := target.Path
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
	_, _, err := parseVendorSourceID(id)
	return err
}

func parseVendorSourceID(id string) (string, string, error) {
	namespace, name, found := strings.Cut(id, "/")
	if !found || !vendorSourceNamePattern.MatchString(name) {
		return "", "", fmt.Errorf("invalid vendor source id: %s", id)
	}
	if namespace == "vendor-shared" {
		return "shared", name, nil
	}
	if strings.HasPrefix(namespace, "vendor-") && strings.HasSuffix(namespace, "-only") {
		scope := strings.TrimSuffix(strings.TrimPrefix(namespace, "vendor-"), "-only")
		if skillNamePattern.MatchString(scope) {
			return scope, name, nil
		}
	}
	return "", "", fmt.Errorf("invalid vendor source id: %s", id)
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
		defaultTargets, _ = targetSet(clients.IDsFor(client.CapabilitySkills), clients)
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
			source, discoverErr := discoverManagedSource(id, path, nil, nil, targets, overrides, clients, fallbackAlways)
			if discoverErr != nil {
				source = failedSource(id, path, discoverErr)
			}
			// An empty group is not a source; a group that failed to scan is, so it
			// stays visible and removable rather than silently disappearing.
			if len(source.Skills) > 0 || source.Availability == SourceDiscoveryFailed {
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
	discovered := make(map[string]bool)
	for _, scopeEntry := range scopeEntries {
		scope := scopeEntry.Name()
		targets, err := targetsForScope(scope, defaults, clients)
		if err != nil {
			return nil, fmt.Errorf("vendor scope: %w", err)
		}
		scopeRoot := filepath.Join(vendorRoot, scope)
		repositories, err := vendorRepositoryNames(scopeRoot, scope, config.Sources)
		if err != nil {
			return nil, err
		}
		for _, repository := range repositories {
			id := ScopedSourceID(SourceVendor, scope, repository)
			discovered[id] = true
			path := filepath.Join(scopeRoot, filepath.FromSlash(repository))
			policy := config.Sources[id]
			source, discoverErr := discoverManagedSource(id, path, policy.DiscoveryPriority, policy.SkillPaths, targets, config.Overrides, clients, fallbackWhenNoManifest)
			if discoverErr != nil {
				source = failedSource(id, path, discoverErr)
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
	for id, policy := range config.Sources {
		if discovered[id] {
			continue
		}
		scope, name, err := parseVendorSourceID(id)
		if err != nil {
			return nil, err
		}
		if _, err := targetsForScope(scope, defaults, clients); err != nil {
			return nil, fmt.Errorf("vendor source %s: %w", id, err)
		}
		branch := policy.Branch
		if branch == "" {
			branch = "main"
		}
		var discoveryPriority []DiscoveryStrategy
		if len(policy.SkillPaths) == 0 {
			discoveryPriority = normalizedDiscoveryPriority(policy.DiscoveryPriority)
		}
		sources = append(sources, Source{
			ID:                id,
			Kind:              SourceVendor,
			Scope:             scope,
			Path:              filepath.Join(vendorRoot, scope, filepath.FromSlash(name)),
			Branch:            branch,
			SkillPaths:        append([]string(nil), policy.SkillPaths...),
			SparsePaths:       append([]string(nil), policy.SparsePaths...),
			DiscoveryPriority: discoveryPriority,
			Availability:      SourceCheckoutMissing,
		})
	}
	return sources, nil
}

// vendorRepositoryNames lists the checkouts under a vendor scope root as
// scope-relative, slash-separated names. Sources are laid out as
// <scope>/<owner>/<repo> so two repositories that share a repository name stay
// distinct, while a checkout registered before the owner level existed keeps its
// bare <scope>/<repo> path.
//
// A directory directly under the scope root is a checkout rather than an owner
// when it is registered under its own name, holds a Git worktree, or has no
// subdirectory to descend into — an uninitialized submodule is an empty
// directory and must never be mistaken for an owner holding no repositories.
func vendorRepositoryNames(scopeRoot, scope string, registered map[string]sourceConfig) ([]string, error) {
	entries, err := readDirectories(scopeRoot, "vendor repositories")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if reservedVendorDirectory(entry.Name()) {
			continue
		}
		path := filepath.Join(scopeRoot, entry.Name())
		checkout, err := isVendorCheckout(path)
		if err != nil {
			return nil, err
		}
		if _, flat := registered[ScopedSourceID(SourceVendor, scope, entry.Name())]; flat || checkout {
			names = append(names, entry.Name())
			continue
		}
		children, err := readDirectories(path, "vendor repositories")
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			names = append(names, entry.Name())
			continue
		}
		for _, child := range children {
			if reservedVendorDirectory(child.Name()) {
				continue
			}
			names = append(names, entry.Name()+"/"+child.Name())
		}
	}
	return names, nil
}

// reservedVendorDirectory reports whether a directory under a vendor scope is
// bookkeeping rather than a source. A source name must start with an
// alphanumeric character, so no dot-prefixed directory can ever be one.
func reservedVendorDirectory(name string) bool {
	return strings.HasPrefix(name, ".")
}

// isVendorCheckout reports whether a directory holds a Git worktree, the marker
// every vendor submodule carries and no owner directory does.
func isVendorCheckout(path string) (bool, error) {
	_, err := os.Lstat(filepath.Join(path, ".git"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect vendor checkout %s: %w", path, err)
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
				source = failedSource(id, path, discoverErr)
			}
			// An empty group is not a source; a group that failed to scan is, so it
			// stays visible and removable rather than silently disappearing.
			if len(source.Skills) > 0 || source.Availability == SourceDiscoveryFailed {
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
	if err := clients.Require(clientID, client.CapabilitySkills); err != nil {
		return nil, err
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

// failedSource keeps a source whose scan failed addressable instead of failing
// the whole catalog. It retains the identity and path that `source list` and
// `source remove` need, and carries no skills, so a name that could not be
// validated never reaches a projection path. Errors that are not scoped to one
// source — an unparsable catalog.yaml, an unknown client, a missing root — still
// fail the load.
func failedSource(id, path string, cause error) Source {
	return Source{
		ID:                 id,
		Path:               path,
		Skills:             []Skill{},
		Availability:       SourceDiscoveryFailed,
		AvailabilityDetail: cause.Error(),
	}
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
	closed := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		yamlLines = append(yamlLines, line)
	}
	if err := scanner.Err(); err != nil {
		return skillFrontmatter{}, err
	}
	if !closed {
		return skillFrontmatter{}, errors.New("missing frontmatter closing delimiter")
	}
	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(yamlLines, "\n")), &frontmatter); err == nil {
		return frontmatter, nil
	}
	// Tolerant fallback: skill frontmatter is conventionally flat `key: value`
	// lines, but authors routinely leave an unquoted ':' inside a description,
	// which strict YAML rejects. Recover name/description from the top-level
	// scalar lines so the skill stays usable instead of failing its whole source.
	recovered := recoverFrontmatterScalars(yamlLines)
	if recovered.Name == "" && recovered.Description == "" {
		return skillFrontmatter{}, errors.New("parse frontmatter: unrecognized structure")
	}
	return recovered, nil
}

// recoverFrontmatterScalars extracts name/description from frontmatter that is
// not strictly valid YAML, reading each top-level `key: value` line and taking
// the whole remainder after the first colon (so an unquoted ':' in the value is
// preserved). Indented, comment, and list-item lines are skipped.
func recoverFrontmatterScalars(lines []string) skillFrontmatter {
	var frontmatter skillFrontmatter
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") ||
			strings.HasPrefix(strings.TrimSpace(line), "#") || strings.HasPrefix(strings.TrimSpace(line), "-") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			if frontmatter.Name == "" {
				frontmatter.Name = value
			}
		case "description":
			if frontmatter.Description == "" {
				frontmatter.Description = value
			}
		}
	}
	return frontmatter
}

func targetSet(ids []Client, registry client.Registry) (map[Client]bool, error) {
	targets := make(map[Client]bool, len(ids))
	for _, id := range ids {
		if err := registry.Require(id, client.CapabilitySkills); err != nil {
			return nil, err
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
