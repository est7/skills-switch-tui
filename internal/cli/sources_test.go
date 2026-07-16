package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/est7/skills-switch-tui/internal/catalog"
	"github.com/est7/skills-switch-tui/internal/source"
)

// linkCodexSkill creates a managed-style projection symlink in the codex target
// directory pointing at a vendor skill directory (which may or may not exist).
func linkCodexSkill(t *testing.T, projectRoot, sourcesRoot, name string) {
	t.Helper()
	targetDir := filepath.Join(projectRoot, ".agents", "skills")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", name)
	if err := os.Symlink(target, filepath.Join(targetDir, name)); err != nil {
		t.Fatal(err)
	}
}

func TestMissingConfiguredSourceRemainsVisibleAndDoctorExplainsIt(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsRoot := filepath.Join(resourceRoot, "skills")
	if err := catalog.RegisterSource(skillsRoot, "vendor-shared/missing", catalog.SourcePolicy{Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	base := []string{"--resources", resourceRoot, "--project", projectRoot}
	out, err := execute(t, append(base, "source", "list", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"vendor-shared/missing", "checkout-missing"} {
		if !strings.Contains(string(out), fragment) {
			t.Fatalf("source list omitted %q:\n%s", fragment, out)
		}
	}
	out, err = execute(t, append(base, "doctor", "--json")...)
	if err == nil {
		t.Fatal("doctor reported healthy for a missing configured checkout")
	}
	for _, fragment := range []string{"vendor-shared/missing", "checkout-missing", "run source update vendor-shared/missing"} {
		if !strings.Contains(string(out), fragment) {
			t.Fatalf("doctor omitted %q:\n%s", fragment, out)
		}
	}
}

func TestUpdateJSONIncludesStructuredSourceFailures(t *testing.T) {
	err := errors.Join(
		&source.SourceError{SourceID: "vendor-shared/one", Path: "/sources/one", Operation: "fetch tracked branch", Err: errors.New("network down")},
		&source.SourceError{SourceID: "vendor-shared/two", Path: "/sources/two", Operation: "clean read-only checkout", Err: errors.New("permission denied")},
	)
	failures := updateFailures(err)
	if len(failures) != 2 {
		t.Fatalf("failures = %#v", failures)
	}
	if failures[0].Source != "vendor-shared/one" || failures[0].Operation != "fetch tracked branch" || failures[0].Error != "network down" {
		t.Fatalf("first failure = %#v", failures[0])
	}
	if failures[1].Source != "vendor-shared/two" || failures[1].Path != "/sources/two" {
		t.Fatalf("second failure = %#v", failures[1])
	}
}

func TestSkillsPruneListsThenRemovesOrphans(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "one"), "one")
	linkCodexSkill(t, projectRoot, sourcesRoot, "one") // live
	linkCodexSkill(t, projectRoot, sourcesRoot, "two") // orphan: no such skill
	base := []string{"--resources", resourceRoot, "--project", projectRoot}
	orphanLink := filepath.Join(projectRoot, ".agents", "skills", "two")

	// Default run lists the orphan without removing it.
	out, err := execute(t, append(base, "skills", "prune")...)
	if err != nil {
		t.Fatalf("prune list: %v", err)
	}
	if !strings.Contains(string(out), "two") {
		t.Fatalf("prune list did not mention the orphan:\n%s", out)
	}
	if _, err := os.Lstat(orphanLink); err != nil {
		t.Fatalf("prune without --yes removed the link: %v", err)
	}

	// --yes removes the orphan and keeps the live link.
	if _, err := execute(t, append(base, "skills", "prune", "--yes")...); err != nil {
		t.Fatalf("prune apply: %v", err)
	}
	if _, err := os.Lstat(orphanLink); !os.IsNotExist(err) {
		t.Fatalf("orphan link not removed by --yes: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "one")); err != nil {
		t.Fatalf("live link removed by prune: %v", err)
	}
}

func TestDoctorReportsOrphanedProjection(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "one"), "one")
	linkCodexSkill(t, projectRoot, sourcesRoot, "two") // orphan
	base := []string{"--resources", resourceRoot, "--project", projectRoot}

	out, err := execute(t, append(base, "doctor")...)
	if err == nil {
		t.Fatalf("doctor reported healthy despite an orphaned projection:\n%s", out)
	}
	if !strings.Contains(string(out), "orphaned") || !strings.Contains(string(out), "two") {
		t.Fatalf("doctor did not surface the orphan:\n%s", out)
	}
}
