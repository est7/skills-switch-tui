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

type action int

const (
	invalidAction action = iota
	createAction
	removeAction
	replaceAction
)

type Change struct {
	action         action
	path           string
	target         string
	expectedTarget string
}

func Create(path, target string) Change {
	return Change{action: createAction, path: path, target: target}
}

func Remove(path, expectedTarget string) Change {
	return Change{action: removeAction, path: path, expectedTarget: expectedTarget}
}

func Replace(path, expectedTarget, target string) Change {
	return Change{action: replaceAction, path: path, target: target, expectedTarget: expectedTarget}
}

// TargetMatcher decides whether an on-disk symlink target is the target a
// caller planned against. The actual target is the raw value from Readlink.
type TargetMatcher func(linkPath, actualTarget, expectedTarget string) bool

type Engine struct {
	Label          string
	ValidateTarget func(string) error
	Conflict       func(path, reason string) error
	MatchTarget    TargetMatcher
	beforeApply    func(Change)
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
		if e.beforeApply != nil {
			e.beforeApply(next)
		}
		if err := e.validate(next); err != nil {
			return Applied{}, e.fail(err, executed, createdDirectories)
		}
		if next.action == createAction {
			created, err := ensureDirectory(filepath.Dir(next.path))
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
			return Applied{}, e.fail(fmt.Errorf("apply %s change %s: %w", e.label(), next.path, err), executed, createdDirectories)
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
	if err := next.validate(); err != nil {
		return fmt.Errorf("invalid %s change: %w", e.label(), err)
	}
	if (next.action == createAction || next.action == replaceAction) && e.ValidateTarget != nil {
		if err := e.ValidateTarget(next.target); err != nil {
			return err
		}
	}
	expected := next.target
	if next.action == removeAction || next.action == replaceAction {
		expected = next.expectedTarget
	}
	matches, exists, conflict, err := e.inspect(next.path, expected)
	if err != nil {
		return e.conflict(next.path, "inspect after preflight: "+err.Error())
	}
	if next.action == createAction && !exists && !conflict {
		return nil
	}
	if (next.action == removeAction || next.action == replaceAction) && exists && matches && !conflict {
		return nil
	}
	return e.conflict(next.path, "target changed after preflight")
}

func (e Engine) apply(next Change) error {
	switch next.action {
	case createAction:
		return os.Symlink(next.target, next.path)
	case removeAction:
		return os.Remove(next.path)
	case replaceAction:
		if err := os.Remove(next.path); err != nil {
			return err
		}
		if err := os.Symlink(next.target, next.path); err != nil {
			_, exists, _, inspectErr := e.inspect(next.path, next.target)
			if inspectErr != nil {
				return errors.Join(err, fmt.Errorf("inspect failed replacement: %w", inspectErr))
			}
			if exists {
				return errors.Join(err, fmt.Errorf("preserve concurrently changed target %s", next.path))
			}
			if restoreErr := os.Symlink(next.expectedTarget, next.path); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("restore original target: %w", restoreErr))
			}
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown %s action %d", e.label(), next.action)
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
		switch next.action {
		case createAction:
			matches, exists, conflict, err := e.inspect(next.path, next.target)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.path, err))
				continue
			}
			if !exists {
				continue
			}
			if conflict || !matches {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.path))
				continue
			}
			if err := os.Remove(next.path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %s: %w", next.path, err))
			}
		case removeAction:
			original := next.expectedTarget
			matches, exists, conflict, err := e.inspect(next.path, original)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.path, err))
				continue
			}
			if exists {
				if conflict || !matches {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.path))
				}
				continue
			}
			if err := os.Symlink(original, next.path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.path, err))
			}
		case replaceAction:
			original := next.expectedTarget
			originalMatches, exists, originalConflict, err := e.inspect(next.path, original)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.path, err))
				continue
			}
			if exists && originalMatches && !originalConflict {
				continue
			}
			newMatches, _, newConflict, err := e.inspect(next.path, next.target)
			if err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("inspect %s: %w", next.path, err))
				continue
			}
			if exists && (newConflict || !newMatches) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserve concurrently changed target %s", next.path))
				continue
			}
			if exists {
				if err := os.Remove(next.path); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.path, err))
					continue
				}
			}
			if err := os.Symlink(original, next.path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", next.path, err))
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

func (c Change) validate() error {
	if c.action != createAction && c.action != removeAction && c.action != replaceAction {
		return errors.New("change must be created with Create, Remove, or Replace")
	}
	if c.path == "" {
		return errors.New("path is required")
	}
	switch c.action {
	case createAction:
		if c.target == "" {
			return errors.New("create target is required")
		}
	case removeAction:
		if c.expectedTarget == "" {
			return errors.New("remove expected target is required")
		}
	case replaceAction:
		if c.expectedTarget == "" || c.target == "" {
			return errors.New("replace expected and new targets are required")
		}
	}
	return nil
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
