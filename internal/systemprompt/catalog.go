package systemprompt

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

type File struct {
	RelativePath string
	SourcePath   string
}

type Group struct {
	ID        string
	Client    client.ID
	Path      string
	Mode      client.PromptMode
	EntryFile string
	Files     []File
}

type Catalog struct {
	Root   string
	Groups []Group
	byID   map[string]Group
}

func (c Catalog) Group(id string) (Group, bool) {
	group, ok := c.byID[id]
	return group, ok
}

func Discover(root string, clients client.Registry) (Catalog, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Catalog{}, fmt.Errorf("resolve system prompt root: %w", err)
	}
	entries, err := os.ReadDir(absRoot)
	if errors.Is(err, os.ErrNotExist) {
		return Catalog{Root: absRoot, byID: make(map[string]Group)}, nil
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("read system prompt root: %w", err)
	}
	loaded := Catalog{Root: absRoot, byID: make(map[string]Group)}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		clientName, ok := strings.CutSuffix(entry.Name(), "-prompt")
		if !ok || clientName == "" {
			return Catalog{}, fmt.Errorf("system prompt directory %q must use <client>-prompt", entry.Name())
		}
		clientID := client.ID(clientName)
		if _, err := clients.UserPromptTargetDir(".", clientID); err != nil {
			return Catalog{}, fmt.Errorf("system prompt directory %q: %w", entry.Name(), err)
		}
		mode, entryFile, err := clients.UserPromptAdapter(clientID)
		if err != nil {
			return Catalog{}, fmt.Errorf("system prompt directory %q: %w", entry.Name(), err)
		}
		group, err := discoverGroup(filepath.Join(absRoot, entry.Name()), entry.Name(), clientID, mode, entryFile)
		if err != nil {
			return Catalog{}, err
		}
		loaded.Groups = append(loaded.Groups, group)
		loaded.byID[group.ID] = group
	}
	sort.Slice(loaded.Groups, func(i, j int) bool { return loaded.Groups[i].ID < loaded.Groups[j].ID })
	return loaded, nil
}

func discoverGroup(path, id string, clientID client.ID, mode client.PromptMode, entryFile string) (Group, error) {
	group := Group{ID: id, Client: clientID, Path: path, Mode: mode, EntryFile: filepath.FromSlash(entryFile)}
	err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != path && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		info, err := os.Stat(current)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("system prompt source is not a regular file: %s", current)
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		group.Files = append(group.Files, File{RelativePath: relative, SourcePath: current})
		return nil
	})
	if err != nil {
		return Group{}, fmt.Errorf("discover system prompt group %s: %w", id, err)
	}
	if len(group.Files) == 0 {
		return Group{}, fmt.Errorf("system prompt group %s has no Markdown files", id)
	}
	sort.Slice(group.Files, func(i, j int) bool { return group.Files[i].RelativePath < group.Files[j].RelativePath })
	if group.Mode == client.PromptConcat {
		foundEntry := false
		for _, file := range group.Files {
			if filepath.Clean(file.RelativePath) == filepath.Clean(group.EntryFile) {
				foundEntry = true
				break
			}
		}
		if !foundEntry {
			return Group{}, fmt.Errorf("system prompt group %s is missing concat entry source %s", id, entryFile)
		}
	}
	return group, nil
}
