package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/resource"
	"github.com/est7/skills-switch-tui/internal/source"
)

const (
	DefaultRepositoryURL = "https://github.com/est7/skills-switch-tui.git"
	BundledSourceID      = "vendor-shared/skills-switch-tui"
	BundledSkillID       = BundledSourceID + "/skills/skills-switch"
)

type Manager struct {
	ResourcesRoot string
	RepositoryURL string
	Git           source.Commander
}

type Result struct {
	ResourcesRoot string `json:"resourcesRoot"`
	SourceID      string `json:"source"`
	SkillID       string `json:"skill"`
	SourceAdded   bool   `json:"sourceAdded"`
}

func (m Manager) Initialize(ctx context.Context) (Result, error) {
	layout, err := resource.NewLayout(m.ResourcesRoot)
	if err != nil {
		return Result{}, err
	}
	if err := initializeResourceFiles(layout); err != nil {
		return Result{}, err
	}

	git := m.Git
	if git == nil {
		git = source.GitCommander{}
	}
	repositoryRoot := filepath.Dir(layout.Root)
	if err := ensureRepository(ctx, git, repositoryRoot); err != nil {
		return Result{}, err
	}

	clients, err := client.LoadRegistry(layout.RegistryFile())
	if err != nil {
		return Result{}, err
	}
	loaded, err := catalog.Load(layout.SkillsRoot(), clients)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		ResourcesRoot: layout.Root,
		SourceID:      BundledSourceID,
		SkillID:       BundledSkillID,
	}
	if _, exists := loaded.Source(BundledSourceID); exists {
		if _, skillExists := loaded.Skill(BundledSkillID); !skillExists {
			return Result{}, fmt.Errorf("bundled source %s exists but Skill %s is not discoverable", BundledSourceID, BundledSkillID)
		}
		return result, nil
	}

	repositoryURL := strings.TrimSpace(m.RepositoryURL)
	if repositoryURL == "" {
		repositoryURL = DefaultRepositoryURL
	}
	sourceManager := source.Manager{
		RepositoryRoot: repositoryRoot,
		SkillsRoot:     layout.SkillsRoot(),
		Git:            git,
		Clients:        clients,
	}
	if err := sourceManager.Add(ctx, source.AddRequest{
		Name:   "skills-switch-tui",
		URL:    repositoryURL,
		Branch: "main",
		Scope:  "shared",
	}); err != nil {
		return Result{}, fmt.Errorf("register bundled Skill source: %w", err)
	}
	result.SourceAdded = true
	return result, nil
}

func initializeResourceFiles(layout resource.Layout) error {
	directories := []string{
		filepath.Join(layout.SkillsRoot(), "local", "shared"),
		filepath.Join(layout.SkillsRoot(), "archived", "shared"),
		filepath.Join(layout.SkillsRoot(), "vendor", "shared"),
		filepath.Dir(layout.MCPCatalogFile()),
		layout.SystemPromptsRoot(),
	}
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create resource directory %s: %w", directory, err)
		}
	}
	files := []struct {
		path    string
		content string
	}{
		{path: filepath.Join(layout.SkillsRoot(), "catalog.yaml"), content: "version: 1\nsources: {}\n"},
		{path: layout.MCPCatalogFile(), content: "{\n  \"version\": 1,\n  \"mcpServers\": {}\n}\n"},
	}
	for _, file := range files {
		if err := writeFileIfAbsent(file.path, file.content); err != nil {
			return err
		}
	}
	return nil
}

func writeFileIfAbsent(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create resource file %s: %w", path, err)
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return fmt.Errorf("write resource file %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close resource file %s: %w", path, err)
	}
	return nil
}

func ensureRepository(ctx context.Context, git source.Commander, repositoryRoot string) error {
	if err := os.MkdirAll(repositoryRoot, 0o755); err != nil {
		return fmt.Errorf("create catalog repository root: %w", err)
	}
	output, err := git.Output(ctx, repositoryRoot, "rev-parse", "--show-toplevel")
	if err == nil && filepath.Clean(strings.TrimSpace(string(output))) == filepath.Clean(repositoryRoot) {
		return nil
	}
	if _, err := git.Output(ctx, repositoryRoot, "init"); err != nil {
		return fmt.Errorf("initialize catalog Git repository: %w", err)
	}
	return nil
}
