package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestAutoPruneAfterUpdateRemovesOrphanedProjections(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")

	// Post-update state: upstream now ships one and three; two is gone.
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "one"), "one")
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "three"), "three")

	// The project enabled one, two and three before the update.
	linkCodexSkill(t, projectRoot, sourcesRoot, "one")
	linkCodexSkill(t, projectRoot, sourcesRoot, "two")
	linkCodexSkill(t, projectRoot, sourcesRoot, "three")

	options := &rootOptions{resourcesRoot: resourceRoot, projectRoot: projectRoot, language: "en"}
	results := []source.UpdateResult{{SourceID: "vendor-shared/repo", Branch: "main", Changed: true}}

	pruned, err := autoPruneAfterUpdate(options, results)
	if err != nil {
		t.Fatalf("autoPruneAfterUpdate: %v", err)
	}
	if len(pruned) != 1 || pruned[0].Name != "two" {
		t.Fatalf("pruned = %#v, want the 'two' link", pruned)
	}

	skillsDir := filepath.Join(projectRoot, ".agents", "skills")
	if _, err := os.Lstat(filepath.Join(skillsDir, "two")); !os.IsNotExist(err) {
		t.Fatalf("orphan link 'two' not removed: %v", err)
	}
	for _, name := range []string{"one", "three"} {
		if _, err := os.Lstat(filepath.Join(skillsDir, name)); err != nil {
			t.Fatalf("live link %q was removed: %v", name, err)
		}
	}
}

func TestAutoPruneAfterUpdateSkipsWhenNotInProject(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir() // no .git: not a project
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "one"), "one")
	linkCodexSkill(t, projectRoot, sourcesRoot, "two") // would be an orphan if scanned

	options := &rootOptions{resourcesRoot: resourceRoot, projectRoot: projectRoot, language: "en"}
	results := []source.UpdateResult{{SourceID: "vendor-shared/repo", Branch: "main", Changed: true}}

	pruned, err := autoPruneAfterUpdate(options, results)
	if err != nil {
		t.Fatalf("autoPruneAfterUpdate: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %#v, want none outside a project", pruned)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "two")); err != nil {
		t.Fatalf("link removed outside a project: %v", err)
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

func TestAutoPruneAfterUpdateSkipsUnchangedSources(t *testing.T) {
	resourceRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcesRoot := filepath.Join(resourceRoot, "skills")
	writeCLISkill(t, filepath.Join(sourcesRoot, "vendor", "shared", "repo", "skills", "one"), "one")
	linkCodexSkill(t, projectRoot, sourcesRoot, "two") // orphan, but its source did not change

	options := &rootOptions{resourcesRoot: resourceRoot, projectRoot: projectRoot, language: "en"}
	results := []source.UpdateResult{{SourceID: "vendor-shared/repo", Branch: "main", Changed: false}}

	pruned, err := autoPruneAfterUpdate(options, results)
	if err != nil {
		t.Fatalf("autoPruneAfterUpdate: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %#v, want none when nothing changed", pruned)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, ".agents", "skills", "two")); err != nil {
		t.Fatalf("link removed for an unchanged source: %v", err)
	}
}
