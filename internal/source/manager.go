package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
)

var sourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Commander interface {
	Output(ctx context.Context, directory string, arguments ...string) ([]byte, error)
}

type GitCommander struct{}

func (GitCommander) Output(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type Manager struct {
	AgentsRoot  string
	SourcesRoot string
	Git         Commander
}

type AddRequest struct {
	Name              string
	URL               string
	Branch            string
	SparsePaths       []string
	DiscoveryPriority []catalog.DiscoveryStrategy
}

func (m Manager) Add(ctx context.Context, request AddRequest) error {
	if !sourceNamePattern.MatchString(request.Name) {
		return fmt.Errorf("invalid source name %q", request.Name)
	}
	if request.URL == "" {
		return errors.New("source URL is required")
	}
	if request.Branch == "" {
		request.Branch = "main"
	}
	if err := catalog.ValidateDiscoveryPriority(request.DiscoveryPriority); err != nil {
		return fmt.Errorf("discovery priority: %w", err)
	}
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	targetPath := filepath.Join(m.SourcesRoot, "vendor", request.Name)
	if _, err := os.Lstat(targetPath); err == nil {
		return fmt.Errorf("source path already exists: %s", targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect source path: %w", err)
	}
	if err := catalog.ValidateSourceRegistration(m.SourcesRoot, "vendor/"+request.Name); err != nil {
		return err
	}
	relativePath, err := filepath.Rel(m.AgentsRoot, targetPath)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return fmt.Errorf("sources root must be inside agents root: %s", m.SourcesRoot)
	}
	if _, err := m.Git.Output(ctx, m.AgentsRoot,
		"submodule", "add", "-b", request.Branch, request.URL, filepath.ToSlash(relativePath),
	); err != nil {
		return err
	}
	discovery, err := catalog.PlanVendorDiscovery(targetPath, request.DiscoveryPriority)
	if err != nil {
		return err
	}
	effectiveSparsePaths := mergeSparsePaths(request.SparsePaths, discovery.SparsePaths)
	if len(effectiveSparsePaths) > 0 {
		if _, err := m.Git.Output(ctx, targetPath, "sparse-checkout", "init", "--cone"); err != nil {
			return err
		}
		arguments := append([]string{"sparse-checkout", "set"}, effectiveSparsePaths...)
		if _, err := m.Git.Output(ctx, targetPath, arguments...); err != nil {
			return err
		}
	}
	return catalog.RegisterSource(m.SourcesRoot, "vendor/"+request.Name, catalog.SourcePolicy{
		Branch:            request.Branch,
		SparsePaths:       request.SparsePaths,
		DiscoveryPriority: request.DiscoveryPriority,
	})
}

func mergeSparsePaths(groups ...[]string) []string {
	unique := make(map[string]bool)
	for _, group := range groups {
		for _, path := range group {
			path = filepath.ToSlash(filepath.Clean(path))
			if path != "." && path != "" {
				unique[path] = true
			}
		}
	}
	merged := make([]string, 0, len(unique))
	for path := range unique {
		merged = append(merged, path)
	}
	sort.Strings(merged)
	return merged
}

type UpdateResult struct {
	SourceID string `json:"source"`
	Branch   string `json:"branch"`
	Current  string `json:"current"`
	Remote   string `json:"remote"`
	Changed  bool   `json:"changed"`
}

type DirtyError struct {
	SourceIDs []string
}

func (e *DirtyError) Error() string {
	return "dirty vendor sources block update: " + strings.Join(e.SourceIDs, ", ")
}

func (m Manager) Update(ctx context.Context, sources []catalog.Source, dryRun bool) ([]UpdateResult, error) {
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	results := make([]UpdateResult, 0, len(sources))
	dirty := make([]string, 0)
	for _, source := range sources {
		if source.Archived || !strings.HasPrefix(source.ID, "vendor/") {
			continue
		}
		status, err := m.Git.Output(ctx, source.Path, "status", "--porcelain")
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", source.ID, err)
		}
		if strings.TrimSpace(string(status)) != "" {
			dirty = append(dirty, source.ID)
			continue
		}
		current, err := m.Git.Output(ctx, source.Path, "rev-parse", "HEAD")
		if err != nil {
			return nil, fmt.Errorf("read %s revision: %w", source.ID, err)
		}
		remote, err := m.Git.Output(ctx, source.Path, "ls-remote", "origin", "refs/heads/"+source.Branch)
		if err != nil {
			return nil, fmt.Errorf("read %s remote revision: %w", source.ID, err)
		}
		remoteFields := strings.Fields(string(remote))
		if len(remoteFields) == 0 {
			return nil, fmt.Errorf("source %s remote branch not found: %s", source.ID, source.Branch)
		}
		currentRevision := strings.TrimSpace(string(current))
		results = append(results, UpdateResult{
			SourceID: source.ID,
			Branch:   source.Branch,
			Current:  currentRevision,
			Remote:   remoteFields[0],
			Changed:  currentRevision != remoteFields[0],
		})
	}
	if len(dirty) > 0 {
		return nil, &DirtyError{SourceIDs: dirty}
	}
	if dryRun {
		return results, nil
	}
	for _, result := range results {
		if !result.Changed {
			continue
		}
		source, ok := findSource(sources, result.SourceID)
		if !ok {
			return nil, fmt.Errorf("source disappeared during update: %s", result.SourceID)
		}
		relativePath, err := filepath.Rel(m.AgentsRoot, source.Path)
		if err != nil || strings.HasPrefix(relativePath, "..") {
			return nil, fmt.Errorf("source path is outside agents root: %s", source.Path)
		}
		if _, err := m.Git.Output(ctx, m.AgentsRoot,
			"submodule", "update", "--init", "--remote", "--", filepath.ToSlash(relativePath),
		); err != nil {
			return nil, fmt.Errorf("update %s: %w", source.ID, err)
		}
		if len(source.DiscoveryPriority) > 0 || len(source.SparsePaths) > 0 {
			if _, err := m.Git.Output(ctx, source.Path, "sparse-checkout", "disable"); err != nil {
				return nil, fmt.Errorf("expand %s sparse checkout: %w", source.ID, err)
			}
			discovery, err := catalog.PlanVendorDiscovery(source.Path, source.DiscoveryPriority)
			if err != nil {
				return nil, fmt.Errorf("recompute %s discovery: %w", source.ID, err)
			}
			effectiveSparsePaths := mergeSparsePaths(source.SparsePaths, discovery.SparsePaths)
			if len(effectiveSparsePaths) == 0 {
				continue
			}
			if _, err := m.Git.Output(ctx, source.Path, "sparse-checkout", "init", "--cone"); err != nil {
				return nil, fmt.Errorf("initialize %s sparse checkout: %w", source.ID, err)
			}
			if _, err := m.Git.Output(ctx, source.Path, append([]string{"sparse-checkout", "set"}, effectiveSparsePaths...)...); err != nil {
				return nil, fmt.Errorf("reapply %s sparse checkout: %w", source.ID, err)
			}
		}
	}
	return results, nil
}

func findSource(sources []catalog.Source, id string) (catalog.Source, bool) {
	for _, source := range sources {
		if source.ID == id {
			return source, true
		}
	}
	return catalog.Source{}, false
}
