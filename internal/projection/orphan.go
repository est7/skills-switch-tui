package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

// Orphan is a managed projection (symlink or clean managed copy) whose
// upstream provider no longer exists on disk. This is the state a project is
// left in when an upstream source drops a skill the project had enabled, or
// when a source is relocated or removed while another project is untouched.
type Orphan struct {
	Client catalog.Client
	Scope  Scope
	Name   string
	Path   string
	Target string
}

func (m Manager) OrphanedProjections(sources []catalog.Source) ([]Orphan, error) {
	return m.OrphanedProjectionsAt(sources, ScopeProject)
}

func (m Manager) OrphanedProjectionsAt(sources []catalog.Source, scope Scope) ([]Orphan, error) {
	orphans, err := m.orphanedProjectionsAt(sources, scope, false)
	if err != nil {
		return nil, err
	}
	stale, err := m.staleProjectionsAt(scope)
	if err != nil {
		return nil, err
	}
	return append(orphans, stale...), nil
}

func (m Manager) OrphanedProjectionsAfterRefreshAt(sources []catalog.Source, scope Scope) ([]Orphan, error) {
	return m.orphanedProjectionsAt(sources, scope, true)
}

// OrphanedConflict is a managed copy whose upstream provider is no longer live
// in the catalog (or whose marker is corrupt) and that was modified locally so
// PruneOrphans will not delete it automatically.
type OrphanedConflict struct {
	Client catalog.Client
	Scope  Scope
	Name   string
	Path   string
	Target string
	Cause  error
}

// OrphanedModifiedCopiesAt reports locally modified (or corrupt-marker) managed
// copies in scope whose skill name is not covered by any live skill in sources.
// Doctor uses this so modified orphaned copies stay visible as conflicts even
// though PruneOrphans refuses to delete them.
func (m Manager) OrphanedModifiedCopiesAt(sources []catalog.Source, scope Scope) ([]OrphanedConflict, error) {
	if scope == ScopeGlobal && m.userHome == "" {
		return nil, errors.New("global skill scope requires a user home")
	}
	liveNames := make(map[string]bool)
	for _, source := range sources {
		for _, skill := range source.Skills {
			liveNames[skill.Name] = true
		}
	}
	capability := client.CapabilityProjectSkills
	if scope == ScopeGlobal {
		capability = client.CapabilityGlobalSkills
	}
	conflicts := make([]OrphanedConflict, 0)
	seenDirs := make(map[string]bool)
	for _, clientID := range m.clients.IDsFor(capability) {
		targetDir, err := m.targetDirAt(clientID, scope)
		if err != nil {
			continue
		}
		dirKey := canonicalPath(targetDir)
		if seenDirs[dirKey] {
			continue
		}
		seenDirs[dirKey] = true
		entries, err := os.ReadDir(targetDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read projection directory %s: %w", targetDir, err)
		}
		for _, dirEntry := range entries {
			if isStagingEntryName(dirEntry.Name()) || liveNames[dirEntry.Name()] {
				continue
			}
			entryPath := filepath.Join(targetDir, dirEntry.Name())
			entry, err := inspectEntry(entryPath)
			if err != nil {
				return nil, fmt.Errorf("inspect projection %s: %w", entryPath, err)
			}
			if entry.kind != entryModifiedManagedCopy {
				continue
			}
			conflicts = append(conflicts, OrphanedConflict{
				Client: clientID,
				Scope:  scope,
				Name:   dirEntry.Name(),
				Path:   entryPath,
				Target: entry.resolvedTarget,
				Cause:  entry.cause,
			})
		}
	}
	return conflicts, nil
}

func (m Manager) orphanedProjectionsAt(sources []catalog.Source, scope Scope, refreshed bool) ([]Orphan, error) {
	if scope == ScopeGlobal && m.userHome == "" {
		return nil, errors.New("global skill scope requires a user home")
	}
	orphans := make([]Orphan, 0)
	for _, source := range sources {
		sourceRoot := canonicalPath(source.Path)
		if sourceRoot == "" || sourceRoot == "." {
			continue
		}
		if refreshed {
			info, err := os.Stat(sourceRoot)
			if err != nil {
				return nil, fmt.Errorf("inspect refreshed source %s at %s: %w", source.ID, sourceRoot, err)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("inspect refreshed source %s at %s: not a directory", source.ID, sourceRoot)
			}
		} else if len(source.Skills) == 0 {
			continue
		}
		live := make(map[string]bool, len(source.Skills))
		for _, skill := range source.Skills {
			live[canonicalPath(skill.Path)] = true
		}
		capability := client.CapabilityProjectSkills
		if scope == ScopeGlobal {
			capability = client.CapabilityGlobalSkills
		}
		seenDirs := make(map[string]bool)
		for _, clientID := range m.clients.IDsFor(capability) {
			targetDir, err := m.targetDirAt(clientID, scope)
			if err != nil {
				continue
			}
			dirKey := canonicalPath(targetDir)
			if seenDirs[dirKey] {
				continue
			}
			seenDirs[dirKey] = true
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
				if entry.kind != entrySymlink && entry.kind != entryManagedCopy {
					// Never treat a user-owned regular file, directory, or
					// locally modified copy as an orphan.
					continue
				}
				if !underRoot(entry.resolvedTarget, sourceRoot) {
					continue
				}
				if live[entry.resolvedTarget] {
					continue
				}
				if _, statErr := os.Stat(entry.resolvedTarget); !errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				orphans = append(orphans, Orphan{
					Client: clientID,
					Scope:  scope,
					Name:   dirEntry.Name(),
					Path:   entryPath,
					Target: entry.resolvedTarget,
				})
			}
		}
	}
	return orphans, nil
}

func (m Manager) staleProjectionsAt(scope Scope) ([]Orphan, error) {
	if scope == ScopeGlobal && m.userHome == "" {
		return nil, errors.New("global skill scope requires a user home")
	}
	capability := client.CapabilityProjectSkills
	if scope == ScopeGlobal {
		capability = client.CapabilityGlobalSkills
	}
	stale := make([]Orphan, 0)
	seenDirs := make(map[string]bool)
	for _, clientID := range m.clients.IDsFor(capability) {
		targetDir, err := m.targetDirAt(clientID, scope)
		if err != nil {
			continue
		}
		dirKey := canonicalPath(targetDir)
		if seenDirs[dirKey] {
			continue
		}
		seenDirs[dirKey] = true
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
			if entry.kind != entrySymlink && entry.kind != entryManagedCopy {
				continue
			}
			if !m.isStaleTarget(entry.resolvedTarget) {
				continue
			}
			stale = append(stale, Orphan{Client: clientID, Scope: scope, Name: dirEntry.Name(), Path: entryPath, Target: entry.resolvedTarget})
		}
	}
	return stale, nil
}

// PruneOrphans removes the given orphan symlinks and clean managed copies.
// Before removing each entry it re-verifies that the path still matches the
// exact projection detected, preserving anything modified or replaced by a
// user in the meantime.
func (m Manager) PruneOrphans(orphans []Orphan) ([]Orphan, error) {
	removed := make([]Orphan, 0, len(orphans))
	var failures []error
	for _, orphan := range orphans {
		entry, err := inspectEntry(orphan.Path)
		if err != nil {
			failures = append(failures, fmt.Errorf("inspect %s: %w", orphan.Path, err))
			continue
		}
		wantTarget := canonicalPath(orphan.Target)
		switch entry.kind {
		case entryAbsent:
			continue
		case entrySymlink:
			if entry.resolvedTarget != wantTarget {
				failures = append(failures, fmt.Errorf("preserve %s: target changed since detection", orphan.Path))
				continue
			}
			if err := os.Remove(orphan.Path); err != nil {
				failures = append(failures, fmt.Errorf("remove %s: %w", orphan.Path, err))
				continue
			}
			removed = append(removed, orphan)
		case entryManagedCopy:
			if entry.resolvedTarget != wantTarget {
				failures = append(failures, fmt.Errorf("preserve %s: target changed since detection", orphan.Path))
				continue
			}
			if err := os.RemoveAll(orphan.Path); err != nil {
				failures = append(failures, fmt.Errorf("remove %s: %w", orphan.Path, err))
				continue
			}
			removed = append(removed, orphan)
		case entryModifiedManagedCopy:
			failures = append(failures, fmt.Errorf("preserve %s: %v", orphan.Path, entry.cause))
		default:
			failures = append(failures, fmt.Errorf("preserve %s: no longer a managed projection", orphan.Path))
		}
	}
	return removed, errors.Join(failures...)
}

func underRoot(path, root string) bool {
	if path == root {
		return false
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
