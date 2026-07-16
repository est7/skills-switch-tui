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

// Orphan is a managed projection symlink that dangles: it points into a source's
// directory at a target that no longer exists on disk. This is the state a
// project is left in when an upstream source drops a skill the project had
// enabled. A link whose target still exists but is merely no longer discovered
// (for example after a discovery scope was narrowed) is deliberately not an
// orphan — that is a scan/config concern, and removing such a link would delete
// a projection whose skill is still present.
type Orphan struct {
	Client catalog.Client
	Scope  Scope
	Name   string
	Path   string
	Target string
}

// OrphanedProjections reports managed symlinks in the project that point into
// one of the given sources but dangle — their target no longer exists on disk,
// the state left behind when an upstream source drops a skill the project had
// enabled. A link whose target still exists is never reported, even if the
// source no longer discovers it, so narrowing a discovery scope can never cause
// a still-present skill's projection to be removed.
//
// Detection is scoped to each source's own directory by matching a symlink's
// recorded target against source.Path, not by resolving it on disk. A deleted
// target still dangles at its old path, so string-prefix attribution keeps it
// bound to the source that used to own it; links belonging to other sources (or
// to nothing under a source) are never considered.
//
// A source with no discovered skills is skipped entirely. An empty source means
// "unavailable / not checked out" — for example an uninitialized submodule —
// never "every skill was removed", so a missing checkout can never be mistaken
// for a wholesale upstream deletion and trigger mass cleanup. Callers that want
// to clean after an update pass only the sources they just refreshed, which are
// known to be present.
func (m Manager) OrphanedProjections(sources []catalog.Source) ([]Orphan, error) {
	return m.OrphanedProjectionsAt(sources, ScopeProject)
}

func (m Manager) OrphanedProjectionsAt(sources []catalog.Source, scope Scope) ([]Orphan, error) {
	return m.orphanedProjectionsAt(sources, scope, false)
}

// OrphanedProjectionsAfterRefreshAt is the update-only variant of orphan
// detection. A source that was successfully refreshed is known to be available,
// so zero discovered Skills means the upstream repository genuinely removed its
// last Skill and dangling projections may be retired. The checkout directory is
// revalidated before scanning to keep a missing submodule from becoming a mass
// deletion signal.
func (m Manager) OrphanedProjectionsAfterRefreshAt(sources []catalog.Source, scope Scope) ([]Orphan, error) {
	return m.orphanedProjectionsAt(sources, scope, true)
}

func (m Manager) orphanedProjectionsAt(sources []catalog.Source, scope Scope, refreshed bool) ([]Orphan, error) {
	if scope == ScopeGlobal && m.userHome == "" {
		return nil, errors.New("global skill scope requires a user home")
	}
	orphans := make([]Orphan, 0)
	for _, source := range sources {
		sourceRoot := filepath.Clean(source.Path)
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
			live[filepath.Clean(skill.Path)] = true
		}
		capability := client.CapabilityProjectSkills
		if scope == ScopeGlobal {
			capability = client.CapabilityGlobalSkills
		}
		for _, clientID := range m.clients.IDsFor(capability) {
			var targetDir string
			var err error
			if scope == ScopeGlobal {
				targetDir, err = m.clients.UserSkillsTargetDir(m.userHome, clientID)
			} else {
				targetDir, err = m.clients.TargetDir(m.projectRoot, clientID)
			}
			if err != nil {
				// Client declares no project skills directory; it cannot hold
				// projections to orphan.
				continue
			}
			entries, err := os.ReadDir(targetDir)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read projection directory %s: %w", targetDir, err)
			}
			for _, entry := range entries {
				linkPath := filepath.Join(targetDir, entry.Name())
				info, err := os.Lstat(linkPath)
				if err != nil {
					return nil, fmt.Errorf("inspect projection %s: %w", linkPath, err)
				}
				if info.Mode()&os.ModeSymlink == 0 {
					// Never treat a user-owned regular file or directory as a
					// managed projection.
					continue
				}
				target, err := os.Readlink(linkPath)
				if err != nil {
					return nil, fmt.Errorf("read projection %s: %w", linkPath, err)
				}
				resolved := target
				if !filepath.IsAbs(resolved) {
					resolved = filepath.Join(targetDir, resolved)
				}
				resolved = filepath.Clean(resolved)
				if !underRoot(resolved, sourceRoot) {
					continue
				}
				if live[resolved] {
					continue
				}
				if _, statErr := os.Stat(resolved); !errors.Is(statErr, os.ErrNotExist) {
					// Target still exists on disk (or its state is uncertain):
					// the skill was not removed upstream, so this link is not an
					// orphan and must never be auto-removed.
					continue
				}
				orphans = append(orphans, Orphan{
					Client: clientID,
					Scope:  scope,
					Name:   entry.Name(),
					Path:   linkPath,
					Target: resolved,
				})
			}
		}
	}
	return orphans, nil
}

// PruneOrphans removes the given orphan symlinks. Before unlinking each one it
// re-verifies the path is still the exact symlink that was detected, pointing at
// the same target, so a link a user replaced between detection and removal is
// preserved — the same preflight discipline Apply uses. Removal of independent
// links is best-effort: a failure or a concurrently changed link is collected
// and reported without aborting the rest, and the returned slice lists exactly
// the links removed.
func (m Manager) PruneOrphans(orphans []Orphan) ([]Orphan, error) {
	removed := make([]Orphan, 0, len(orphans))
	var failures []error
	for _, orphan := range orphans {
		info, err := os.Lstat(orphan.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("inspect %s: %w", orphan.Path, err))
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			failures = append(failures, fmt.Errorf("preserve %s: no longer a symlink", orphan.Path))
			continue
		}
		target, err := os.Readlink(orphan.Path)
		if err != nil {
			failures = append(failures, fmt.Errorf("read %s: %w", orphan.Path, err))
			continue
		}
		resolved := target
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(orphan.Path), resolved)
		}
		if filepath.Clean(resolved) != orphan.Target {
			failures = append(failures, fmt.Errorf("preserve %s: target changed since detection", orphan.Path))
			continue
		}
		if err := os.Remove(orphan.Path); err != nil {
			failures = append(failures, fmt.Errorf("remove %s: %w", orphan.Path, err))
			continue
		}
		removed = append(removed, orphan)
	}
	return removed, errors.Join(failures...)
}

// underRoot reports whether path is a strict descendant of root. The source root
// itself is not a projection target, so an exact match is excluded.
func underRoot(path, root string) bool {
	if path == root {
		return false
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
