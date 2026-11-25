package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

type Manager struct {
	projectRoot     string
	userHome        string
	skillsRoot      string
	sourceRoots     []string
	clients         client.Registry
	providersByPath map[string]catalog.Skill
	beforeAdoptLink func(path, backupPath, ssotPath string) error
}

func New(projectRoot string, loaded catalog.Catalog) Manager {
	return NewWithUserHome(projectRoot, "", loaded)
}

func NewWithUserHome(projectRoot, userHome string, loaded catalog.Catalog) Manager {
	providersByPath := make(map[string]catalog.Skill)
	sourceRoots := make([]string, 0, len(loaded.Sources))
	for _, source := range loaded.Sources {
		sourceRoots = append(sourceRoots, canonicalPath(source.Path))
		if source.IsArchived() {
			continue
		}
		for _, skill := range source.Skills {
			providersByPath[canonicalPath(skill.Path)] = skill
		}
	}
	return Manager{
		projectRoot:     projectRoot,
		userHome:        userHome,
		skillsRoot:      loaded.Root,
		sourceRoots:     sourceRoots,
		clients:         loaded.Clients,
		providersByPath: providersByPath,
	}
}

type State string

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Health is a structured diagnosis of one concrete projection scope. State is
// the stable UI/API category; Cause retains the exact filesystem failure or
// conflict explanation needed by doctor output and troubleshooting.
type Health struct {
	State  State
	Scope  Scope
	Client catalog.Client
	Path   string
	Target string
	Cause  error
}

const (
	StateDisabled            State = "disabled"
	StateEnabled             State = "enabled"
	StateIncompatible        State = "incompatible"
	StateIncompatibleEnabled State = "incompatible_enabled"
	StateConflict            State = "conflict"
	StateBroken              State = "broken"
	StateStale               State = "stale"
	StateGlobal              State = "global"
	StateDuplicate           State = "duplicate"
)

func (m Manager) State(skill catalog.Skill, clientID catalog.Client) (State, error) {
	return m.StateAt(skill, clientID, ScopeProject)
}

func (m Manager) StateAt(skill catalog.Skill, clientID catalog.Client, scope Scope) (State, error) {
	definition, _ := m.clients.Definition(clientID)
	if scope == ScopeProject && m.userHome != "" && definition.UserSkillsDir != "" {
		globalPath, err := m.TargetPathAt(skill, clientID, ScopeGlobal)
		if err != nil {
			return "", err
		}
		_, err = os.Lstat(globalPath)
		if err == nil && m.yieldsSlot(globalPath, skill) {
			// Another provider or a stale link holds the global name; this
			// skill's project scope is independent of it.
			return m.directState(skill, clientID, scope)
		}
		if err == nil {
			projectHealth, healthErr := m.HealthAt(skill, clientID, ScopeProject)
			if healthErr != nil {
				return "", healthErr
			}
			if projectHealth.State != StateDisabled && projectHealth.State != StateIncompatible {
				return StateDuplicate, nil
			}
			globalState, stateErr := m.directState(skill, clientID, ScopeGlobal)
			if stateErr != nil {
				return "", stateErr
			}
			if globalState == StateEnabled {
				return StateGlobal, nil
			}
			return globalState, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect global projection %s: %w", globalPath, err)
		}
	}
	return m.directState(skill, clientID, scope)
}

func (m Manager) directState(skill catalog.Skill, clientID catalog.Client, scope Scope) (State, error) {
	health, err := m.HealthAt(skill, clientID, scope)
	return health.State, err
}

func (m Manager) skillModeAt(clientID catalog.Client, scope Scope) client.SkillMode {
	if scope == ScopeGlobal {
		return client.SkillModeSymlink
	}
	mode, err := m.clients.ProjectSkillsMode(clientID)
	if err != nil || mode == "" {
		return client.SkillModeSymlink
	}
	return mode
}

func (m Manager) HealthAt(skill catalog.Skill, clientID catalog.Client, scope Scope) (Health, error) {
	supported := skill.Supports(clientID)
	entryPath, err := m.TargetPathAt(skill, clientID, scope)
	if err != nil {
		return Health{}, err
	}
	health := Health{Scope: scope, Client: clientID, Path: entryPath}
	entry, err := inspectEntry(entryPath)
	if err != nil {
		return Health{}, fmt.Errorf("inspect projection %s: %w", entryPath, err)
	}

	mode := m.skillModeAt(clientID, scope)
	cleanSkillPath := canonicalPath(skill.Path)

	switch entry.kind {
	case entryAbsent:
		if !supported {
			health.State = StateIncompatible
			return health, nil
		}
		health.State = StateDisabled
		return health, nil
	case entryUnmanaged:
		health.State = StateConflict
		health.Cause = entry.cause
		return health, nil
	case entryBrokenSymlink:
		health.State = StateBroken
		health.Cause = entry.cause
		return health, nil
	case entryModifiedManagedCopy:
		health.Target = entry.resolvedTarget
		health.State = StateConflict
		health.Cause = entry.cause
		return health, nil
	case entrySymlink:
		health.Target = entry.resolvedTarget
		if mode == client.SkillModeCopy {
			// Copy-mode clients (such as Antigravity in workspace scope) ignore
			// symlinks pointing outside the repository root, so a catalog-managed
			// symlink in a shared directory leaves this client disabled while
			// still surfacing unmanaged foreign symlinks as conflicts.
			if entry.resolvedTarget == cleanSkillPath || m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
				if !supported {
					health.State = StateIncompatible
					return health, nil
				}
				health.State = StateDisabled
				return health, nil
			}
			health.State = StateConflict
			health.Cause = fmt.Errorf("target %s is not the selected provider %s", health.Target, cleanSkillPath)
			return health, nil
		}
		if entry.resolvedTarget != cleanSkillPath {
			if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
				if !supported {
					health.State = StateIncompatible
					return health, nil
				}
				health.State = StateDisabled
				return health, nil
			}
			health.State = StateConflict
			health.Cause = fmt.Errorf("target %s is not the selected provider %s", health.Target, cleanSkillPath)
			return health, nil
		}
		if _, err := os.Stat(filepath.Join(entry.resolvedTarget, "SKILL.md")); err != nil {
			health.State = StateBroken
			health.Cause = fmt.Errorf("inspect provider SKILL.md: %w", err)
			return health, nil
		}
		if !supported {
			health.State = StateIncompatibleEnabled
			health.Cause = fmt.Errorf("skill does not support client %s", clientID)
			return health, nil
		}
		health.State = StateEnabled
		return health, nil
	case entryManagedCopy:
		health.Target = entry.resolvedTarget
		if entry.resolvedTarget != cleanSkillPath {
			if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
				if !supported {
					health.State = StateIncompatible
					return health, nil
				}
				health.State = StateDisabled
				return health, nil
			}
			health.State = StateConflict
			health.Cause = fmt.Errorf("target %s is not the selected provider %s", health.Target, cleanSkillPath)
			return health, nil
		}
		if _, err := os.Stat(filepath.Join(entry.resolvedTarget, "SKILL.md")); err != nil {
			health.State = StateBroken
			health.Cause = fmt.Errorf("inspect provider SKILL.md: %w", err)
			return health, nil
		}
		if !supported {
			health.State = StateIncompatibleEnabled
			health.Cause = fmt.Errorf("skill does not support client %s", clientID)
			return health, nil
		}
		sourceHash, err := hashSkillDir(cleanSkillPath)
		if err != nil {
			health.State = StateBroken
			health.Cause = fmt.Errorf("inspect provider contents: %w", err)
			return health, nil
		}
		if sourceHash != entry.contentHash {
			health.State = StateStale
			health.Cause = errors.New("managed skill copy is out of date with source; re-enable to refresh")
			return health, nil
		}
		health.State = StateEnabled
		return health, nil
	default:
		return Health{}, fmt.Errorf("unknown inspected entry kind %d at %s", entry.kind, entryPath)
	}
}

func (m Manager) TargetPath(skill catalog.Skill, clientID catalog.Client) (string, error) {
	return m.TargetPathAt(skill, clientID, ScopeProject)
}

func (m Manager) TargetPathAt(skill catalog.Skill, clientID catalog.Client, scope Scope) (string, error) {
	targetDir, err := m.targetDirAt(clientID, scope)
	if err != nil {
		return "", err
	}
	return filepath.Join(targetDir, skill.Name), nil
}

func (m Manager) targetDirAt(clientID catalog.Client, scope Scope) (string, error) {
	var targetDir string
	var err error
	if scope == ScopeGlobal {
		if m.userHome == "" {
			return "", errors.New("global skill scope requires a user home")
		}
		targetDir, err = m.clients.UserSkillsTargetDir(m.userHome, clientID)
	} else {
		targetDir, err = m.clients.TargetDir(m.projectRoot, clientID)
	}
	if err != nil {
		return "", err
	}
	return targetDir, nil
}

type Conflict struct {
	Path   string
	Reason string
}

type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	parts := make([]string, 0, len(e.Conflicts))
	for _, conflict := range e.Conflicts {
		parts = append(parts, fmt.Sprintf("%s: %s", conflict.Path, conflict.Reason))
	}
	return "projection conflicts: " + strings.Join(parts, "; ")
}

type Operation struct {
	Skills  []catalog.Skill
	Client  catalog.Client
	Enabled bool
	Scope   Scope
}

// Retirement is an applied, reversible set of source-owned projection changes.
// It exists for source lifecycle operations that must retire or retarget
// projections before a Git/catalog mutation and restore them if that later
// mutation fails. Call Commit once the outer mutation succeeds so stashed
// managed copies are discarded; Commit is idempotent and a no-op after Restore.
type Retirement struct {
	applied *appliedTransaction
}

func (r Retirement) Restore() error {
	return r.applied.Restore()
}

func (r Retirement) Commit() error {
	return r.applied.Commit()
}

// RetireSource atomically removes exact catalog-managed symlinks and copies
// pointing into source from the requested scopes. User-owned files, locally
// modified copies, projections for another provider, and absent paths are not
// part of the transaction. Copy stashes are preserved until Commit is called.
func (m Manager) RetireSource(source catalog.Source, scopes ...Scope) (Retirement, error) {
	changes := make([]slotChange, 0)
	var conflicts []Conflict
	seenDirs := make(map[string]bool)
	for _, scope := range scopes {
		for _, clientID := range m.clients.IDs() {
			if !m.SupportsScope(clientID, scope) {
				continue
			}
			targetDir, err := m.targetDirAt(clientID, scope)
			if err != nil {
				return Retirement{}, err
			}
			dirKey := canonicalPath(targetDir)
			if seenDirs[dirKey] {
				continue
			}
			seenDirs[dirKey] = true
			planned, plannedConflicts := planRetireSource(source.Skills, targetDir)
			changes = append(changes, planned...)
			conflicts = append(conflicts, plannedConflicts...)
		}
	}
	if len(conflicts) > 0 {
		return Retirement{}, &ConflictError{Conflicts: conflicts}
	}
	applied, err := executeSlotChanges(changes, true)
	if err != nil {
		return Retirement{}, err
	}
	return Retirement{applied: applied}, nil
}

// RetargetSource repoints a source's own projections after its checkout moves on
// disk, for the requested scopes. It updates both symlinks and clean managed
// copies so what a user enabled stays enabled instead of dangling.
func (m Manager) RetargetSource(source catalog.Source, newRoot string, scopes ...Scope) (Retirement, error) {
	changes := make([]slotChange, 0)
	seenDirs := make(map[string]bool)
	for _, scope := range scopes {
		for _, clientID := range m.clients.IDs() {
			if !m.SupportsScope(clientID, scope) {
				continue
			}
			targetDir, err := m.targetDirAt(clientID, scope)
			if err != nil {
				return Retirement{}, err
			}
			dirKey := canonicalPath(targetDir)
			if seenDirs[dirKey] {
				continue
			}
			seenDirs[dirKey] = true
			planned, err := planRetargetSource(source, targetDir, newRoot)
			if err != nil {
				return Retirement{}, err
			}
			changes = append(changes, planned...)
		}
	}
	applied, err := executeSlotChanges(changes, false)
	if err != nil {
		return Retirement{}, err
	}
	return Retirement{applied: applied}, nil
}

// RefreshedCopy records one clean managed copy refreshed after its source
// changed upstream.
type RefreshedCopy struct {
	Client catalog.Client `json:"client"`
	Scope  Scope          `json:"scope"`
	Name   string         `json:"name"`
	Path   string         `json:"path"`
	Source string         `json:"source"`
}

// RefreshCopies updates clean (locally unmodified) managed copies whose
// upstream provider in sources changed. User-modified copies are left alone so
// doctor and TUI continue surfacing them as conflicts without losing edits.
func (m Manager) RefreshCopies(sources []catalog.Source, scopes ...Scope) ([]RefreshedCopy, error) {
	providers := make(map[string]catalog.Skill)
	for _, source := range sources {
		for _, skill := range source.Skills {
			providers[canonicalPath(skill.Path)] = skill
		}
	}
	if len(providers) == 0 {
		return nil, nil
	}
	changes := make([]slotChange, 0)
	refreshed := make([]RefreshedCopy, 0)
	seenDirs := make(map[string]bool)
	for _, scope := range scopes {
		for _, clientID := range m.clients.IDs() {
			if !m.SupportsScope(clientID, scope) {
				continue
			}
			targetDir, err := m.targetDirAt(clientID, scope)
			if err != nil {
				return nil, err
			}
			dirKey := canonicalPath(targetDir)
			if seenDirs[dirKey] {
				continue
			}
			seenDirs[dirKey] = true
			reportClient := clientID
			if scope == ScopeProject {
				if copyPeers := m.copyModePeersForDir(targetDir); len(copyPeers) > 0 {
					reportClient = copyPeers[0]
				}
			}
			entries, err := os.ReadDir(targetDir)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read projection directory %s: %w", targetDir, err)
			}
			for _, dirEntry := range entries {
				if isStagingEntryName(dirEntry.Name()) {
					continue
				}
				entryPath := filepath.Join(targetDir, dirEntry.Name())
				entry, err := inspectEntry(entryPath)
				if err != nil {
					return nil, fmt.Errorf("inspect projection %s: %w", entryPath, err)
				}
				if entry.kind != entryManagedCopy {
					continue
				}
				skill, ok := providers[entry.resolvedTarget]
				if !ok {
					continue
				}
				sourceHash, err := hashSkillDir(skill.Path)
				if err != nil {
					return nil, fmt.Errorf("hash refreshed provider %s: %w", skill.Path, err)
				}
				if sourceHash == entry.contentHash {
					continue
				}
				changes = append(changes, slotChange{
					path:     entryPath,
					expected: slotSpec{kind: slotCopy, target: entry.resolvedTarget, hash: entry.contentHash},
					desired:  slotSpec{kind: slotCopy, target: skill.Path},
				})
				refreshed = append(refreshed, RefreshedCopy{
					Client: reportClient,
					Scope:  scope,
					Name:   dirEntry.Name(),
					Path:   entryPath,
					Source: skill.SourceID,
				})
			}
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	if _, err := executeSlotChanges(changes, false); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func planRetargetSource(source catalog.Source, targetDir, newRoot string) ([]slotChange, error) {
	sourceRoot := canonicalPath(source.Path)
	changes := make([]slotChange, 0)
	for _, skill := range source.Skills {
		skillPath := canonicalPath(skill.Path)
		relative, err := filepath.Rel(sourceRoot, skillPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("resolve skill %s against source root %s", skill.ID, sourceRoot)
		}
		entryPath := filepath.Join(targetDir, skill.Name)
		entry, err := inspectEntry(entryPath)
		if err != nil {
			return nil, fmt.Errorf("inspect projection %s: %w", entryPath, err)
		}
		switch entry.kind {
		case entrySymlink:
			if entry.resolvedTarget != skillPath {
				continue
			}
			changes = append(changes, slotChange{
				path:     entryPath,
				expected: slotSpec{kind: slotSymlink, target: entry.resolvedTarget, rawTarget: entry.rawLinkTarget},
				desired:  slotSpec{kind: slotSymlink, target: filepath.Join(newRoot, relative)},
			})
		case entryManagedCopy:
			if entry.resolvedTarget != skillPath {
				continue
			}
			changes = append(changes, slotChange{
				path:        entryPath,
				expected:    slotSpec{kind: slotCopy, target: entry.resolvedTarget, hash: entry.contentHash},
				desired:     slotSpec{kind: slotCopy, target: filepath.Join(newRoot, relative)},
				retargetTag: true,
			})
		}
	}
	return changes, nil
}

func (m Manager) SetEnabled(skills []catalog.Skill, clientID catalog.Client, enabled bool) error {
	return m.SetEnabledAt(skills, clientID, enabled, ScopeProject)
}

func (m Manager) SetEnabledAt(skills []catalog.Skill, clientID catalog.Client, enabled bool, scope Scope) error {
	return m.Apply([]Operation{{Skills: skills, Client: clientID, Enabled: enabled, Scope: scope}})
}

func (m Manager) SupportsScope(clientID catalog.Client, scope Scope) bool {
	definition, ok := m.clients.Definition(clientID)
	if !ok {
		return false
	}
	if scope == ScopeGlobal {
		return m.userHome != "" && definition.UserSkillsDir != ""
	}
	return definition.ProjectSkillsDir != ""
}

// DisableEverywhere returns the operations that retire skills from every scope
// each registered client supports, so deleting their provider leaves no
// projection behind. A client may support only one of the two scopes.
func (m Manager) DisableEverywhere(skills []catalog.Skill) []Operation {
	operations := make([]Operation, 0)
	for _, clientID := range m.clients.IDs() {
		for _, scope := range []Scope{ScopeProject, ScopeGlobal} {
			if m.SupportsScope(clientID, scope) {
				operations = append(operations, Operation{Skills: skills, Client: clientID, Enabled: false, Scope: scope})
			}
		}
	}
	return operations
}

type slotIntent struct {
	path               string
	scope              Scope
	targetDir          string
	hasEnable          bool
	enableSkill        catalog.Skill
	enableMode         client.SkillMode
	downgradeToSymlink bool

	hasDisable     bool
	disableSkills  map[string]catalog.Skill // canonical provider path -> skill
	removeSymlink  bool
	removeCopy     bool
	retireForPromo bool
}

// projectPeersSharingDir returns every registered client whose project skills
// directory resolves to the same canonical directory as targetDir, in registry
// order.
func (m Manager) projectPeersSharingDir(targetDir string) []catalog.Client {
	want := canonicalPath(targetDir)
	peers := make([]catalog.Client, 0)
	for _, id := range m.clients.IDsFor(client.CapabilityProjectSkills) {
		dir, err := m.targetDirAt(id, ScopeProject)
		if err != nil {
			continue
		}
		if canonicalPath(dir) == want {
			peers = append(peers, id)
		}
	}
	return peers
}

func (m Manager) copyModePeersForDir(targetDir string) []catalog.Client {
	peers := make([]catalog.Client, 0)
	for _, id := range m.projectPeersSharingDir(targetDir) {
		if m.skillModeAt(id, ScopeProject) == client.SkillModeCopy {
			peers = append(peers, id)
		}
	}
	return peers
}

func formatClientNames(clients []catalog.Client) string {
	names := make([]string, len(clients))
	for i, id := range clients {
		names[i] = string(id)
	}
	return strings.Join(names, ", ")
}

func formatClientFlags(clients []catalog.Client) string {
	flags := make([]string, len(clients))
	for i, id := range clients {
		flags[i] = "--client " + string(id)
	}
	return strings.Join(flags, " ")
}

func (m Manager) Apply(operations []Operation) error {
	intents := make(map[string]*slotIntent)
	conflicts := make([]Conflict, 0)

	getIntent := func(path string, scope Scope, targetDir string) *slotIntent {
		key := canonicalSlotPath(path)
		intent, ok := intents[key]
		if !ok {
			intent = &slotIntent{
				path:          filepath.Clean(path),
				scope:         scope,
				targetDir:     targetDir,
				disableSkills: make(map[string]catalog.Skill),
			}
			intents[key] = intent
		}
		return intent
	}

	for _, operation := range operations {
		scope := operation.Scope
		if scope == "" {
			scope = ScopeProject
		}
		targetDir, err := m.targetDirAt(operation.Client, scope)
		if err != nil {
			return err
		}
		mode := m.skillModeAt(operation.Client, scope)
		definition, _ := m.clients.Definition(operation.Client)

		if scope == ScopeProject && operation.Enabled && m.userHome != "" && definition.UserSkillsDir != "" {
			globalDir, globalErr := m.clients.UserSkillsTargetDir(m.userHome, operation.Client)
			if globalErr != nil {
				return globalErr
			}
			for _, skill := range operation.Skills {
				globalPath := filepath.Join(globalDir, skill.Name)
				if _, globalErr := os.Lstat(globalPath); globalErr == nil {
					if m.yieldsSlot(globalPath, skill) {
						continue
					}
					conflicts = append(conflicts, Conflict{Path: globalPath, Reason: "skill is globally configured; disable global scope first"})
				} else if !errors.Is(globalErr, os.ErrNotExist) {
					conflicts = append(conflicts, Conflict{Path: globalPath, Reason: globalErr.Error()})
				}
			}
		}

		if scope == ScopeGlobal && operation.Enabled && m.SupportsScope(operation.Client, ScopeProject) {
			projectDir, projectErr := m.clients.TargetDir(m.projectRoot, operation.Client)
			if projectErr != nil {
				return projectErr
			}
			projectMode := m.skillModeAt(operation.Client, ScopeProject)
			for _, skill := range operation.Skills {
				projectPath := filepath.Join(projectDir, skill.Name)
				intent := getIntent(projectPath, ScopeProject, projectDir)
				intent.hasDisable = true
				intent.retireForPromo = true
				intent.disableSkills[canonicalPath(skill.Path)] = skill
				if projectMode == client.SkillModeSymlink {
					intent.removeSymlink = true
				} else {
					intent.removeCopy = true
				}
			}
		}

		seenNames := make(map[string]string)
		for _, skill := range operation.Skills {
			entryPath := filepath.Join(targetDir, skill.Name)
			if previousID, exists := seenNames[skill.Name]; exists && previousID != skill.ID {
				conflicts = append(conflicts, Conflict{Path: entryPath, Reason: "multiple selected skills use the same name"})
				continue
			}
			seenNames[skill.Name] = skill.ID

			if operation.Enabled && !skill.Supports(operation.Client) {
				conflicts = append(conflicts, Conflict{Path: entryPath, Reason: "skill is incompatible with " + string(operation.Client)})
				continue
			}
			if scope == ScopeProject && operation.Enabled && mode == client.SkillModeCopy {
				// Materializing a managed copy exposes the skill to every peer
				// client reading the same project directory, so preflight both
				// peer compatibility and peer global locks up front.
				for _, peer := range m.projectPeersSharingDir(targetDir) {
					if peer == operation.Client {
						continue
					}
					if !skill.Supports(peer) {
						reason := fmt.Sprintf("project directory is shared with %s, which does not support this skill", peer)
						if m.SupportsScope(operation.Client, ScopeGlobal) {
							reason += fmt.Sprintf("; enable with --scope global --client %s instead", operation.Client)
						}
						conflicts = append(conflicts, Conflict{Path: entryPath, Reason: reason})
					}
					if m.SupportsScope(peer, ScopeGlobal) {
						peerGlobalDir, peerErr := m.clients.UserSkillsTargetDir(m.userHome, peer)
						if peerErr != nil {
							return peerErr
						}
						peerGlobalPath := filepath.Join(peerGlobalDir, skill.Name)
						if _, peerErr := os.Lstat(peerGlobalPath); peerErr == nil {
							if !m.yieldsSlot(peerGlobalPath, skill) {
								reason := fmt.Sprintf("skill is globally configured for shared-directory peer %s; disable %s global scope first", peer, peer)
								if m.SupportsScope(operation.Client, ScopeGlobal) {
									reason += fmt.Sprintf(" or enable with --scope global --client %s", operation.Client)
								}
								conflicts = append(conflicts, Conflict{Path: peerGlobalPath, Reason: reason})
							}
						} else if !errors.Is(peerErr, os.ErrNotExist) {
							conflicts = append(conflicts, Conflict{Path: peerGlobalPath, Reason: peerErr.Error()})
						}
					}
				}
			}
			if operation.Enabled {
				if info, statErr := os.Stat(filepath.Join(skill.Path, "SKILL.md")); statErr != nil || info.IsDir() {
					reason := "source SKILL.md is unavailable"
					if statErr != nil {
						reason += ": " + statErr.Error()
					}
					conflicts = append(conflicts, Conflict{Path: entryPath, Reason: reason})
					continue
				}
			}

			intent := getIntent(entryPath, scope, targetDir)
			if operation.Enabled {
				if intent.hasEnable && canonicalPath(intent.enableSkill.Path) != canonicalPath(skill.Path) {
					conflicts = append(conflicts, Conflict{Path: entryPath, Reason: "multiple selected skills use the same name"})
					continue
				}
				intent.hasEnable = true
				intent.enableSkill = skill
				if mode == client.SkillModeCopy || intent.enableMode == "" {
					intent.enableMode = mode
				}
			} else {
				intent.hasDisable = true
				intent.disableSkills[canonicalPath(skill.Path)] = skill
				if mode == client.SkillModeSymlink {
					intent.removeSymlink = true
				} else {
					intent.removeCopy = true
				}
			}
		}
	}

	paths := make([]string, 0, len(intents))
	for path := range intents {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	changes := make([]slotChange, 0, len(paths))
	for _, path := range paths {
		intent := intents[path]
		planned, intentConflicts := m.planSlotIntent(intent)
		if planned != nil {
			changes = append(changes, *planned)
		}
		conflicts = append(conflicts, intentConflicts...)
	}

	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}
	_, err := executeSlotChanges(changes, false)
	return err
}

func (m Manager) planSlotIntent(intent *slotIntent) (*slotChange, []Conflict) {
	if intent.hasEnable && intent.hasDisable {
		cleanEnable := canonicalPath(intent.enableSkill.Path)
		if _, sameSkill := intent.disableSkills[cleanEnable]; sameSkill {
			// Enabling a symlink-only client (codex) while disabling an
			// incompatible copy-mode client (agy) on the shared path is
			// achievable by downgrading any existing copy to a symlink.
			if intent.enableMode == client.SkillModeSymlink && !intent.removeSymlink {
				intent.downgradeToSymlink = true
			} else {
				return nil, []Conflict{{Path: intent.path, Reason: "operation requests both enabled and disabled on shared path"}}
			}
		}
	}

	entry, err := inspectEntry(intent.path)
	if err != nil {
		return nil, []Conflict{{Path: intent.path, Reason: err.Error()}}
	}

	if intent.hasEnable {
		return m.planEnableSlot(intent, entry)
	}
	return m.planDisableSlot(intent, entry)
}

func (m Manager) planEnableSlot(intent *slotIntent, entry inspectedEntry) (*slotChange, []Conflict) {
	skill := intent.enableSkill
	cleanSkillPath := canonicalPath(skill.Path)
	desiredSpec := slotSpec{kind: slotSymlink, target: skill.Path}
	if intent.enableMode == client.SkillModeCopy {
		desiredSpec = slotSpec{kind: slotCopy, target: skill.Path}
	}

	switch entry.kind {
	case entryAbsent:
		return &slotChange{path: intent.path, expected: slotSpec{kind: slotAbsent}, desired: desiredSpec}, nil
	case entryUnmanaged, entryModifiedManagedCopy:
		return nil, []Conflict{{Path: intent.path, Reason: entry.cause.Error()}}
	case entryBrokenSymlink:
		return nil, []Conflict{{Path: intent.path, Reason: entry.cause.Error()}}
	case entrySymlink:
		expected := slotSpec{kind: slotSymlink, target: entry.resolvedTarget, rawTarget: entry.rawLinkTarget}
		if entry.resolvedTarget == cleanSkillPath {
			if intent.enableMode == client.SkillModeCopy {
				return &slotChange{path: intent.path, expected: expected, desired: desiredSpec}, nil
			}
			return nil, nil
		}
		if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
			return &slotChange{path: intent.path, expected: expected, desired: desiredSpec}, nil
		}
		return nil, []Conflict{{Path: intent.path, Reason: "symlink is not managed by this skill"}}
	case entryManagedCopy:
		expected := slotSpec{kind: slotCopy, target: entry.resolvedTarget, hash: entry.contentHash}
		if entry.resolvedTarget == cleanSkillPath {
			if intent.downgradeToSymlink {
				return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotSymlink, target: skill.Path}}, nil
			}
			sourceHash, err := hashSkillDir(cleanSkillPath)
			if err != nil {
				return nil, []Conflict{{Path: intent.path, Reason: "hash source skill: " + err.Error()}}
			}
			if sourceHash != entry.contentHash {
				return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotCopy, target: skill.Path}}, nil
			}
			// Preserve an already-fresh managed copy when enabling a symlink
			// client that shares the same directory (codex + agy).
			return nil, nil
		}
		if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
			return &slotChange{path: intent.path, expected: expected, desired: desiredSpec}, nil
		}
		return nil, []Conflict{{Path: intent.path, Reason: "managed copy is not managed by this skill"}}
	default:
		return nil, []Conflict{{Path: intent.path, Reason: "unsupported projection entry"}}
	}
}

func (m Manager) planDisableSlot(intent *slotIntent, entry inspectedEntry) (*slotChange, []Conflict) {
	switch entry.kind {
	case entryAbsent:
		return nil, nil
	case entryUnmanaged:
		if intent.retireForPromo {
			return nil, []Conflict{{Path: intent.path, Reason: "project skill blocks global promotion and is not a managed symlink"}}
		}
		return nil, []Conflict{{Path: intent.path, Reason: entry.cause.Error()}}
	case entryModifiedManagedCopy:
		return nil, []Conflict{{Path: intent.path, Reason: entry.cause.Error()}}
	case entryBrokenSymlink:
		return nil, []Conflict{{Path: intent.path, Reason: entry.cause.Error()}}
	case entrySymlink:
		expected := slotSpec{kind: slotSymlink, target: entry.resolvedTarget, rawTarget: entry.rawLinkTarget}
		if !intent.removeSymlink {
			// Copy-mode disable (e.g., agy) must not delete a symlink owned by a
			// symlink-mode client (e.g., codex) sharing .agents/skills.
			if _, matched := intent.disableSkills[entry.resolvedTarget]; matched {
				return nil, nil
			}
			for _, skill := range intent.disableSkills {
				if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
					return nil, nil
				}
			}
			if intent.retireForPromo {
				return nil, []Conflict{{Path: intent.path, Reason: "project skill blocks global promotion and is not catalog-managed"}}
			}
			return nil, []Conflict{{Path: intent.path, Reason: "symlink is not managed by this skill"}}
		}
		if intent.retireForPromo {
			if m.isStaleTarget(entry.resolvedTarget) {
				return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotAbsent}}, nil
			}
			provider, managed := m.providersByPath[entry.resolvedTarget]
			if !managed || !matchesAnySkillName(intent.disableSkills, provider.Name) {
				return nil, []Conflict{{Path: intent.path, Reason: "project skill blocks global promotion and is not catalog-managed"}}
			}
			if _, matched := intent.disableSkills[canonicalPath(provider.Path)]; !matched {
				// Another provider owns the project slot; leave it untouched.
				return nil, nil
			}
			return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotAbsent}}, nil
		}
		if _, matched := intent.disableSkills[entry.resolvedTarget]; matched {
			return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotAbsent}}, nil
		}
		for _, skill := range intent.disableSkills {
			if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
				return nil, nil
			}
		}
		return nil, []Conflict{{Path: intent.path, Reason: "symlink is not managed by this skill"}}
	case entryManagedCopy:
		expected := slotSpec{kind: slotCopy, target: entry.resolvedTarget, hash: entry.contentHash}
		checkSharedCopyPeer := func() []Conflict {
			if intent.scope != ScopeProject || intent.removeCopy {
				return nil
			}
			copyPeers := m.copyModePeersForDir(intent.targetDir)
			if len(copyPeers) == 0 {
				return nil
			}
			peerNames := formatClientNames(copyPeers)
			if intent.retireForPromo {
				return []Conflict{{
					Path:   intent.path,
					Reason: fmt.Sprintf("managed copy is shared with %s; promote or disable %s together to remove it", peerNames, peerNames),
				}}
			}
			return []Conflict{{
				Path:   intent.path,
				Reason: fmt.Sprintf("managed copy is shared with %s; include %s to remove it", peerNames, formatClientFlags(copyPeers)),
			}}
		}
		if intent.retireForPromo {
			if m.isStaleTarget(entry.resolvedTarget) {
				return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotAbsent}}, nil
			}
			provider, managed := m.providersByPath[entry.resolvedTarget]
			if !managed || !matchesAnySkillName(intent.disableSkills, provider.Name) {
				return nil, []Conflict{{Path: intent.path, Reason: "project skill blocks global promotion and is not catalog-managed"}}
			}
			if _, matched := intent.disableSkills[canonicalPath(provider.Path)]; !matched {
				return nil, nil
			}
			if peerConflicts := checkSharedCopyPeer(); len(peerConflicts) > 0 {
				return nil, peerConflicts
			}
			return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotAbsent}}, nil
		}
		if _, matched := intent.disableSkills[entry.resolvedTarget]; matched {
			if peerConflicts := checkSharedCopyPeer(); len(peerConflicts) > 0 {
				return nil, peerConflicts
			}
			return &slotChange{path: intent.path, expected: expected, desired: slotSpec{kind: slotAbsent}}, nil
		}
		for _, skill := range intent.disableSkills {
			if m.isManagedAlternative(entry.resolvedTarget, skill) || m.isStaleTarget(entry.resolvedTarget) {
				return nil, nil
			}
		}
		return nil, []Conflict{{Path: intent.path, Reason: "managed copy is not managed by this skill"}}
	default:
		return nil, []Conflict{{Path: intent.path, Reason: "unsupported projection entry"}}
	}
}

func matchesAnySkillName(skills map[string]catalog.Skill, name string) bool {
	for _, skill := range skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func planRetireSource(skills []catalog.Skill, targetDir string) ([]slotChange, []Conflict) {
	changes := make([]slotChange, 0)
	conflicts := make([]Conflict, 0)
	for _, skill := range skills {
		entryPath := filepath.Join(targetDir, skill.Name)
		entry, err := inspectEntry(entryPath)
		if err != nil {
			conflicts = append(conflicts, Conflict{Path: entryPath, Reason: err.Error()})
			continue
		}
		cleanSkillPath := canonicalPath(skill.Path)
		switch entry.kind {
		case entrySymlink:
			if entry.resolvedTarget != cleanSkillPath {
				continue
			}
			changes = append(changes, slotChange{
				path:     entryPath,
				expected: slotSpec{kind: slotSymlink, target: entry.resolvedTarget, rawTarget: entry.rawLinkTarget},
				desired:  slotSpec{kind: slotAbsent},
			})
		case entryManagedCopy:
			if entry.resolvedTarget != cleanSkillPath {
				continue
			}
			changes = append(changes, slotChange{
				path:     entryPath,
				expected: slotSpec{kind: slotCopy, target: entry.resolvedTarget, hash: entry.contentHash},
				desired:  slotSpec{kind: slotAbsent},
			})
		}
	}
	return changes, conflicts
}

// yieldsSlot reports whether path is a managed projection that occupies
// selected's name without being selected's projection and without blocking it:
// a projection to a catalog-managed alternative provider, or a stale target.
func (m Manager) yieldsSlot(path string, selected catalog.Skill) bool {
	entry, err := inspectEntry(path)
	if err != nil {
		return false
	}
	if entry.kind != entrySymlink && entry.kind != entryManagedCopy {
		return false
	}
	return m.isManagedAlternative(entry.resolvedTarget, selected) || m.isStaleTarget(entry.resolvedTarget)
}

func (m Manager) isManagedAlternative(target string, selected catalog.Skill) bool {
	provider, ok := m.providersByPath[canonicalPath(target)]
	return ok && provider.Name == selected.Name && canonicalPath(provider.Path) != canonicalPath(selected.Path)
}

// isStaleTarget reports whether target is what a managed projection points at
// after its provider left the catalog: it lies under the resources skills root,
// outside every registered source, and nothing exists there.
func (m Manager) isStaleTarget(target string) bool {
	target = canonicalPath(target)
	if m.skillsRoot == "" || !underRoot(target, canonicalPath(m.skillsRoot)) {
		return false
	}
	for _, root := range m.sourceRoots {
		if target == root || underRoot(target, root) {
			return false
		}
	}
	_, err := os.Lstat(target)
	return errors.Is(err, os.ErrNotExist)
}
