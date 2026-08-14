package projection

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
	"github.com/est7/skills-switch-tui/internal/linktransaction"
)

type AdoptionStatus string

const (
	AdoptionAdopted  AdoptionStatus = "adopted"
	AdoptionRefused  AdoptionStatus = "refused"
	AdoptionFailed   AdoptionStatus = "failed"
	AdoptionStranded AdoptionStatus = "stranded"
)

type Adoption struct {
	Path     string
	SkillID  string
	SSOTPath string
	Status   AdoptionStatus
	Reason   string
}

// AdoptSkills moves unmanaged client Skills into the local catalog and leaves
// their original locations enabled as managed projections. Each path is an
// independent transaction; errors are aggregated after every item is tried.
func (m Manager) AdoptSkills(paths []string, scope, group string) ([]Adoption, error) {
	if strings.TrimSpace(scope) == "" {
		scope = "shared"
	}
	if scope != "shared" {
		if err := m.clients.Require(client.ID(scope), client.CapabilitySkills); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(m.skillsRoot) == "" {
		return nil, errors.New("adopt Skills requires a catalog root")
	}

	results := make([]Adoption, 0, len(paths))
	itemErrors := make([]error, 0)
	for _, path := range paths {
		result, err := m.adoptSkill(path, scope, group)
		results = append(results, result)
		if err != nil {
			itemErrors = append(itemErrors, fmt.Errorf("adopt %s: %w", path, err))
		}
	}
	return results, errors.Join(itemErrors...)
}

func (m Manager) adoptSkill(path, scope, group string) (Adoption, error) {
	result := Adoption{Path: path}
	unmanaged, err := m.unmanagedSkillAt(path)
	if err != nil {
		return failedAdoption(result, err)
	}
	if unmanaged == nil {
		return refusedAdoption(result, errors.New("path is not an unmanaged Skill reported by discover"))
	}
	result.Path = unmanaged.Path
	target, err := catalog.ResolveLocalSkillTarget(m.skillsRoot, scope, group, unmanaged.Name)
	if err != nil {
		return refusedAdoption(result, err)
	}
	result.SkillID = target.ID
	result.SSOTPath = target.Path
	if _, err := os.Lstat(target.Path); err == nil {
		return refusedAdoption(result, fmt.Errorf("destination already exists: %s", target.Path))
	} else if !errors.Is(err, os.ErrNotExist) {
		return failedAdoption(result, fmt.Errorf("inspect destination: %w", err))
	}
	backupPath := unmanaged.Path + ".adopting-backup"
	if _, err := os.Lstat(backupPath); err == nil {
		return refusedAdoption(result, fmt.Errorf("adoption backup already exists: %s", backupPath))
	} else if !errors.Is(err, os.ErrNotExist) {
		return failedAdoption(result, fmt.Errorf("inspect adoption backup: %w", err))
	}

	if err := copyAdoptionTree(unmanaged.Path, target.Path); err != nil {
		cleanupErr := cleanupAdoptionSSOT(target.Path, m.skillsRoot)
		return finalizeAdoptionFailure(result, backupPath, errors.Join(fmt.Errorf("copy Skill to SSOT: %w", err), cleanupErr))
	}
	if err := os.Rename(unmanaged.Path, backupPath); err != nil {
		cleanupErr := cleanupAdoptionSSOT(target.Path, m.skillsRoot)
		return finalizeAdoptionFailure(result, backupPath, errors.Join(fmt.Errorf("create adoption backup: %w", err), cleanupErr))
	}

	var applied *linktransaction.Applied
	operationErr := error(nil)
	if m.beforeAdoptLink != nil {
		operationErr = m.beforeAdoptLink(unmanaged.Path, backupPath, target.Path)
	}
	if operationErr == nil {
		engine := adoptionLinkEngine()
		transaction, linkErr := engine.Execute([]linktransaction.Change{
			linktransaction.Create(unmanaged.Path, target.Path),
		})
		if linkErr != nil {
			operationErr = fmt.Errorf("create managed projection: %w", linkErr)
		} else {
			applied = &transaction
		}
	}
	if operationErr == nil {
		if err := os.RemoveAll(backupPath); err != nil {
			operationErr = fmt.Errorf("remove adoption backup: %w", err)
		}
	}
	if operationErr == nil {
		result.Status = AdoptionAdopted
		return result, nil
	}

	rollbackErr := rollbackAdoption(unmanaged.Path, backupPath, target.Path, m.skillsRoot, applied)
	combined := errors.Join(operationErr, rollbackErr)
	return finalizeAdoptionFailure(result, backupPath, combined)
}

func finalizeAdoptionFailure(result Adoption, backupPath string, err error) (Adoption, error) {
	if adoptionRestored(result.Path, backupPath, result.SSOTPath) {
		return failedAdoption(result, err)
	}
	stranded := fmt.Errorf(
		"adoption stranded; original=%s backup=%s ssot=%s: %w",
		result.Path,
		backupPath,
		result.SSOTPath,
		err,
	)
	result.Status = AdoptionStranded
	result.Reason = stranded.Error()
	return result, stranded
}

func (m Manager) unmanagedSkillAt(path string) (*UnmanagedSkill, error) {
	discovered, err := m.DiscoverUnmanagedSkills()
	if err != nil {
		return nil, err
	}
	want := filepath.Clean(path)
	for index := range discovered {
		if filepath.Clean(discovered[index].Path) == want {
			return &discovered[index], nil
		}
	}
	return nil, nil
}

func refusedAdoption(result Adoption, err error) (Adoption, error) {
	result.Status = AdoptionRefused
	result.Reason = err.Error()
	return result, err
}

func failedAdoption(result Adoption, err error) (Adoption, error) {
	result.Status = AdoptionFailed
	result.Reason = err.Error()
	return result, err
}

func adoptionLinkEngine() linktransaction.Engine {
	return linktransaction.Engine{
		Label:          "skill adoption projection",
		MatchTarget:    linktransaction.EquivalentTarget,
		ValidateTarget: validateAdoptionTarget,
	}
}

func validateAdoptionTarget(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect adopted Skill %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("adopted Skill is not a directory: %s", path)
	}
	manifest, err := os.Stat(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("inspect adopted SKILL.md: %w", err)
	}
	if manifest.IsDir() {
		return fmt.Errorf("adopted SKILL.md is a directory: %s", path)
	}
	return nil
}

func copyAdoptionTree(source, destination string) error {
	rootInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("source is not a real directory: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}

	type directoryMode struct {
		path string
		mode fs.FileMode
	}
	directories := []directoryMode{{path: destination, mode: rootInfo.Mode()}}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destination, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		case info.IsDir():
			if err := os.Mkdir(targetPath, 0o700); err != nil {
				return err
			}
			directories = append(directories, directoryMode{path: targetPath, mode: info.Mode()})
			return nil
		case info.Mode().IsRegular():
			return copyAdoptionFile(path, targetPath, info.Mode())
		default:
			return fmt.Errorf("unsupported file type %s at %s", info.Mode().Type(), path)
		}
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index].path, directories[index].mode); err != nil {
			return err
		}
	}
	return nil
}

func copyAdoptionFile(source, destination string, mode fs.FileMode) (resultErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.Close()) }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if output != nil {
			resultErr = errors.Join(resultErr, output.Close())
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	output = nil
	return os.Chmod(destination, mode)
}

func rollbackAdoption(original, backup, ssot, skillsRoot string, applied *linktransaction.Applied) error {
	rollbackErrors := make([]error, 0)
	if applied != nil {
		if err := applied.Restore(); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove adoption projection: %w", err))
		}
	} else if err := removeExactAdoptionLink(original, ssot); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	if _, err := os.Lstat(original); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(backup, original); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore adoption backup %s to %s: %w", backup, original, err))
		}
	} else if err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect adoption original %s: %w", original, err))
	} else {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("cannot restore %s while a path exists; backup remains at %s", original, backup))
	}
	if err := cleanupAdoptionSSOT(ssot, skillsRoot); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	return errors.Join(rollbackErrors...)
}

func removeExactAdoptionLink(path, target string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect adoption projection %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("preserve concurrently created path %s", path)
	}
	actual, err := os.Readlink(path)
	if err != nil {
		return fmt.Errorf("read adoption projection %s: %w", path, err)
	}
	if !linktransaction.EquivalentTarget(path, actual, target) {
		return fmt.Errorf("preserve concurrently changed link %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove adoption projection %s: %w", path, err)
	}
	return nil
}

func cleanupAdoptionSSOT(path, skillsRoot string) error {
	localRoot := filepath.Join(skillsRoot, string(catalog.SourceLocal))
	relative, err := filepath.Rel(localRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to clean adoption path outside local catalog: %s", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove adoption SSOT %s: %w", path, err)
	}
	for current := filepath.Dir(path); current != localRoot && current != filepath.Dir(current); current = filepath.Dir(current) {
		if err := os.Remove(current); err != nil {
			break
		}
	}
	return nil
}

func adoptionRestored(original, backup, ssot string) bool {
	info, err := os.Lstat(original)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	if _, err := os.Lstat(ssot); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}
