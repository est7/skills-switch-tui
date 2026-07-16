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
	if request.Scope != "shared" {
		if err := m.Clients.Require(client.ID(request.Scope), client.CapabilitySkills); err != nil {
			return err
		}
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
		if cleanupErr := m.removeSubmoduleGitdir(rollbackCtx, repositoryRoot, relativePath); cleanupErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback submodule gitdir: %w", cleanupErr))
		}
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
	Sources   []DirtySource
}

type DirtySource struct {
	SourceID string
	Path     string
	Status   string
}

type SourceError struct {
	SourceID  string
	Path      string
	Operation string
	Err       error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("%s (%s): %s: %v", e.SourceID, e.Path, e.Operation, e.Err)
}

func (e *SourceError) Unwrap() error {
	return e.Err
}

func sourceError(source catalog.Source, operation string, err error) error {
	return &SourceError{SourceID: source.ID, Path: source.Path, Operation: operation, Err: err}
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
		return newDirtyError([]DirtySource{{SourceID: source.ID, Path: source.Path, Status: string(status)}})
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
	if err := m.removeSubmoduleGitdir(ctx, repositoryRoot, relativePath); err != nil {
		return fmt.Errorf("remove %s submodule gitdir: %w", source.ID, err)
	}
	return nil
}

// removeSubmoduleGitdir deletes only a strict descendant of the repository's
// common Git modules directory. Git-provided paths are treated as untrusted
// filesystem input and cleanup failures remain observable to the caller.
func (m Manager) removeSubmoduleGitdir(ctx context.Context, repositoryRoot, relativePath string) error {
	out, err := m.Git.Output(ctx, repositoryRoot, "rev-parse", "--git-path", "modules/"+filepath.ToSlash(relativePath))
	if err != nil {
		return fmt.Errorf("resolve submodule gitdir: %w", err)
	}
	gitdir := strings.TrimSpace(string(out))
	if gitdir == "" {
		return nil
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(repositoryRoot, gitdir)
	}
	commonOut, err := m.Git.Output(ctx, repositoryRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve Git common directory: %w", err)
	}
	commonDir := strings.TrimSpace(string(commonOut))
	if commonDir == "" {
		return errors.New("resolve Git common directory: empty path")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repositoryRoot, commonDir)
	}
	modulesRoot := filepath.Clean(filepath.Join(commonDir, "modules"))
	gitdir = filepath.Clean(gitdir)
	relative, err := filepath.Rel(modulesRoot, gitdir)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse submodule gitdir outside %s: %s", modulesRoot, gitdir)
	}
	if err := os.RemoveAll(gitdir); err != nil {
		return fmt.Errorf("delete submodule gitdir %s: %w", gitdir, err)
	}
	return nil
}

func (e *DirtyError) Error() string {
	if len(e.Sources) > 0 {
		messages := make([]string, 0, len(e.Sources))
		for _, source := range e.Sources {
			messages = append(messages, fmt.Sprintf("%s (%s): working tree has local changes: %s",
				source.SourceID, source.Path, formatGitStatus(source.Status)))
		}
		return strings.Join(messages, "; ")
	}
	return "dirty vendor sources block the operation: " + strings.Join(e.SourceIDs, ", ")
}

func newDirtyError(sources []DirtySource) *DirtyError {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.SourceID)
	}
	return &DirtyError{SourceIDs: ids, Sources: sources}
}

func formatGitStatus(status string) string {
	lines := strings.Split(strings.TrimSpace(status), "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	return strings.Join(lines, "; ")
}

type updatePlan struct {
	source catalog.Source
	result UpdateResult
}

func (m Manager) Update(ctx context.Context, sources []catalog.Source, dryRun bool) ([]UpdateResult, error) {
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	plans := make([]updatePlan, 0, len(sources))
	updateErrors := make([]error, 0)
	for _, source := range sources {
		if source.IsArchived() || !source.IsVendor() {
			continue
		}
		if !dryRun {
			if source.IsCheckoutMissing() {
				if err := m.initializeSubmodule(ctx, source); err != nil {
					updateErrors = append(updateErrors, err)
					continue
				}
				source.Availability = catalog.SourceAvailable
			}
			if _, err := m.Git.Output(ctx, source.Path, "reset", "--hard", "HEAD"); err != nil {
				updateErrors = append(updateErrors, sourceError(source, "reset read-only checkout", err))
				continue
			}
			// Vendor sources are immutable inputs. Remove every untracked and
			// ignored path as well as tracked edits so discovery can never ingest
			// local files that are absent from the remote repository.
			if _, err := m.Git.Output(ctx, source.Path, "clean", "-ffdx"); err != nil {
				updateErrors = append(updateErrors, sourceError(source, "clean read-only checkout", err))
				continue
			}
		}
		current, err := m.Git.Output(ctx, source.Path, "rev-parse", "HEAD")
		if err != nil {
			updateErrors = append(updateErrors, sourceError(source, "read current revision", err))
			continue
		}
		remote, err := m.Git.Output(ctx, source.Path, "ls-remote", "origin", "refs/heads/"+source.Branch)
		if err != nil {
			updateErrors = append(updateErrors, sourceError(source, "read remote revision", err))
			continue
		}
		remoteFields := strings.Fields(string(remote))
		if len(remoteFields) == 0 {
			updateErrors = append(updateErrors, sourceError(source, "read remote revision", fmt.Errorf("remote branch not found: %s", source.Branch)))
			continue
		}
		currentRevision := strings.TrimSpace(string(current))
		plans = append(plans, updatePlan{source: source, result: UpdateResult{
			SourceID: source.ID,
			Branch:   source.Branch,
			Current:  currentRevision,
			Remote:   remoteFields[0],
			Changed:  currentRevision != remoteFields[0],
		}})
	}
	results := make([]UpdateResult, 0, len(plans))
	if dryRun {
		for _, plan := range plans {
			results = append(results, plan.result)
		}
		return results, errors.Join(updateErrors...)
	}
	for _, plan := range plans {
		if !plan.result.Changed {
			results = append(results, plan.result)
			continue
		}
		applied, err := m.applyUpdate(ctx, plan.source, plan.result.Remote)
		if applied {
			results = append(results, plan.result)
		}
		if err != nil {
			updateErrors = append(updateErrors, err)
		}
	}
	return results, errors.Join(updateErrors...)
}

func (m Manager) applyUpdate(ctx context.Context, source catalog.Source, revision string) (bool, error) {
	if err := m.initializeSubmodule(ctx, source); err != nil {
		return false, err
	}
	if _, err := m.Git.Output(ctx, source.Path, "fetch", "--no-tags", "origin", "refs/heads/"+source.Branch); err != nil {
		return false, sourceError(source, "fetch tracked branch", err)
	}
	if _, err := m.Git.Output(ctx, source.Path, "reset", "--hard", revision); err != nil {
		return false, sourceError(source, "checkout remote revision", err)
	}
	actual, err := m.Git.Output(ctx, source.Path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return false, sourceError(source, "verify updated revision", err)
	}
	if actualRevision := strings.TrimSpace(string(actual)); actualRevision != revision {
		return false, sourceError(source, "verify updated revision", fmt.Errorf("expected %s, got %s", revision, actualRevision))
	}
	if len(source.DiscoveryPriority) == 0 && len(source.SkillPaths) == 0 && len(source.SparsePaths) == 0 {
		return true, nil
	}
	if _, err := m.Git.Output(ctx, source.Path, "sparse-checkout", "disable"); err != nil {
		return false, sourceError(source, "expand sparse checkout", err)
	}
	discovery, err := catalog.PlanVendorDiscovery(source.Path, source.DiscoveryPriority, source.SkillPaths)
	if err != nil {
		return false, sourceError(source, "recompute discovery", err)
	}
	effectiveSparsePaths := mergeSparsePaths(source.SparsePaths, discovery.SparsePaths)
	if len(effectiveSparsePaths) == 0 {
		return true, nil
	}
	if _, err := m.Git.Output(ctx, source.Path, "sparse-checkout", "init", "--cone"); err != nil {
		return false, sourceError(source, "initialize sparse checkout", err)
	}
	if _, err := m.Git.Output(ctx, source.Path, append([]string{"sparse-checkout", "set"}, effectiveSparsePaths...)...); err != nil {
		return false, sourceError(source, "reapply sparse checkout", err)
	}
	return true, nil
}

func (m Manager) initializeSubmodule(ctx context.Context, source catalog.Source) error {
	repositoryRoot, err := m.repositoryRoot(ctx)
	if err != nil {
		return sourceError(source, "resolve resources repository root", err)
	}
	relativePath, err := filepath.Rel(repositoryRoot, source.Path)
	if err != nil || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return sourceError(source, "resolve submodule path", fmt.Errorf("path is outside repository root %s", repositoryRoot))
	}
	if _, err := m.Git.Output(ctx, repositoryRoot,
		"submodule", "update", "--init", "--", filepath.ToSlash(relativePath),
	); err != nil {
		return sourceError(source, "initialize submodule", err)
	}
	return nil
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
