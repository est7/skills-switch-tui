// Package linktransaction executes reversible batches of symbolic-link changes.
// Callers own projection policy and source validation; this package owns the
// filesystem transaction, concurrent-change checks, and rollback semantics.
package linktransaction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Action int

const (
	Create Action = iota
	Remove
	Replace
)

type Change struct {
	Action         Action
	Path           string
	Target         string
	OriginalTarget string
}

// TargetMatcher decides whether an on-disk symlink target is the target a
// caller planned against. The actual target is the raw value from Readlink.
type TargetMatcher func(linkPath, actualTarget, expectedTarget string) bool

type Engine struct {
	Label          string
	BeforeApply    func(Change)
	ValidateSource func(Change) error
	Conflict       func(path, reason string) error
	MatchTarget    TargetMatcher
}

// Applied is a successfully executed transaction that can later be restored.
// Restore is concurrency-safe: it never overwrites a path changed by another
// actor after the transaction completed.
type Applied struct {
	engine             Engine
	changes            []Change
	createdDirectories []string
}

func (a Applied) Restore() error {
	err := a.engine.rollback(a.changes)
	cleanupDirectories(a.createdDirectories)
	return err
}

func (e Engine) Execute(changes []Change) (Applied, error) {
	if len(changes) == 0 {
		return Applied{engine: e}, nil
	}
	executed := make([]Change, 0, len(changes))
	createdDirectories := make([]string, 0)
	for _, next := range changes {
		if e.BeforeApply != nil {
			e.BeforeApply(next)
		}
		if err := e.validate(next); err != nil {
			return Applied{}, e.fail(err, executed, createdDirectories)
		}
		if next.Action == Create {
			created, err := ensureDirectory(filepath.Dir(next.Path))
			if err != nil {
				return Applied{}, e.fail(fmt.Errorf("create %s directory: %w", e.label(), err), executed, createdDirectories)
			}
			createdDirectories = append(createdDirectories, created...)
			// Directory creation widens the race window, so recheck immediately
			// before mutating the target path.
			if err := e.validate(next); err != nil {
				return Applied{}, e.fail(err, executed, createdDirectories)
			}
		}
		if err := e.apply(next); err != nil {
			return Applied{}, e.fail(fmt.Errorf("apply %s change %s: %w", e.label(), next.Path, err), executed, createdDirectories)
		}
		executed = append(executed, next)
	}
	return Applied{
		engine:             e,
		changes:            append([]Change(nil), executed...),
		createdDirectories: append([]string(nil), createdDirectories...),
	}, nil
}

func (e Engine) validate(next Change) error {
	if next.Action != Create && next.Action != Remove && next.Action != Replace {
		return fmt.Errorf("unknown %s action %d", e.label(), next.Action)
	}
	if (next.Action == Create || next.Action == Replace) && e.ValidateSource != nil {
		if err := e.ValidateSource(next); err != nil {
			return err
		}
	}
	expected := next.Target
	if next.Action == Remove || next.Action == Replace {
		expected = originalTarget(next)
	}
	matches, exists, conflict, err := e.inspect(next.Path, expected)
	if err != nil {
		return e.conflict(next.Path, "inspect after preflight: "+err.Error())
	}
	if next.Action == Create && !exists && !conflict {
		return nil
	}
	if (next.Action == Remove || next.Action == Replace) && exists && matches && !conflict {
		return nil
	}
	return e.conflict(next.Path, "target changed after preflight")
}

func (e Engine) apply(next Change) error {
	switch next.Action {
	case Create:
		return os.Symlink(next.Target, next.Path)
	case Remove:
		return os.Remove(next.Path)
	case Replace:
		if err := os.Remove(next.Path); err != nil {
			return err
		}
		if err := os.Symlink(next.Target, next.Path); err != nil {
			_, exists, _, inspectErr := e.inspect(next.Path, next.Target)
			if inspectErr != nil {
				return errors.Join(err, fmt.Errorf("inspect failed replacement: %w", inspectErr))
			}
			if exists {
				return errors.Join(err, fmt.Errorf("preserve concurrently changed target %s", next.Path))
			}
			if restoreErr := os.Symlink(originalTarget(next), next.Path); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("restore original target: %w", restoreErr))
			}
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown %s action %d", e.label(), next.Action)
	}
}

func (e Engine) fail(applyErr error, executed []Change, createdDirectories []string) error {
	rollbackErr := e.rollback(executed)
	cleanupDirectories(createdDirectories)
	if rollbackErr == nil {
		return applyErr
	}
	return errors.Join(applyErr, fmt.Errorf("rollback %s changes: %w", e.label(), rollbackErr))
}

func (e Engine) rollback(changes []Change) error {
	var rollbackErrors []error
	for index := len(changes) - 1; index >= 0; index-- {
		next := changes[index]
		switch next.Action {
		case Create:
			matches, exists, conflict, err := e.inspect(next.Path, next.Target)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.Path, err))
				continue
			}
			if !exists {
				continue
			}
			if conflict || !matches {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.Path))
				continue
			}
			if err := os.Remove(next.Path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %s: %w", next.Path, err))
			}
		case Remove:
			original := originalTarget(next)
			matches, exists, conflict, err := e.inspect(next.Path, original)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.Path, err))
				continue
			}
			if exists {
				if conflict || !matches {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.Path))
				}
				continue
			}
			if err := os.Symlink(original, next.Path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.Path, err))
			}
		case Replace:
			original := originalTarget(next)
			originalMatches, exists, originalConflict, err := e.inspect(next.Path, original)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.Path, err))
				continue
			}
			if exists && originalMatches && !originalConflict {
				continue
			}
			newMatches, _, newConflict, err := e.inspect(next.Path, next.Target)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.Path, err))
				continue
			}
			if exists && (newConflict || !newMatches) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.Path))
				continue
			}
			if exists {
				if err := os.Remove(next.Path); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.Path, err))
					continue
				}
			}
			if err := os.Symlink(original, next.Path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.Path, err))
			}
		}
	}
	return errors.Join(rollbackErrors...)
}

func (e Engine) inspect(path, expectedTarget string) (matches, exists, conflict bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, false, nil
	}
	if err != nil {
		return false, false, false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, true, true, nil
	}
	actualTarget, err := os.Readlink(path)
	if err != nil {
		return false, true, false, err
	}
	matcher := e.MatchTarget
	if matcher == nil {
		matcher = ExactTarget
	}
	if !matcher(path, actualTarget, expectedTarget) {
		return false, true, true, nil
	}
	return true, true, false, nil
}

func ExactTarget(_ string, actualTarget, expectedTarget string) bool {
	return actualTarget == expectedTarget
}

func EquivalentTarget(linkPath, actualTarget, expectedTarget string) bool {
	resolve := func(target string) string {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(linkPath), target)
		}
		return filepath.Clean(target)
	}
	return resolve(actualTarget) == resolve(expectedTarget)
}

func (e Engine) conflict(path, reason string) error {
	if e.Conflict != nil {
		return e.Conflict(path, reason)
	}
	return fmt.Errorf("%s conflict at %s: %s", e.label(), path, reason)
}

func (e Engine) label() string {
	if strings.TrimSpace(e.Label) == "" {
		return "link transaction"
	}
	return e.Label
}

func originalTarget(change Change) string {
	if change.OriginalTarget != "" {
		return change.OriginalTarget
	}
	return change.Target
}

func ensureDirectory(path string) ([]string, error) {
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

func cleanupDirectories(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
