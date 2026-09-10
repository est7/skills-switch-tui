package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

// sourceNamePattern accepts a vendor source name: the owner/repo path a source
// occupies under its scope, or a bare repository name for a remote with no owner
// segment and for sources registered before the owner level existed.
var sourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)?$`)

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
	targetPath := filepath.Join(m.SkillsRoot, "vendor", request.Scope, filepath.FromSlash(request.Name))
	if err := m.rejectRegisteredRemote(ctx, repositoryRoot, request.URL); err != nil {
		return err
	}
	if _, err := os.Lstat(targetPath); err == nil {
		return fmt.Errorf("source path already exists: %s", targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect source path: %w", err)
	}
	// A source registered before the owner level existed occupies a bare
	// repository name, which a later owner of the same name would nest a
	// checkout inside of.
	if owner := filepath.Dir(targetPath); strings.Contains(request.Name, "/") {
		if _, err := os.Lstat(filepath.Join(owner, ".git")); err == nil {
			return fmt.Errorf("owner path %s is an existing checkout: rename it before adding %s", owner, request.Name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect owner path: %w", err)
		}
	}
	if err := catalog.ValidateSourceRegistration(m.SkillsRoot, sourceID); err != nil {
		return err
	}
	relativePath, err := filepath.Rel(repositoryRoot, targetPath)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return fmt.Errorf("skills root must be inside repository root %s: %s", repositoryRoot, m.SkillsRoot)
	}
	// The rollback is armed before `submodule add` runs: git clones into
	// .git/modules first and only then checks the branch out, so a missing
	// branch fails after the clone with nothing staged and a checkout holding
	// nothing but a .git pointer. Both that strand and a fully added submodule
	// are discarded the same way.
	completed := false
	defer func() {
		if completed {
			return
		}
		if rollbackErr := m.discardCheckout(context.WithoutCancel(ctx), repositoryRoot, targetPath, relativePath); rollbackErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback added submodule: %w", rollbackErr))
		}
	}()
	if _, err := m.Git.Output(ctx, repositoryRoot,
		"submodule", "add", "-b", request.Branch, request.URL, filepath.ToSlash(relativePath),
	); err != nil {
		return err
	}
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

type registeredSubmodule struct {
	Path string
	URL  string
}

// MigrationStatus is what became of one planned move. A run reports each
// outcome distinctly: a source left alone by preflight, one that moved, one an
// error moved back, and one left mid-move because the restore itself failed are
// four different states for a user to act on.
type MigrationStatus string

const (
	MigrationPlanned        MigrationStatus = "planned"
	MigrationMoved          MigrationStatus = "moved"
	MigrationSkipped        MigrationStatus = "skipped"
	MigrationFailed         MigrationStatus = "failed"
	MigrationRolledBack     MigrationStatus = "rolled_back"
	MigrationRollbackFailed MigrationStatus = "rollback_failed"
)

// Migration is one vendor checkout's move from the bare repository name it was
// registered under to the owner/repo path derived from its remote.
type Migration struct {
	SourceID   string          `json:"source"`
	TargetID   string          `json:"target,omitempty"`
	Path       string          `json:"path"`
	TargetPath string          `json:"targetPath,omitempty"`
	URL        string          `json:"url,omitempty"`
	Status     MigrationStatus `json:"status"`
	Reason     string          `json:"reason,omitempty"`
}

func (m Migration) Actionable() bool {
	return m.Status == MigrationPlanned
}

func (m Migration) skip(reason string) Migration {
	m.Status = MigrationSkipped
	m.Reason = reason
	return m
}

// PlanOwnerMigration reports how each vendor source registered under a bare
// repository name would move under its owner. A source is planned only when its
// remote yields an owner, its checkout is clean, and the destination is free;
// everything else is returned with the reason it was skipped.
func (m Manager) PlanOwnerMigration(ctx context.Context, sources []catalog.Source) ([]Migration, error) {
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	repositoryRoot, err := m.repositoryRoot(ctx)
	if err != nil {
		return nil, err
	}
	modules, err := m.registeredSubmodules(ctx, repositoryRoot)
	if err != nil {
		return nil, err
	}
	remotes := make(map[string]string, len(modules))
	for _, module := range modules {
		remotes[filepath.Clean(filepath.Join(repositoryRoot, filepath.FromSlash(module.Path)))] = module.URL
	}

	migrations := make([]Migration, 0)
	claimed := make(map[string]string)
	for _, source := range sources {
		if !source.IsVendor() || source.IsArchived() {
			continue
		}
		_, name, found := strings.Cut(source.ID, "/")
		if !found || strings.Contains(name, "/") {
			continue
		}
		migration := Migration{SourceID: source.ID, Path: source.Path, Status: MigrationPlanned}
		url, tracked := remotes[filepath.Clean(source.Path)]
		if !tracked {
			migrations = append(migrations, migration.skip("checkout is not a registered submodule"))
			continue
		}
		migration.URL = url
		targetName := remoteName(url)
		if targetName == "" {
			migrations = append(migrations, migration.skip("remote has no owner segment to qualify the name with"))
			continue
		}
		migration.TargetID = catalog.ScopedSourceID(catalog.SourceVendor, source.Scope, targetName)
		migration.TargetPath = filepath.Join(m.SkillsRoot, "vendor", source.Scope, filepath.FromSlash(targetName))
		if owner, taken := claimed[migration.TargetPath]; taken {
			migrations = append(migrations, migration.skip("destination is already claimed by "+owner))
			continue
		}
		// A destination nested inside the source is the legitimate case of a
		// source named after what turns out to be its own owner; it is staged
		// through a sibling rather than treated as a collision.
		if _, err := os.Lstat(migration.TargetPath); err == nil && !nestedUnder(migration.TargetPath, migration.Path) {
			migrations = append(migrations, migration.skip("destination already exists"))
			continue
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect migration destination %s: %w", migration.TargetPath, err)
		}
		if err := catalog.ValidateSourceRegistration(m.SkillsRoot, migration.TargetID); err != nil {
			migrations = append(migrations, migration.skip(err.Error()))
			continue
		}
		status, err := m.Git.Output(ctx, source.Path, "status", "--porcelain")
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", source.ID, err)
		}
		if strings.TrimSpace(string(status)) != "" {
			migrations = append(migrations, migration.skip("checkout has local changes"))
			continue
		}
		claimed[migration.TargetPath] = source.ID
		migrations = append(migrations, migration)
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].SourceID < migrations[j].SourceID })
	return migrations, nil
}

// MigrationError reports what a failed migration left on disk, so a caller can
// tell "nothing changed" from "moved back" from "stranded mid-move" instead of
// collapsing all three into one failure.
type MigrationError struct {
	Status MigrationStatus
	Err    error
}

func (e *MigrationError) Error() string { return e.Err.Error() }

func (e *MigrationError) Unwrap() error { return e.Err }

// relocationError reports a failed relocation and, decisively, whether the
// checkout is still where the operation found it. A recovery that succeeds at an
// outer layer must be able to overrule an inner layer that could not recover on
// its own, so this is carried as state rather than as a sentinel in the error
// chain — an error, once wrapped, cannot be un-wrapped when a later step fixes
// what it described.
type relocationError struct {
	Stranded bool
	Err      error
}

func (e *relocationError) Error() string { return e.Err.Error() }

func (e *relocationError) Unwrap() error { return e.Err }

// stranded reports whether a relocation left the checkout somewhere other than
// where it started. The outermost relocationError wins: it is the one that knows
// the final state.
func stranded(err error) bool {
	var relocationErr *relocationError
	return errors.As(err, &relocationErr) && relocationErr.Stranded
}

// strandedFailure marks a failure that could not put the checkout back.
func strandedFailure(err error) error {
	return &relocationError{Stranded: true, Err: err}
}

// recoveredFailure marks a failure whose checkout is back where it started. It
// overrules any inner verdict, including one from a step that could not recover
// on its own before this layer did.
func recoveredFailure(err error) error {
	return &relocationError{Err: err}
}

// MigrationStatusOf reports what a MigrateSource failure left behind. An error
// from anywhere else means the migration never touched the checkout.
func MigrationStatusOf(err error) MigrationStatus {
	var migrationErr *MigrationError
	if errors.As(err, &migrationErr) {
		return migrationErr.Status
	}
	return MigrationFailed
}

// MigrateSource moves one planned checkout and carries its registration across.
// `git mv` is what relocates a submodule: it rewrites .gitmodules, the gitlink,
// and the checkout's gitdir pointer together. Every failure path either leaves
// the checkout exactly where it started or reports that it could not.
func (m Manager) MigrateSource(ctx context.Context, migration Migration) error {
	if !migration.Actionable() {
		return fmt.Errorf("migrate %s: %s", migration.SourceID, migration.Reason)
	}
	if m.Git == nil {
		m.Git = GitCommander{}
	}
	repositoryRoot, err := m.repositoryRoot(ctx)
	if err != nil {
		return err
	}
	if err := m.relocate(ctx, repositoryRoot, migration.Path, migration.TargetPath); err != nil {
		m.pruneEmptyOwnerDirectory(migration.TargetPath)
		status := MigrationFailed
		if stranded(err) {
			status = MigrationRollbackFailed
		}
		return &MigrationError{
			Status: status,
			Err:    fmt.Errorf("move %s checkout: %w", migration.SourceID, err),
		}
	}
	if err := catalog.RenameSource(m.SkillsRoot, migration.SourceID, migration.TargetID); err != nil {
		cause := fmt.Errorf("register %s after moving its checkout: %w", migration.TargetID, err)
		if restoreErr := m.relocate(context.WithoutCancel(ctx), repositoryRoot, migration.TargetPath, migration.Path); restoreErr != nil {
			return &MigrationError{
				Status: MigrationRollbackFailed,
				Err:    errors.Join(cause, fmt.Errorf("restore %s checkout: %w", migration.SourceID, restoreErr)),
			}
		}
		m.pruneEmptyOwnerDirectory(migration.TargetPath)
		return &MigrationError{Status: MigrationRolledBack, Err: cause}
	}
	m.pruneEmptyOwnerDirectory(migration.Path)
	return nil
}

// relocate moves a checkout between two paths and guarantees the outcome is one
// of two states: the checkout is at `to`, or it is back at `from`. A failure it
// cannot undo is reported as errStrandedCheckout rather than as a plain move
// failure, because the two demand different recovery from a user.
//
// When one path contains the other — a source named after what turns out to be
// its own owner, or the reverse move that undoes it — `git mv` refuses outright,
// so the checkout is staged through a directory outside both.
func (m Manager) relocate(ctx context.Context, repositoryRoot, from, to string) error {
	if !nestedUnder(to, from) && !nestedUnder(from, to) {
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return fmt.Errorf("create destination directory for %s: %w", to, err)
		}
		return m.moveCheckout(ctx, repositoryRoot, from, to)
	}
	base := filepath.Dir(to)
	if nestedUnder(to, from) {
		base = filepath.Dir(from)
	}
	staged, err := freeStagingPath(base, filepath.Base(filepath.Clean(from)))
	if err != nil {
		return err
	}
	if err := m.moveCheckout(ctx, repositoryRoot, from, staged); err != nil {
		return err
	}
	if err := m.completeStagedMove(ctx, repositoryRoot, staged, to); err != nil {
		undoCtx := context.WithoutCancel(ctx)
		if undoErr := m.completeStagedMove(undoCtx, repositoryRoot, staged, from); undoErr != nil {
			return strandedFailure(errors.Join(err, fmt.Errorf("staged at %s: %w", staged, undoErr)))
		}
		// The checkout is back at `from`, whatever the failed attempt reported
		// about its own intermediate state.
		return recoveredFailure(err)
	}
	return nil
}

// completeStagedMove finishes a staged relocation. The directory a staged
// checkout vacated can sit exactly on the destination; `git mv` would move the
// checkout inside it instead of onto it, so an emptied one is cleared first. A
// directory that still holds anything fails the removal and stops the move.
func (m Manager) completeStagedMove(ctx context.Context, repositoryRoot, staged, to string) error {
	if info, err := os.Lstat(to); err == nil && info.IsDir() {
		if err := os.Remove(to); err != nil {
			return fmt.Errorf("clear destination %s: %w", to, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination %s: %w", to, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("create destination directory for %s: %w", to, err)
	}
	return m.moveCheckout(ctx, repositoryRoot, staged, to)
}

// moveCheckout relocates a submodule worktree and repoints its gitdir at the new
// location, as one operation. Repair belongs to every move: `git mv` leaves a
// sparse checkout's config.worktree pointing at where the checkout used to be.
// A repair that fails moves the checkout back, so a caller that sees an error
// can rely on nothing having moved.
func (m Manager) moveCheckout(ctx context.Context, repositoryRoot, from, to string) error {
	fromRelative, err := m.repositoryRelativePath(repositoryRoot, from)
	if err != nil {
		return err
	}
	toRelative, err := m.repositoryRelativePath(repositoryRoot, to)
	if err != nil {
		return err
	}
	if _, err := m.Git.Output(ctx, repositoryRoot, "mv", "--", fromRelative, toRelative); err != nil {
		return err
	}
	repairErr := m.repairSubmoduleWorktree(ctx, repositoryRoot, to)
	if repairErr == nil {
		return nil
	}
	undoCtx := context.WithoutCancel(ctx)
	if _, undoErr := m.Git.Output(undoCtx, repositoryRoot, "mv", "--", toRelative, fromRelative); undoErr != nil {
		return strandedFailure(errors.Join(repairErr, fmt.Errorf("left at %s: %w", to, undoErr)))
	}
	if err := m.repairSubmoduleWorktree(undoCtx, repositoryRoot, from); err != nil {
		return strandedFailure(errors.Join(repairErr, fmt.Errorf("worktree pointer at %s: %w", from, err)))
	}
	return recoveredFailure(repairErr)
}

// nestedUnder reports whether path sits strictly inside root.
func nestedUnder(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path != root && strings.HasPrefix(path, root+string(filepath.Separator))
}

// freeStagingPath picks an unused directory to stage a move through. The
// StagingPrefix keeps a directory left behind by an interrupted run out of
// discovery and out of the names a source can be registered under.
func freeStagingPath(directory, name string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s%s-%d", catalog.StagingPrefix, name, attempt))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("inspect staging path %s: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("no free staging path in %s", directory)
}

// repairSubmoduleWorktree points a moved submodule's core.worktree at its new
// location. `git mv` rewrites that setting in the submodule's main config only,
// but a sparse checkout turns on extensions.worktreeConfig, and the stale value
// left behind in config.worktree outranks it — a moved sparse checkout is
// otherwise left with a gitdir that cannot find its worktree.
func (m Manager) repairSubmoduleWorktree(ctx context.Context, repositoryRoot, worktree string) error {
	gitdir, err := submoduleGitdir(worktree)
	if err != nil {
		return err
	}
	if gitdir == "" {
		// A plain directory rather than a submodule checkout: nothing pins a
		// worktree, so nothing needs repointing.
		return nil
	}
	relative, err := filepath.Rel(gitdir, worktree)
	if err != nil {
		return fmt.Errorf("resolve worktree of %s: %w", worktree, err)
	}
	relative = filepath.ToSlash(relative)
	// Run from the parent repository: git resolves a working tree before it
	// edits any file, and inside the submodule gitdir that resolution is exactly
	// what is broken.
	for _, name := range []string{"config", "config.worktree"} {
		file := filepath.Join(gitdir, name)
		if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect submodule %s: %w", name, err)
		}
		if _, err := m.Git.Output(ctx, repositoryRoot, "config", "--file", file, "core.worktree", relative); err != nil {
			return fmt.Errorf("repoint submodule worktree in %s: %w", file, err)
		}
	}
	return nil
}

// submoduleGitdir resolves the gitdir a submodule checkout points at, or "" when
// the path is not a submodule checkout.
func submoduleGitdir(worktree string) (string, error) {
	marker := filepath.Join(worktree, ".git")
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", marker, err)
	}
	if info.IsDir() {
		// A standalone repository keeps its gitdir inside the worktree, so it
		// carries no core.worktree to repoint.
		return "", nil
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", marker, err)
	}
	pointer, ok := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
	if !ok {
		return "", fmt.Errorf("read %s: not a submodule gitdir pointer", marker)
	}
	gitdir := strings.TrimSpace(pointer)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	return filepath.Clean(gitdir), nil
}

func (m Manager) repositoryRelativePath(repositoryRoot, path string) (string, error) {
	relative, err := filepath.Rel(repositoryRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside repository root %s: %s", repositoryRoot, path)
	}
	return filepath.ToSlash(relative), nil
}

// rejectRegisteredRemote fails when the parent repository already tracks the
// requested remote as a vendor checkout. Owner-qualified names keep two distinct
// repositories from colliding on a path, so a path collision is no longer
// evidence of a duplicate: the remote itself is what identifies a source.
func (m Manager) rejectRegisteredRemote(ctx context.Context, repositoryRoot, url string) error {
	wanted := canonicalRemote(url)
	if wanted == "" {
		return nil
	}
	modules, err := m.registeredSubmodules(ctx, repositoryRoot)
	if err != nil {
		return err
	}
	for _, module := range modules {
		if canonicalRemote(module.URL) != wanted {
			continue
		}
		id := m.vendorSourceIDForPath(repositoryRoot, module.Path)
		if id == "" {
			continue
		}
		return fmt.Errorf("repository %s is already registered as %s at %s", url, id,
			filepath.Join(repositoryRoot, filepath.FromSlash(module.Path)))
	}
	return nil
}

// registeredSubmodules reads the parent repository's .gitmodules. A repository
// that has never had a submodule added carries no such file and tracks none.
func (m Manager) registeredSubmodules(ctx context.Context, repositoryRoot string) ([]registeredSubmodule, error) {
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".gitmodules")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect .gitmodules: %w", err)
	}
	output, err := m.Git.Output(ctx, repositoryRoot, "config", "--file", ".gitmodules", "--list")
	if err != nil {
		return nil, fmt.Errorf("read submodule configuration: %w", err)
	}
	paths := make(map[string]string)
	urls := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		// A submodule name may itself contain dots, so the trailing key segment
		// is what identifies the field.
		name, ok := strings.CutPrefix(key, "submodule.")
		if !ok {
			continue
		}
		if trimmed, ok := strings.CutSuffix(name, ".path"); ok {
			paths[trimmed] = value
		} else if trimmed, ok := strings.CutSuffix(name, ".url"); ok {
			urls[trimmed] = value
		}
	}
	names := make([]string, 0, len(paths))
	for name, path := range paths {
		if path != "" && urls[name] != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	modules := make([]registeredSubmodule, 0, len(names))
	for _, name := range names {
		modules = append(modules, registeredSubmodule{Path: paths[name], URL: urls[name]})
	}
	return modules, nil
}

// vendorSourceIDForPath maps a repository-relative submodule path to its vendor
// source ID, or "" when the path is not a vendor checkout under the skills root.
func (m Manager) vendorSourceIDForPath(repositoryRoot, submodulePath string) string {
	absolute := filepath.Join(repositoryRoot, filepath.FromSlash(submodulePath))
	relative, err := filepath.Rel(filepath.Join(m.SkillsRoot, "vendor"), absolute)
	if err != nil {
		return ""
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) < 2 || segments[0] == ".." || segments[0] == "." {
		return ""
	}
	return catalog.ScopedSourceID(catalog.SourceVendor, segments[0], strings.Join(segments[1:], "/"))
}

// remoteName derives the owner/repo name a checkout belongs under from any
// tracked remote, including the local mirror paths ParseSourceRef deliberately
// refuses as user input. It returns "" when the remote carries no owner segment
// or yields a name a source cannot be registered under.
func remoteName(url string) string {
	path := strings.TrimSpace(url)
	if index := strings.Index(path, "://"); index >= 0 {
		_, path, _ = strings.Cut(path[index+3:], "/")
	} else if _, scpPath, ok := cutSCPRemote(path); ok {
		path = scpPath
	}
	name := qualifiedName(splitPathSegments(strings.TrimSuffix(strings.Trim(path, "/"), ".git")))
	if !strings.Contains(name, "/") || !sourceNamePattern.MatchString(name) {
		return ""
	}
	return name
}

// canonicalRemote reduces a clone URL to a host/path identity so the same
// repository is recognized across scheme, credentials, and .git suffix
// differences. It returns "" when no identity can be derived.
//
// Two remotes are the same repository only when they address the same server, so
// the port stays part of the identity and only a scheme's default port is
// dropped. Host names are case-insensitive; repository paths are not, because
// the server — or a case-sensitive filesystem for a local mirror — decides that.
// A file:// URL and a plain path name the same local repository.
func canonicalRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && strings.Contains(raw, "://") {
		if parsed.Scheme == "file" {
			// Git ignores a file URL's authority and clones the path on this
			// machine, so file://anything/p and /p are one repository. Treating
			// the authority as part of the identity would let the same
			// repository be registered twice under two names.
			return localRemoteIdentity(parsed.Path)
		}
		path := trimRepositoryPath(parsed.Path)
		host := remoteHostIdentity(strings.ToLower(parsed.Hostname()), parsed.Port(), parsed.Scheme)
		if host == "" || path == "" {
			return ""
		}
		return host + "/" + path
	}
	if host, path, ok := cutSCPRemote(raw); ok {
		path = trimRepositoryPath(path)
		if path == "" {
			return ""
		}
		host, port := strings.ToLower(host), ""
		if bracketed, rest, ok := strings.Cut(host, "]"); ok {
			host, port = strings.TrimPrefix(bracketed, "["), strings.TrimPrefix(rest, ":")
		}
		return remoteHostIdentity(host, port, "ssh") + "/" + path
	}
	return localRemoteIdentity(raw)
}

// remoteHostIdentity renders a host and port as one identity. An IPv6 literal
// keeps its brackets: without them, host 2001:db8::1 on port 2222 and host
// 2001:db8::1:2222 on the default port would read as the same server.
func remoteHostIdentity(host, port, scheme string) string {
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" && port != defaultRemotePort(scheme) {
		host += ":" + port
	}
	return host
}

// localRemoteIdentity identifies a mirror addressed by filesystem path. The path
// is made absolute so a relative and an absolute reference to one mirror agree.
// Unlike a served repository, a local path keeps any .git suffix: /tmp/repo.git
// and /tmp/repo are two directories, not one repository written two ways.
func localRemoteIdentity(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return "file/" + path
	}
	return "file/" + filepath.ToSlash(absolute)
}

// cutSCPRemote splits an scp-style remote into host and path. The userinfo is
// optional: `host:owner/repo` is as valid a remote as `git@host:owner/repo`, and
// both must reduce to the same identity as the equivalent URL.
func cutSCPRemote(raw string) (string, string, bool) {
	if strings.Contains(raw, "://") {
		return "", "", false
	}
	// The userinfo separator is the "@" before the host, not one inside a path.
	authority := raw
	if at := strings.Index(raw, "@"); at >= 0 {
		if colon := strings.Index(raw, ":"); colon < 0 || at < colon {
			authority = raw[at+1:]
		}
	}
	var host, path string
	if strings.HasPrefix(authority, "[") {
		// A bracketed IPv6 literal carries colons of its own, so the path
		// separator is the first colon after the closing bracket.
		end := strings.Index(authority, "]")
		if end < 0 || end+1 >= len(authority) || authority[end+1] != ':' {
			return "", "", false
		}
		host, path = authority[:end+1], authority[end+2:]
	} else {
		var found bool
		if host, path, found = strings.Cut(authority, ":"); !found {
			return "", "", false
		}
	}
	if host == "" || path == "" {
		return "", "", false
	}
	// A Windows drive letter, an absolute path, and a relative path are not
	// remote hosts; a host name never contains a path separator.
	if strings.HasPrefix(path, "/") || len(host) == 1 {
		return "", "", false
	}
	if strings.HasPrefix(host, ".") || strings.ContainsAny(host, `/\`) {
		return "", "", false
	}
	return host, path, true
}

func trimRepositoryPath(path string) string {
	return strings.TrimSuffix(strings.Trim(path, "/"), ".git")
}

func defaultRemotePort(scheme string) string {
	switch scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	case "ssh", "git+ssh":
		return "22"
	case "git":
		return "9418"
	}
	return ""
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
	repositoryRoot, err := m.repositoryRoot(ctx)
	if err != nil {
		return err
	}
	relativePath, err := filepath.Rel(repositoryRoot, source.Path)
	if err != nil || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source path is outside repository root %s: %s", repositoryRoot, source.Path)
	}
	staged, err := m.isStagedGitlink(ctx, repositoryRoot, relativePath)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", source.ID, err)
	}
	if !staged {
		return m.removeOrphanCheckout(ctx, repositoryRoot, source, relativePath)
	}
	status, err := m.Git.Output(ctx, source.Path, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("inspect %s: %w", source.ID, err)
	}
	if strings.TrimSpace(string(status)) != "" {
		return newDirtyError([]DirtySource{{SourceID: source.ID, Path: source.Path, Status: string(status)}})
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
	m.pruneEmptyOwnerDirectory(source.Path)
	return nil
}

// removeOrphanCheckout deletes a vendor checkout that discovery lists but the
// parent index does not track: what a `submodule add` that failed after its
// clone leaves behind. Such a checkout has no user work to protect — its status
// reports every tracked file as a staged deletion because the worktree was never
// populated — and nothing for `git rm` to remove, so the dirty guard and the git
// path both stay out of it. A policy that survived in catalog.yaml goes with it.
func (m Manager) removeOrphanCheckout(ctx context.Context, repositoryRoot string, source catalog.Source, relativePath string) error {
	if err := removeStrandedCheckout(source.Path); err != nil {
		return fmt.Errorf("remove %s orphan checkout: %w", source.ID, err)
	}
	if err := m.removeSubmoduleGitdir(ctx, repositoryRoot, relativePath); err != nil {
		return fmt.Errorf("remove %s submodule gitdir: %w", source.ID, err)
	}
	registered, err := catalog.IsSourceRegistered(m.SkillsRoot, source.ID)
	if err != nil {
		return fmt.Errorf("inspect %s policy: %w", source.ID, err)
	}
	if registered {
		if err := catalog.UnregisterSource(m.SkillsRoot, source.ID); err != nil {
			return fmt.Errorf("unregister %s after removing orphan checkout: %w", source.ID, err)
		}
	}
	m.pruneEmptyOwnerDirectory(source.Path)
	return nil
}

// discardCheckout removes a vendor checkout in whichever state an add left it: a
// staged gitlink is dropped through git so .gitmodules follows, and a checkout
// that never reached the index is deleted directly. Both end with the
// .git/modules gitdir gone, so a later add of the same path is not refused as
// an existing local repository, and with an emptied owner directory pruned.
func (m Manager) discardCheckout(ctx context.Context, repositoryRoot, targetPath, relativePath string) error {
	staged, err := m.isStagedGitlink(ctx, repositoryRoot, relativePath)
	if err != nil {
		return err
	}
	if staged {
		if _, err := m.Git.Output(ctx, repositoryRoot, "rm", "-f", "--", filepath.ToSlash(relativePath)); err != nil {
			return err
		}
	} else if err := removeStrandedCheckout(targetPath); err != nil {
		return err
	}
	if err := m.removeSubmoduleGitdir(ctx, repositoryRoot, relativePath); err != nil {
		return fmt.Errorf("rollback submodule gitdir: %w", err)
	}
	m.pruneEmptyOwnerDirectory(targetPath)
	return nil
}

// isStagedGitlink reports whether the parent index tracks relativePath as a
// submodule (mode 160000). A path git has not staged — because `submodule add`
// failed after its clone, or because it was never a submodule — is not.
func (m Manager) isStagedGitlink(ctx context.Context, repositoryRoot, relativePath string) (bool, error) {
	out, err := m.Git.Output(ctx, repositoryRoot, "ls-files", "--stage", "--", filepath.ToSlash(relativePath))
	if err != nil {
		return false, fmt.Errorf("inspect index entry for %s: %w", relativePath, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "160000 ") {
			return true, nil
		}
	}
	return false, nil
}

// removeStrandedCheckout deletes a checkout that is a submodule pointer into a
// gitdir kept elsewhere. A path that is already gone needs nothing; a directory
// holding its own .git is a standalone repository somebody placed by hand and is
// refused rather than deleted.
func removeStrandedCheckout(path string) error {
	gitdir, err := submoduleGitdir(path)
	if err != nil {
		return err
	}
	if gitdir == "" {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		return fmt.Errorf("refuse to delete %s: not a submodule checkout", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("delete stranded checkout %s: %w", path, err)
	}
	return nil
}

// pruneEmptyOwnerDirectory drops the owner level a removed checkout leaves
// behind. Removal is best effort and only ever touches an already empty
// directory that sits strictly below a scope root under vendor/, so a scope root
// and any owner still holding a checkout both survive.
func (m Manager) pruneEmptyOwnerDirectory(sourcePath string) {
	vendorRoot := filepath.Join(m.SkillsRoot, "vendor")
	owner := filepath.Dir(filepath.Clean(sourcePath))
	relative, err := filepath.Rel(vendorRoot, owner)
	if err != nil {
		return
	}
	// scope/owner is the only depth an owner directory can occupy.
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) != 2 || segments[0] == ".." || segments[1] == ".." {
		return
	}
	_ = os.Remove(owner)
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
