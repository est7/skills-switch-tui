package userresource

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/client"
)

type Kind string

const (
	KindCommand     Kind = "command"
	KindHook        Kind = "hook"
	KindAgent       Kind = "agent"
	KindOutputStyle Kind = "output-style"
)

type Resource struct {
	ID           string
	Kind         Kind
	Scope        string
	RelativePath string
	SourcePath   string
}

func (r Resource) Supports(clientID client.ID) bool {
	return r.Scope == "shared" || r.Scope == string(clientID)
}

type Catalog struct {
	Root      string
	Kind      Kind
	Resources []Resource
	byID      map[string]Resource
}

func (c Catalog) Resource(id string) (Resource, bool) {
	resource, ok := c.byID[id]
	return resource, ok
}

func Discover(root string, kind Kind, clients client.Registry) (Catalog, error) {
	if _, err := Describe(kind); err != nil {
		return Catalog{}, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Catalog{}, fmt.Errorf("resolve %s root: %w", kind, err)
	}
	entries, err := os.ReadDir(absRoot)
	if errors.Is(err, os.ErrNotExist) {
		return Catalog{Root: absRoot, Kind: kind, byID: make(map[string]Resource)}, nil
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("read %s root: %w", kind, err)
	}
	loaded := Catalog{Root: absRoot, Kind: kind, byID: make(map[string]Resource)}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() {
			return Catalog{}, fmt.Errorf("%s root entry %q must be a scope directory", kind, entry.Name())
		}
		physicalScope := entry.Name()
		scope := physicalScope
		if physicalScope != "shared" {
			clientName, ok := strings.CutSuffix(physicalScope, "-only")
			if !ok || clientName == "" {
				return Catalog{}, fmt.Errorf("%s scope %q must be shared or <client>-only", kind, physicalScope)
			}
			scope = clientName
			clientID := client.ID(clientName)
			if !clients.Has(clientID) {
				return Catalog{}, fmt.Errorf("%s scope %q names an unregistered client", kind, physicalScope)
			}
			if _, err := targetRoot(clients, ".", kind, clientID); err != nil {
				return Catalog{}, fmt.Errorf("%s scope %q: %w", kind, physicalScope, err)
			}
		}
		if err := discoverScope(&loaded, filepath.Join(absRoot, physicalScope), scope); err != nil {
			return Catalog{}, err
		}
	}
	sort.Slice(loaded.Resources, func(i, j int) bool { return loaded.Resources[i].ID < loaded.Resources[j].ID })
	return loaded, nil
}

func discoverScope(catalog *Catalog, scopeRoot, scope string) error {
	return filepath.WalkDir(scopeRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != scopeRoot && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") || entry.Name() == ".gitkeep" {
			return nil
		}
		info, err := os.Stat(current)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s source is not a regular file: %s", catalog.Kind, current)
		}
		relative, err := filepath.Rel(scopeRoot, current)
		if err != nil {
			return err
		}
		logicalScope := scope
		if scope != "shared" {
			logicalScope += "-only"
		}
		id := logicalScope + "/" + filepath.ToSlash(relative)
		if _, exists := catalog.byID[id]; exists {
			return fmt.Errorf("duplicate %s id %s", catalog.Kind, id)
		}
		resource := Resource{
			ID:           id,
			Kind:         catalog.Kind,
			Scope:        scope,
			RelativePath: relative,
			SourcePath:   current,
		}
		catalog.Resources = append(catalog.Resources, resource)
		catalog.byID[id] = resource
		return nil
	})
}

func targetRoot(clients client.Registry, userHome string, kind Kind, clientID client.ID) (string, error) {
	descriptor, err := Describe(kind)
	if err != nil {
		return "", err
	}
	return descriptor.TargetDir(clients, userHome, clientID)
}
