package projection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type slotKind int

const (
	slotAbsent slotKind = iota
	slotSymlink
	slotCopy
)

type slotSpec struct {
	kind      slotKind
	target    string
	rawTarget string
	hash      string
}

type slotChange struct {
	path        string
	expected    slotSpec
	desired     slotSpec
	retargetTag bool
}

type executedSlotChange struct {
	change      slotChange
	appliedHash string
	stashPath   string
}

type appliedTransaction struct {
	changes            []executedSlotChange
	createdDirectories []string
	committed          bool
	restored           bool
}

func (a *appliedTransaction) Restore() error {
	if a == nil {
		return nil
	}
	a.restored = true
	var rollbackErrors []error
	for index := len(a.changes) - 1; index >= 0; index-- {
		if err := rollbackExecutedChange(&a.changes[index], true); err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if a.changes[index].stashPath != "" {
			_ = os.RemoveAll(a.changes[index].stashPath)
			a.changes[index].stashPath = ""
		}
	}
	cleanupCreatedDirs(a.createdDirectories)
	return errors.Join(rollbackErrors...)
}

func (a *appliedTransaction) Commit() error {
	if a == nil || a.committed || a.restored {
		return nil
	}
	a.committed = true
	var errs []error
	for index := range a.changes {
		if a.changes[index].stashPath != "" {
			if err := os.RemoveAll(a.changes[index].stashPath); err != nil {
				errs = append(errs, err)
				continue
			}
			a.changes[index].stashPath = ""
		}
	}
	return errors.Join(errs...)
}

func executeSlotChanges(changes []slotChange, keepStashes bool) (*appliedTransaction, error) {
	return executeSlotChangesWithHook(changes, keepStashes, nil)
}

func executeSlotChangesWithHook(changes []slotChange, keepStashes bool, beforeApply func(slotChange) error) (*appliedTransaction, error) {
	if len(changes) == 0 {
		return &appliedTransaction{}, nil
	}
	executed := make([]executedSlotChange, 0, len(changes))
	createdDirs := make([]string, 0)
	fail := func(applyErr error) (*appliedTransaction, error) {
		var rollbackErrors []error
		for index := len(executed) - 1; index >= 0; index-- {
			if err := rollbackExecutedChange(&executed[index], true); err != nil {
				rollbackErrors = append(rollbackErrors, err)
				continue
			}
			if executed[index].stashPath != "" {
				_ = os.RemoveAll(executed[index].stashPath)
				executed[index].stashPath = ""
			}
		}
		cleanupCreatedDirs(createdDirs)
		if len(rollbackErrors) == 0 {
			return nil, applyErr
		}
		return nil, errors.Join(applyErr, fmt.Errorf("rollback skill projection changes: %w", errors.Join(rollbackErrors...)))
	}

	for _, next := range changes {
		if beforeApply != nil {
			if err := beforeApply(next); err != nil {
				return fail(err)
			}
		}
		if err := validateSlotChange(next); err != nil {
			return fail(err)
		}
		if next.expected.kind == slotAbsent && next.desired.kind != slotAbsent {
			created, err := ensureProjectionDirectory(filepath.Dir(next.path))
			if err != nil {
				return fail(fmt.Errorf("create skill projection directory: %w", err))
			}
			createdDirs = append(createdDirs, created...)
			if err := validateSlotChange(next); err != nil {
				return fail(err)
			}
		}
		record, err := applySlotChange(next)
		if err != nil {
			return fail(err)
		}
		executed = append(executed, record)
	}

	applied := &appliedTransaction{
		changes:            executed,
		createdDirectories: createdDirs,
	}
	if !keepStashes {
		// Commit immediately when callers do not hold a staged Retirement for
		// deferred rollback (Apply, RefreshCopies, RetargetSource).
		_ = applied.Commit()
	}
	return applied, nil
}

func validateSlotChange(next slotChange) error {
	if next.desired.kind == slotSymlink || next.desired.kind == slotCopy {
		if err := validateSkillSource(next.desired.target); err != nil {
			return &ConflictError{Conflicts: []Conflict{{Path: next.desired.target, Reason: "source SKILL.md changed after preflight: " + err.Error()}}}
		}
	}
	actual, err := inspectEntry(next.path)
	if err != nil {
		return &ConflictError{Conflicts: []Conflict{{Path: next.path, Reason: "inspect after preflight: " + err.Error()}}}
	}
	if !entryMatchesSpec(actual, next.expected) {
		return &ConflictError{Conflicts: []Conflict{{Path: next.path, Reason: "target changed after preflight"}}}
	}
	return nil
}

func entryMatchesSpec(actual inspectedEntry, spec slotSpec) bool {
	switch spec.kind {
	case slotAbsent:
		return actual.kind == entryAbsent
	case slotSymlink:
		if actual.kind != entrySymlink {
			return false
		}
		if spec.rawTarget != "" {
			return actual.rawLinkTarget == spec.rawTarget
		}
		return canonicalPath(actual.resolvedTarget) == canonicalPath(spec.target)
	case slotCopy:
		if actual.kind != entryManagedCopy {
			return false
		}
		if canonicalPath(actual.resolvedTarget) != canonicalPath(spec.target) {
			return false
		}
		if spec.hash != "" && actual.contentHash != spec.hash {
			return false
		}
		return true
	default:
		return false
	}
}

func applySlotChange(next slotChange) (executedSlotChange, error) {
	record := executedSlotChange{change: next}

	// Fast-path for source migrate retargeting an existing clean managed copy:
	// rewrite only the marker's sourcePath atomically in place.
	if next.retargetTag && next.expected.kind == slotCopy && next.desired.kind == slotCopy {
		marker := managedCopyMarker{
			Version:     managedCopyMarkerVersion,
			SourcePath:  canonicalPath(next.desired.target),
			ContentHash: next.expected.hash,
		}
		if err := writeManagedCopyMarker(next.path, marker); err != nil {
			return executedSlotChange{}, fmt.Errorf("apply skill projection change %s: %w", next.path, err)
		}
		record.appliedHash = marker.ContentHash
		return record, nil
	}

	var stagedDir string
	if next.desired.kind == slotCopy {
		var marker managedCopyMarker
		var err error
		stagedDir, marker, err = stageManagedCopy(next.desired.target, filepath.Dir(next.path))
		if err != nil {
			return executedSlotChange{}, fmt.Errorf("apply skill projection change %s: %w", next.path, err)
		}
		record.appliedHash = marker.ContentHash
	}
	cleanupStaged := func() {
		if stagedDir != "" {
			_ = os.RemoveAll(stagedDir)
		}
	}

	// Remove or stash the existing slot entry first when replacing or deleting.
	switch next.expected.kind {
	case slotSymlink:
		if err := os.Remove(next.path); err != nil {
			cleanupStaged()
			return executedSlotChange{}, fmt.Errorf("apply skill projection change %s: %w", next.path, err)
		}
	case slotCopy:
		stash, err := reserveStashPath(filepath.Dir(next.path))
		if err != nil {
			cleanupStaged()
			return executedSlotChange{}, fmt.Errorf("apply skill projection change %s: %w", next.path, err)
		}
		if err := os.Rename(next.path, stash); err != nil {
			cleanupStaged()
			return executedSlotChange{}, fmt.Errorf("apply skill projection change %s: %w", next.path, err)
		}
		record.stashPath = stash
	}

	// Install the desired entry.
	var installErr error
	switch next.desired.kind {
	case slotAbsent:
		return record, nil
	case slotSymlink:
		installErr = os.Symlink(next.desired.target, next.path)
	case slotCopy:
		installErr = os.Rename(stagedDir, next.path)
		if installErr == nil {
			stagedDir = ""
		}
	}
	if installErr == nil {
		return record, nil
	}

	// Local recovery when installing the replacement failed after removing/stashing expected.
	cleanupStaged()
	restoreErr := restoreOriginalSlot(next.path, next.expected, record.stashPath)
	if restoreErr != nil {
		return executedSlotChange{}, errors.Join(
			fmt.Errorf("apply skill projection change %s: %w", next.path, installErr),
			restoreErr,
		)
	}
	return executedSlotChange{}, fmt.Errorf("apply skill projection change %s: %w", next.path, installErr)
}

func rollbackExecutedChange(record *executedSlotChange, useStash bool) error {
	next := record.change
	if next.retargetTag && next.expected.kind == slotCopy && next.desired.kind == slotCopy {
		actual, err := inspectEntry(next.path)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", next.path, err)
		}
		expectedCurrent := slotSpec{kind: slotCopy, target: next.desired.target, hash: record.appliedHash}
		if entryMatchesSpec(actual, next.expected) {
			return nil
		}
		if !entryMatchesSpec(actual, expectedCurrent) {
			return fmt.Errorf("preserve concurrently changed target %s", next.path)
		}
		marker := managedCopyMarker{
			Version:     managedCopyMarkerVersion,
			SourcePath:  canonicalPath(next.expected.target),
			ContentHash: next.expected.hash,
		}
		return writeManagedCopyMarker(next.path, marker)
	}

	actual, err := inspectEntry(next.path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", next.path, err)
	}
	if entryMatchesSpec(actual, next.expected) {
		return nil
	}
	appliedSpec := next.desired
	if appliedSpec.kind == slotCopy {
		appliedSpec.hash = record.appliedHash
	}
	if actual.kind != entryAbsent && !entryMatchesSpec(actual, appliedSpec) {
		return fmt.Errorf("preserve concurrently changed target %s", next.path)
	}
	if actual.kind != entryAbsent {
		if actual.kind == entrySymlink {
			if err := os.Remove(next.path); err != nil {
				return fmt.Errorf("remove %s: %w", next.path, err)
			}
		} else if err := os.RemoveAll(next.path); err != nil {
			return fmt.Errorf("remove %s: %w", next.path, err)
		}
	}
	stash := ""
	if useStash {
		stash = record.stashPath
	}
	if err := restoreOriginalSlot(next.path, next.expected, stash); err != nil {
		return err
	}
	if stash != "" {
		record.stashPath = ""
	}
	return nil
}

func restoreOriginalSlot(path string, expected slotSpec, stashPath string) error {
	switch expected.kind {
	case slotAbsent:
		return nil
	case slotSymlink:
		target := expected.rawTarget
		if target == "" {
			target = expected.target
		}
		if err := os.Symlink(target, path); err != nil {
			return fmt.Errorf("restore %s: %w", path, err)
		}
		return nil
	case slotCopy:
		if stashPath != "" {
			if err := os.Rename(stashPath, path); err != nil {
				return fmt.Errorf("restore %s (backup preserved at %s): %w", path, stashPath, err)
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("prepare restore directory for %s: %w", path, err)
		}
		staged, _, err := stageManagedCopy(expected.target, filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("restore %s: %w", path, err)
		}
		if err := os.Rename(staged, path); err != nil {
			_ = os.RemoveAll(staged)
			return fmt.Errorf("restore %s: %w", path, err)
		}
		return nil
	default:
		return nil
	}
}

func reserveStashPath(dir string) (string, error) {
	file, err := os.CreateTemp(dir, ".skills-switch-stash-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func ensureProjectionDirectory(path string) ([]string, error) {
	missing := make([]string, 0)
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%s exists and is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return missing, nil
}

func cleanupCreatedDirs(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func isStagingEntryName(name string) bool {
	return strings.HasPrefix(name, ".skills-switch-copy-") || strings.HasPrefix(name, ".skills-switch-stash-")
}
