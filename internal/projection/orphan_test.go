package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

func TestOrphanedProjectionsDetectsUpstreamRemoval(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	one := newSkill(t, sourceRoot, "one")
	two := newSkill(t, sourceRoot, "two")
	three := newSkill(t, sourceRoot, "three")

	// The project enabled 1,2,3 while upstream still shipped them.
	before := sourceWith(sourceRoot, one, two, three)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{before}})
	if err := manager.SetEnabled([]catalog.Skill{one, two, three}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}

	// Upstream dropped 2 and added 4: skill two's directory is gone, four is new.
	if err := os.RemoveAll(two.Path); err != nil {
		t.Fatal(err)
	}
	four := newSkill(t, sourceRoot, "four")
	after := sourceWith(sourceRoot, one, three, four)

	orphans, err := manager.OrphanedProjections([]catalog.Source{after})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 {
		t.Fatalf("orphans = %#v, want exactly the removed skill", orphans)
	}
	orphan := orphans[0]
	if orphan.Name != "two" || orphan.Client != catalog.ClientCodex {
		t.Fatalf("orphan = %#v, want the 'two' codex link", orphan)
	}
	if orphan.Target != filepath.Clean(two.Path) {
		t.Fatalf("orphan.Target = %q, want %q", orphan.Target, two.Path)
	}
}

func TestOrphanedProjectionsSkipsEmptySource(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	one := newSkill(t, sourceRoot, "one")
	before := sourceWith(sourceRoot, one)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{before}})
	if err := manager.SetEnabled([]catalog.Skill{one}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}

	// An uninitialized submodule discovers zero skills even though the project
	// still links into it: this must never be read as "every skill removed".
	empty := catalog.Source{ID: before.ID, Kind: catalog.SourceVendor, Path: sourceRoot}
	orphans, err := manager.OrphanedProjections([]catalog.Source{empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %#v, want none for an empty (unavailable) source", orphans)
	}
}

func TestOrphanedProjectionsAfterRefreshDetectsEmptySourceRemoval(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	one := newSkill(t, sourceRoot, "one")
	before := sourceWith(sourceRoot, one)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{before}})
	if err := manager.SetEnabled([]catalog.Skill{one}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(one.Path); err != nil {
		t.Fatal(err)
	}

	refreshed := catalog.Source{ID: before.ID, Kind: catalog.SourceVendor, Path: sourceRoot}
	orphans, err := manager.OrphanedProjectionsAfterRefreshAt([]catalog.Source{refreshed}, ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].Name != "one" {
		t.Fatalf("orphans = %#v, want the last removed skill", orphans)
	}
}

func TestOrphanedProjectionsAfterRefreshRejectsMissingCheckout(t *testing.T) {
	projectRoot := t.TempDir()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry()})

	_, err := manager.OrphanedProjectionsAfterRefreshAt([]catalog.Source{{
		ID: "vendor-shared/missing", Kind: catalog.SourceVendor, Path: missingRoot,
	}}, ScopeProject)
	if err == nil || !strings.Contains(err.Error(), "vendor-shared/missing") {
		t.Fatalf("error = %v, want missing refreshed checkout attribution", err)
	}
}

func TestOrphanedProjectionsIgnoresPresentButUndiscoveredTarget(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	one := newSkill(t, sourceRoot, "one")
	// 'two' still exists on disk with its SKILL.md, but the source no longer
	// discovers it (e.g. a narrowed discovery scope). It must NOT be pruned:
	// only genuinely deleted (dangling) targets are orphans.
	two := newSkill(t, sourceRoot, "two")

	before := sourceWith(sourceRoot, one, two)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{before}})
	if err := manager.SetEnabled([]catalog.Skill{one, two}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}

	// Discovery now reports only 'one'; 'two' is still present on disk.
	narrowed := sourceWith(sourceRoot, one)
	orphans, err := manager.OrphanedProjections([]catalog.Source{narrowed})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %#v, want none while the target is still on disk", orphans)
	}
}

func TestOrphanedProjectionsRespectsSourceRootBoundary(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	// The source lives at <root>/foo and owns one skill.
	foo := newSkill(t, filepath.Join(sourceRoot, "foo"), "kept")
	source := sourceWith(filepath.Join(sourceRoot, "foo"), foo)

	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{source}})
	if err := manager.SetEnabled([]catalog.Skill{foo}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	// A dangling link into a SIBLING directory that shares the "foo" prefix
	// ("foobar") must not be attributed to source "foo".
	linkRaw(t, projectRoot, "sibling", filepath.Join(sourceRoot, "foobar", "skills", "x"))

	orphans, err := manager.OrphanedProjections([]catalog.Source{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %#v, want none (sibling-prefix link is out of scope)", orphans)
	}
}

func TestOrphanedProjectionsIgnoresOutOfScopeLinks(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	otherRoot := t.TempDir()
	mine := newSkill(t, sourceRoot, "mine")
	external := newSkill(t, otherRoot, "external")

	source := sourceWith(sourceRoot, mine)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{source}})
	if err := manager.SetEnabled([]catalog.Skill{mine}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	// A link owned by a different source and a loose link pointing at nothing
	// under any source are both out of the scanned source's scope.
	linkRaw(t, projectRoot, "external", external.Path)
	linkRaw(t, projectRoot, "loose", t.TempDir())

	orphans, err := manager.OrphanedProjections([]catalog.Source{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %#v, want none (mine live; other links out of scope)", orphans)
	}
}

func TestPruneOrphansRemovesDetectedLinksAndKeepsLive(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	gone := newSkill(t, sourceRoot, "gone")
	kept := newSkill(t, sourceRoot, "kept")
	before := sourceWith(sourceRoot, gone, kept)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{before}})
	if err := manager.SetEnabled([]catalog.Skill{gone, kept}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone.Path); err != nil {
		t.Fatal(err)
	}
	after := sourceWith(sourceRoot, kept)

	orphans, err := manager.OrphanedProjections([]catalog.Source{after})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := manager.PruneOrphans(orphans)
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if len(removed) != 1 || removed[0].Name != "gone" {
		t.Fatalf("removed = %#v, want the 'gone' link", removed)
	}
	assertMissing(t, filepath.Join(projectRoot, ".agents", "skills", "gone"))
	assertLinkTarget(t, filepath.Join(projectRoot, ".agents", "skills", "kept"), kept.Path)
}

func TestPruneOrphansPreservesLinkReplacedSinceDetection(t *testing.T) {
	projectRoot := t.TempDir()
	sourceRoot := t.TempDir()
	gone := newSkill(t, sourceRoot, "gone")
	survivor := newSkill(t, sourceRoot, "survivor")
	before := sourceWith(sourceRoot, gone, survivor)
	manager := New(projectRoot, catalog.Catalog{Clients: client.DefaultRegistry(), Sources: []catalog.Source{before}})
	if err := manager.SetEnabled([]catalog.Skill{gone, survivor}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone.Path); err != nil {
		t.Fatal(err)
	}
	after := sourceWith(sourceRoot, survivor)

	orphans, err := manager.OrphanedProjections([]catalog.Source{after})
	if err != nil {
		t.Fatal(err)
	}

	// A user replaces the orphan link with their own file before pruning runs.
	linkPath := filepath.Join(projectRoot, ".agents", "skills", "gone")
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte("user-owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := manager.PruneOrphans(orphans)
	if err == nil {
		t.Fatal("PruneOrphans succeeded despite a replaced link")
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %#v, want none", removed)
	}
	contents, readErr := os.ReadFile(linkPath)
	if readErr != nil || !strings.Contains(string(contents), "user-owned") {
		t.Fatalf("user file not preserved: contents=%q err=%v", contents, readErr)
	}
}

func sourceWith(root string, skills ...catalog.Skill) catalog.Source {
	return catalog.Source{ID: "vendor-shared/test", Kind: catalog.SourceVendor, Path: root, Skills: skills}
}

func linkRaw(t *testing.T, projectRoot, name, target string) {
	t.Helper()
	dir := filepath.Join(projectRoot, ".agents", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}
