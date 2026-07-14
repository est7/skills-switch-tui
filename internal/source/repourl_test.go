package source

import "testing"

func TestParseRepoURL(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		clone  string
		id     string
		branch string
		path   string
	}{
		{
			name:   "github tree with subpath",
			raw:    "https://github.com/DannyMac180/skills/tree/main/codex-dynamic-workflows",
			clone:  "https://github.com/DannyMac180/skills.git",
			id:     "skills",
			branch: "main",
			path:   "codex-dynamic-workflows",
		},
		{
			name:   "github blob on a feature branch",
			raw:    "https://github.com/owner/repo/blob/feat/x/skills/a",
			clone:  "https://github.com/owner/repo.git",
			id:     "repo",
			branch: "feat",
			path:   "x/skills/a",
		},
		{
			name:   "plain github repo without .git",
			raw:    "https://github.com/owner/repo",
			clone:  "https://github.com/owner/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:   "plain repo with .git",
			raw:    "https://example.com/owner/repo.git",
			clone:  "https://example.com/owner/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:   "gitlab subgroup tree",
			raw:    "https://gitlab.com/group/sub/repo/-/tree/main/skills",
			clone:  "https://gitlab.com/group/sub/repo.git",
			id:     "repo",
			branch: "main",
			path:   "skills",
		},
		{
			name:   "scp ssh remote",
			raw:    "git@github.com:owner/repo.git",
			clone:  "git@github.com:owner/repo.git",
			id:     "repo",
			branch: "main",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := ParseRepoURL(tc.raw)
			if err != nil {
				t.Fatalf("ParseRepoURL(%q): %v", tc.raw, err)
			}
			if ref.CloneURL != tc.clone {
				t.Errorf("clone = %q, want %q", ref.CloneURL, tc.clone)
			}
			if ref.Name != tc.id {
				t.Errorf("name = %q, want %q", ref.Name, tc.id)
			}
			if ref.Branch != tc.branch {
				t.Errorf("branch = %q, want %q", ref.Branch, tc.branch)
			}
			if ref.SkillPath != tc.path {
				t.Errorf("skillPath = %q, want %q", ref.SkillPath, tc.path)
			}
		})
	}

	for _, invalid := range []string{"", "   ", "https://github.com/", "notaurl"} {
		if _, err := ParseRepoURL(invalid); err == nil {
			t.Errorf("ParseRepoURL(%q) succeeded, want error", invalid)
		}
	}
}
