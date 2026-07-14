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
	"github.com/est7/skills-switch-tui/internal/client"
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
	RepositoryRoot string
	SkillsRoot     string
	Git            Commander
	Clients        client.Registry
}

type AddRequest struct {
	Name              string
	URL               string
	Branch            string
	Scope             string
	SkillPaths        []string
	SparsePaths       []string
	DiscoveryPriority []catalog.DiscoveryStrategy
}

func (m Manager) Add(ctx context.Context, request AddRequest) (returnErr error) {
	if !sourceNamePattern.MatchString(request.Name) {
		return fmt.Errorf("invalid source name %q", request.Name)
	}
	if request.URL == "" {
		return errors.New("source URL is required")
	}
	if request.Branch == "" {
		request.Branch = "main"
	}
	if request.Scope == "" {
		request.Scope = "shared"
	}
	if request.Scope != "shared" && !m.Clients.Has(client.ID(request.Scope)) {
		return fmt.Errorf("unknown client %q", request.Scope)
	}
	if err := catalog.ValidateDiscoveryPriority(request.DiscoveryPriority); err != nil {
		return fmt.Errorf("discovery priority: %w", err)
	}
	if len(request.SkillPaths) > 0 && len(request.DiscoveryPriority) > 0 {
		return errors.New("skill paths and discovery priority are mutually exclusive")
	}
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	repositoryRoot, err := m.repositoryRoot(ctx)
	if err != nil {
		return err
	}
	sourceID := catalog.ScopedSourceID(catalog.SourceVendor, request.Scope, request.Name)
	targetPath := filepath.Join(m.SkillsRoot, "vendor", request.Scope, request.Name)
	if _, err := os.Lstat(targetPath); err == nil {
		return fmt.Errorf("source path already exists: %s", targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect source path: %w", err)
	}
	if err := catalog.ValidateSourceRegistration(m.SkillsRoot, sourceID); err != nil {
		return err
	}
	relativePath, err := filepath.Rel(repositoryRoot, targetPath)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return fmt.Errorf("skills root must be inside repository root %s: %s", repositoryRoot, m.SkillsRoot)
	}
	if _, err := m.Git.Output(ctx, repositoryRoot,
		"submodule", "add", "-b", request.Branch, request.URL, filepath.ToSlash(relativePath),
	); err != nil {
		return err
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		rollbackCtx := context.WithoutCancel(ctx)
		if _, rollbackErr := m.Git.Output(rollbackCtx, repositoryRoot, "rm", "-f", "--", filepath.ToSlash(relativePath)); rollbackErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback added submodule: %w", rollbackErr))
		}
		// `git rm` leaves the cloned gitdir under .git/modules; remove it so a
		// later add of the same path is not refused as an existing local repo.
		m.removeSubmoduleGitdir(rollbackCtx, repositoryRoot, relativePath)
	}()
	discovery, err := catalog.PlanVendorDiscovery(targetPath, request.DiscoveryPriority, request.SkillPaths)
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
	if err := catalog.RegisterSource(m.SkillsRoot, sourceID, catalog.SourcePolicy{
		Branch:            request.Branch,
		SkillPaths:        request.SkillPaths,
		SparsePaths:       request.SparsePaths,
		DiscoveryPriority: request.DiscoveryPriority,
	}); err != nil {
		return err
	}
	completed = true
	return nil
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

func (m Manager) Remove(ctx context.Context, source catalog.Source) error {
	if source.IsArchived() || !source.IsVendor() {
		return fmt.Errorf("source %s is not a removable vendor source", source.ID)
	}
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	status, err := m.Git.Output(ctx, source.Path, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("inspect %s: %w", source.ID, err)
	}
	if strings.TrimSpace(string(status)) != "" {
		return &DirtyError{SourceIDs: []string{source.ID}}
	}
	repositoryRoot, err := m.repositoryRoot(ctx)
	if err != nil {
		return err
	}
	relativePath, err := filepath.Rel(repositoryRoot, source.Path)
	if err != nil || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source path is outside repository root %s: %s", repositoryRoot, source.Path)
	}
	if _, err := m.Git.Output(ctx, repositoryRoot, "rm", "-f", "--", filepath.ToSlash(relativePath)); err != nil {
		return fmt.Errorf("remove %s submodule: %w", source.ID, err)
	}
	if err := catalog.UnregisterSource(m.SkillsRoot, source.ID); err != nil {
		_, restoreErr := m.Git.Output(ctx, repositoryRoot, "restore", "--staged", "--worktree", "--", ".gitmodules", filepath.ToSlash(relativePath))
		if restoreErr == nil {
			_, restoreErr = m.Git.Output(ctx, repositoryRoot, "submodule", "update", "--init", "--", filepath.ToSlash(relativePath))
		}
		operationErr := fmt.Errorf("unregister %s after removing submodule: %w", source.ID, err)
		if restoreErr != nil {
			return errors.Join(operationErr, fmt.Errorf("restore removed submodule: %w", restoreErr))
		}
		return operationErr
	}
	// Drop the leftover .git/modules gitdir so re-adding the same path succeeds.
	m.removeSubmoduleGitdir(ctx, repositoryRoot, relativePath)
	return nil
}

// removeSubmoduleGitdir deletes the submodule's private gitdir under
// .git/modules, which `git rm` leaves behind. It is best-effort: a failure to
// resolve or remove it does not fail the surrounding operation.
func (m Manager) removeSubmoduleGitdir(ctx context.Context, repositoryRoot, relativePath string) {
	out, err := m.Git.Output(ctx, repositoryRoot, "rev-parse", "--git-path", "modules/"+filepath.ToSlash(relativePath))
	if err != nil {
		return
	}
	gitdir := strings.TrimSpace(string(out))
	if gitdir == "" {
		return
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(repositoryRoot, gitdir)
	}
	_ = os.RemoveAll(gitdir)
}

func (e *DirtyError) Error() string {
	return "dirty vendor sources block the operation: " + strings.Join(e.SourceIDs, ", ")
}

func (m Manager) Update(ctx context.Context, sources []catalog.Source, dryRun bool) ([]UpdateResult, error) {
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	results := make([]UpdateResult, 0, len(sources))
	dirty := make([]string, 0)
	for _, source := range sources {
		if source.IsArchived() || !source.IsVendor() {
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
		repositoryRoot, err := m.repositoryRoot(ctx)
		if err != nil {
			return nil, err
		}
		relativePath, err := filepath.Rel(repositoryRoot, source.Path)
		if err != nil || strings.HasPrefix(relativePath, "..") {
			return nil, fmt.Errorf("source path is outside repository root %s: %s", repositoryRoot, source.Path)
		}
		if _, err := m.Git.Output(ctx, repositoryRoot,
			"submodule", "update", "--init", "--remote", "--", filepath.ToSlash(relativePath),
		); err != nil {
			return nil, fmt.Errorf("update %s: %w", source.ID, err)
		}
		if len(source.DiscoveryPriority) > 0 || len(source.SkillPaths) > 0 || len(source.SparsePaths) > 0 {
			if _, err := m.Git.Output(ctx, source.Path, "sparse-checkout", "disable"); err != nil {
				return nil, fmt.Errorf("expand %s sparse checkout: %w", source.ID, err)
			}
			discovery, err := catalog.PlanVendorDiscovery(source.Path, source.DiscoveryPriority, source.SkillPaths)
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

func (m Manager) repositoryRoot(ctx context.Context) (string, error) {
	if m.RepositoryRoot != "" {
		return m.RepositoryRoot, nil
	}
	output, err := m.Git.Output(ctx, m.SkillsRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve catalog repository root: %w", err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("resolve catalog repository root: git returned an empty path")
	}
	return root, nil
}

func findSource(sources []catalog.Source, id string) (catalog.Source, bool) {
	for _, source := range sources {
		if source.ID == id {
			return source, true
		}
	}
	return catalog.Source{}, false
}
