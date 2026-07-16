package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/projection"
)

// Lifecycle owns the cross-module transaction around source mutations. The Git
// manager remains responsible for repository mechanics; Lifecycle keeps source
// state, catalog discovery, and project/global projections coherent for every
// caller, including both the CLI and TUI.
type Lifecycle struct {
	Manager     Manager
	ProjectRoot string
	UserHome    string
}

type UpdateOutcome struct {
	Results []UpdateResult
	Catalog catalog.Catalog
	Pruned  []projection.Orphan
}

func (l Lifecycle) Update(ctx context.Context, sources []catalog.Source, dryRun bool) (UpdateOutcome, error) {
	results, updateErr := l.Manager.Update(ctx, sources, dryRun)
	if dryRun {
		return UpdateOutcome{Results: results}, updateErr
	}
	outcome, reconcileErr := l.reconcileUpdated(results)
	return outcome, errors.Join(updateErr, reconcileErr)
}

func (l Lifecycle) reconcileUpdated(results []UpdateResult) (UpdateOutcome, error) {
	outcome := UpdateOutcome{Results: results}
	loaded, reloadErr := catalog.Load(l.Manager.SkillsRoot, l.Manager.Clients)
	if reloadErr != nil {
		return outcome, fmt.Errorf("reload catalog after source update: %w", reloadErr)
	}
	outcome.Catalog = loaded

	changed := make(map[string]bool)
	for _, result := range results {
		if result.Changed {
			changed[result.SourceID] = true
		}
	}
	if len(changed) == 0 {
		return outcome, nil
	}

	refreshed := make([]catalog.Source, 0, len(changed))
	for _, source := range loaded.Sources {
		if changed[source.ID] {
			refreshed = append(refreshed, source)
		}
	}
	if len(refreshed) != len(changed) {
		missing := make([]error, 0)
		for sourceID := range changed {
			if _, ok := loaded.Source(sourceID); !ok {
				missing = append(missing, fmt.Errorf("refreshed source %s disappeared from catalog", sourceID))
			}
		}
		return outcome, errors.Join(missing...)
	}

	manager := projection.NewWithUserHome(l.ProjectRoot, l.UserHome, loaded)
	orphans := make([]projection.Orphan, 0)
	if l.ProjectRoot != "" {
		projectOrphans, err := manager.OrphanedProjectionsAfterRefreshAt(refreshed, projection.ScopeProject)
		if err != nil {
			return outcome, fmt.Errorf("reconcile project projections: %w", err)
		}
		orphans = append(orphans, projectOrphans...)
	}
	if l.UserHome != "" {
		globalOrphans, err := manager.OrphanedProjectionsAfterRefreshAt(refreshed, projection.ScopeGlobal)
		if err != nil {
			return outcome, fmt.Errorf("reconcile global projections: %w", err)
		}
		orphans = append(orphans, globalOrphans...)
	}
	pruned, pruneErr := manager.PruneOrphans(orphans)
	outcome.Pruned = pruned
	return outcome, pruneErr
}

func (l Lifecycle) Remove(ctx context.Context, source catalog.Source) error {
	manager := projection.NewWithUserHome(l.ProjectRoot, l.UserHome, catalog.Catalog{
		Clients: l.Manager.Clients,
		Sources: []catalog.Source{source},
	})
	scopes := make([]projection.Scope, 0, 2)
	if l.ProjectRoot != "" {
		scopes = append(scopes, projection.ScopeProject)
	}
	if l.UserHome != "" {
		scopes = append(scopes, projection.ScopeGlobal)
	}
	retirement, err := manager.RetireSource(source, scopes...)
	if err != nil {
		return fmt.Errorf("retire source projections before removal: %w", err)
	}
	if err := l.Manager.Remove(ctx, source); err != nil {
		if restoreErr := retirement.Restore(); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore source projections after failed removal: %w", restoreErr))
		}
		return err
	}
	return nil
}
