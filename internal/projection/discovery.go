package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

// UnmanagedSkill is a real Skill directory found directly in one registered
// client's projection target. Managed and foreign symlinks are excluded
// without following them.
type UnmanagedSkill struct {
	Client catalog.Client
	Scope  Scope
	Name   string
	Path   string
}

// DiscoverUnmanagedSkills reads the requested supported projection scopes and
// reports real directories that contain SKILL.md. With no scopes it discovers
// both project and global targets. Missing targets are a valid empty state;
// other filesystem failures remain observable to the caller.
func (m Manager) DiscoverUnmanagedSkills(scopes ...Scope) ([]UnmanagedSkill, error) {
	requested, err := normalizeDiscoveryScopes(scopes)
	if err != nil {
		return nil, err
	}
	result := make([]UnmanagedSkill, 0)
	for _, clientID := range m.clients.IDsFor(client.CapabilitySkills) {
		for _, scope := range requested {
			if !m.SupportsScope(clientID, scope) {
				continue
			}
			targetDir, err := m.targetDirAt(clientID, scope)
			if err != nil {
				return nil, err
			}
			entries, err := unmanagedSkillEntries(targetDir)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				result = append(result, UnmanagedSkill{
					Client: clientID,
					Scope:  scope,
					Name:   entry,
					Path:   filepath.Join(targetDir, entry),
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Client != result[j].Client {
			return result[i].Client < result[j].Client
		}
		if result[i].Scope != result[j].Scope {
			return result[i].Scope < result[j].Scope
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func normalizeDiscoveryScopes(scopes []Scope) ([]Scope, error) {
	if len(scopes) == 0 {
		scopes = []Scope{ScopeProject, ScopeGlobal}
	}
	result := make([]Scope, 0, len(scopes))
	seen := make(map[Scope]bool, len(scopes))
	for _, scope := range scopes {
		switch scope {
		case ScopeProject, ScopeGlobal:
		default:
			return nil, fmt.Errorf("unknown skill scope %q: expected project or global", scope)
		}
		if !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	return result, nil
}

func unmanagedSkillEntries(targetDir string) ([]string, error) {
	entries, err := os.ReadDir(targetDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skills target %s: %w", targetDir, err)
	}
	result := make([]string, 0)
	for _, entry := range entries {
		path := filepath.Join(targetDir, entry.Name())
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect skill candidate %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		manifestPath := filepath.Join(path, "SKILL.md")
		manifest, err := os.Lstat(manifestPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("inspect skill manifest %s: %w", manifestPath, err)
		}
		if manifest.IsDir() {
			continue
		}
		result = append(result, entry.Name())
	}
	return result, nil
}
