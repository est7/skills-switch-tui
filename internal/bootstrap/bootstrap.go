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
	"github.com/est7/skills-switch-tui/internal/userresource"
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
	if filepath.Base(repositoryRoot) == ".agents" && filepath.Base(layout.Root) == "resources" {
		if err := ensureGitIgnoreEntry(filepath.Join(repositoryRoot, ".gitignore"), "skills/"); err != nil {
			return Result{}, err
		}
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
	for _, descriptor := range userresource.Descriptors() {
		directories = append(directories, filepath.Join(layout.UserResourceRoot(descriptor.Directory), descriptor.BootstrapScope))
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
	file, err := os.CreateTemp(filepath.Dir(path), ".resource-init-*")
	if err != nil {
		return fmt.Errorf("create temporary resource file for %s: %w", path, err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return fmt.Errorf("write resource file %s: %w", path, err)
	}
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return fmt.Errorf("set resource file permissions %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync resource file %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close resource file %s: %w", path, err)
	}
	if err := os.Link(temporaryPath, path); errors.Is(err, os.ErrExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("install resource file %s: %w", path, err)
	}
	return nil
}

func ensureGitIgnoreEntry(path, entry string) error {
	contents, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read Git ignore file %s: %w", path, err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	prefix := ""
	if len(contents) > 0 && contents[len(contents)-1] != '\n' {
		prefix = "\n"
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open Git ignore file %s: %w", path, err)
	}
	if _, err := file.WriteString(prefix + entry + "\n"); err != nil {
		file.Close()
		return fmt.Errorf("write Git ignore file %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Git ignore file %s: %w", path, err)
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
