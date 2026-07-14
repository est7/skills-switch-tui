package source

import "testing"

func TestParseSourceRef(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		clone   string
		id      string
		branch  string
		subpath string
	}{
		{
			name:   "owner/repo shorthand",
			input:  "DannyMac180/skills",
			clone:  "https://github.com/DannyMac180/skills.git",
			id:     "skills",
			branch: "main",
		},
		{
			name:    "owner/repo/subpath shorthand",
			input:   "lihang/android-cc-plugin/plugins/android-debug-tools",
			clone:   "https://github.com/lihang/android-cc-plugin.git",
			id:      "android-cc-plugin",
			branch:  "main",
			subpath: "plugins/android-debug-tools",
		},
		{
			name:   "github: prefix",
			input:  "github:owner/repo",
			clone:  "https://github.com/owner/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:   "gitlab: prefix",
			input:  "gitlab:group/repo",
			clone:  "https://gitlab.com/group/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:    "github tree with subpath",
			input:   "https://github.com/DannyMac180/skills/tree/main/codex-dynamic-workflows",
			clone:   "https://github.com/DannyMac180/skills.git",
			id:      "skills",
			branch:  "main",
			subpath: "codex-dynamic-workflows",
		},
		{
			name:    "github blob on a feature branch",
			input:   "https://github.com/owner/repo/blob/feat/skills/a",
			clone:   "https://github.com/owner/repo.git",
			id:      "repo",
			branch:  "feat",
			subpath: "skills/a",
		},
		{
			name:   "plain github repo without .git",
			input:  "https://github.com/owner/repo",
			clone:  "https://github.com/owner/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:   "plain repo with .git",
			input:  "https://example.com/owner/repo.git",
			clone:  "https://example.com/owner/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:    "gitlab subgroup tree",
			input:   "https://gitlab.com/group/sub/repo/-/tree/main/skills",
			clone:   "https://gitlab.com/group/sub/repo.git",
			id:      "repo",
			branch:  "main",
			subpath: "skills",
		},
		{
			name:   "scp ssh remote",
			input:  "git@github.com:owner/repo.git",
			clone:  "git@github.com:owner/repo.git",
			id:     "repo",
			branch: "main",
		},
		{
			name:    "ssh url with port, userinfo, and tree subpath",
			input:   "ssh://git@gitlab-code.v.show:1022/lihang/android-cc-plugin/-/tree/main/plugins/x",
			clone:   "ssh://git@gitlab-code.v.show:1022/lihang/android-cc-plugin.git",
			id:      "android-cc-plugin",
			branch:  "main",
			subpath: "plugins/x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := ParseSourceRef(tc.input)
			if err != nil {
				t.Fatalf("ParseSourceRef(%q): %v", tc.input, err)
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
			if ref.Subpath != tc.subpath {
				t.Errorf("subpath = %q, want %q", ref.Subpath, tc.subpath)
			}
		})
	}

	// Local paths and other non-source inputs are rejected; a ".." subpath is
	// refused rather than silently normalized.
	for _, invalid := range []string{
		"", "   ", "./my-local-skills", "/abs/path", "notaurl",
		"https://github.com/", "owner", "github:owner",
		"https://github.com/o/r/tree/main/../../etc",
	} {
		if _, err := ParseSourceRef(invalid); err == nil {
			t.Errorf("ParseSourceRef(%q) succeeded, want error", invalid)
		}
	}
}
