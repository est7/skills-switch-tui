package projection

import (
	"path/filepath"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/client"
)

// staleFixture models a project projected before its vendor source moved from
// vendor/shared/<repo> to vendor/shared/<owner>/<repo> while another project
// was the one being migrated.
type staleFixture struct {
	projectRoot string
	userHome    string
	skill       catalog.Skill
	legacyPath  string
	manager     Manager
}

func newStaleFixture(t *testing.T) staleFixture {
	t.Helper()
	skillsRoot := t.TempDir()
	sourceRoot := filepath.Join(skillsRoot, "vendor", "shared", "owner", "repo")
	skill := newSkill(t, sourceRoot, "tool")
	missingRoot := filepath.Join(skillsRoot, "vendor", "shared", "unchecked")
	loaded := catalog.Catalog{
		Root:    skillsRoot,
		Clients: client.DefaultRegistry(),
		Sources: []catalog.Source{
			{ID: "vendor-shared/owner/repo", Kind: catalog.SourceVendor, Path: sourceRoot, Skills: []catalog.Skill{skill}},
			{ID: "vendor-shared/unchecked", Kind: catalog.SourceVendor, Path: missingRoot, Availability: catalog.SourceCheckoutMissing},
		},
	}
	fixture := staleFixture{
		projectRoot: t.TempDir(),
		userHome:    t.TempDir(),
		skill:       skill,
		legacyPath:  filepath.Join(skillsRoot, "vendor", "shared", "repo", "tool"),
	}
	fixture.manager = NewWithUserHome(fixture.projectRoot, fixture.userHome, loaded)
	return fixture
}

func (f staleFixture) projectLink() string {
	return filepath.Join(f.projectRoot, ".agents", "skills", f.skill.Name)
}

func TestStaleLinkYieldsToProjectEnable(t *testing.T) {
	f := newStaleFixture(t)
	linkRaw(t, f.projectRoot, f.skill.Name, f.legacyPath)

	if state, err := f.manager.State(f.skill, catalog.ClientCodex); err != nil || state != StateDisabled {
		t.Fatalf("state = %q, %v; want disabled behind a stale link", state, err)
	}
	if err := f.manager.SetEnabled([]catalog.Skill{f.skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}
	assertLinkTarget(t, f.projectLink(), f.skill.Path)
}

func TestStaleLinkIsRetiredByGlobalPromotion(t *testing.T) {
	f := newStaleFixture(t)
	linkRaw(t, f.projectRoot, f.skill.Name, f.legacyPath)

	if err := f.manager.SetEnabledAt([]catalog.Skill{f.skill}, catalog.ClientCodex, true, ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, f.projectLink())
	assertLinkTarget(t, filepath.Join(f.userHome, ".agents", "skills", f.skill.Name), f.skill.Path)
}

func TestStaleProjectLinkIsNotAGlobalDuplicate(t *testing.T) {
	f := newStaleFixture(t)
	if err := f.manager.SetEnabledAt([]catalog.Skill{f.skill}, catalog.ClientCodex, true, ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	linkRaw(t, f.projectRoot, f.skill.Name, f.legacyPath)

	if state, err := f.manager.State(f.skill, catalog.ClientCodex); err != nil || state != StateGlobal {
		t.Fatalf("state = %q, %v; want global", state, err)
	}
}

func TestDanglingLinksThatAreNotStaleStillBlock(t *testing.T) {
	cases := map[string]func(f staleFixture) string{
		// A registered source whose checkout is missing may come back; its
		// links are not stale.
		"registered missing checkout": func(f staleFixture) string {
			return filepath.Join(f.manager.sourceRoots[1], f.skill.Name)
		},
		// Outside the resources skills root the link is not a projection
		// this tool could have made.
		"outside the skills root": func(f staleFixture) string {
			return filepath.Join(f.projectRoot, "gone", f.skill.Name)
		},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			f := newStaleFixture(t)
			dangling := target(f)
			linkRaw(t, f.projectRoot, f.skill.Name, dangling)

			if state, err := f.manager.State(f.skill, catalog.ClientCodex); err != nil || state != StateConflict {
				t.Fatalf("state = %q, %v; want conflict", state, err)
			}
			if err := f.manager.SetEnabled([]catalog.Skill{f.skill}, catalog.ClientCodex, true); err == nil {
				t.Fatal("enable succeeded over a link that is not stale")
			}
			if err := f.manager.SetEnabledAt([]catalog.Skill{f.skill}, catalog.ClientCodex, true, ScopeGlobal); err == nil {
				t.Fatal("global promotion removed a link that is not stale")
			}
			assertLinkTarget(t, f.projectLink(), dangling)
		})
	}
}

func TestPruneRemovesStaleLinksOnly(t *testing.T) {
	f := newStaleFixture(t)
	linkRaw(t, f.projectRoot, "legacy", f.legacyPath)
	unchecked := filepath.Join(f.manager.sourceRoots[1], "kept")
	linkRaw(t, f.projectRoot, "unchecked", unchecked)
	outside := filepath.Join(f.projectRoot, "gone")
	linkRaw(t, f.projectRoot, "outside", outside)
	if err := f.manager.SetEnabled([]catalog.Skill{f.skill}, catalog.ClientCodex, true); err != nil {
		t.Fatal(err)
	}

	orphans, err := f.manager.OrphanedProjectionsAt(nil, ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].Name != "legacy" || orphans[0].Target != f.legacyPath {
		t.Fatalf("orphans = %#v, want only the stale legacy link", orphans)
	}
	refreshed, err := f.manager.OrphanedProjectionsAfterRefreshAt(nil, ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed) != 0 {
		t.Fatalf("post-update cleanup = %#v, want stale links left to explicit prune", refreshed)
	}
	if _, err := f.manager.PruneOrphans(orphans); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.projectRoot, ".agents", "skills")
	assertMissing(t, filepath.Join(dir, "legacy"))
	assertLinkTarget(t, filepath.Join(dir, "unchecked"), unchecked)
	assertLinkTarget(t, filepath.Join(dir, "outside"), outside)
	assertLinkTarget(t, f.projectLink(), f.skill.Path)
}
