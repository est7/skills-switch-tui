package source

import (
	"fmt"
	"net/url"
	"strings"
)

// RepoRef is a repository reference derived from a user-supplied link. It lets a
// caller register a vendor source from just a URL by deriving the clone URL,
// source name, tracked branch, and (for a tree/blob link) an authoritative
// Skill subdirectory.
type RepoRef struct {
	CloneURL  string
	Name      string
	Branch    string
	SkillPath string
}

// ParseRepoURL normalizes a GitHub/GitLab web link, a plain repository URL, or
// an scp-style SSH remote into a RepoRef. A `/tree/<branch>/<path>` or
// `/blob/<branch>/<path>` link yields the branch and Skill subpath; other forms
// default the branch to main and leave the subpath empty. It errors only when no
// repository name can be derived.
//
// The branch is assumed to be a single path segment: a slash-containing branch
// (`tree/release/v1/...`) resolves the first segment as the branch and folds the
// rest into the Skill path. This is inherent tree-URL ambiguity — it fails
// loudly at `git submodule add -b` and is overridable with an explicit --branch.
func ParseRepoURL(raw string) (RepoRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RepoRef{}, fmt.Errorf("empty repository URL")
	}

	// scp-style SSH remote: git@host:owner/repo(.git)
	if !strings.Contains(raw, "://") && strings.Contains(raw, "@") && strings.Contains(raw, ":") {
		name := repoName(raw[strings.Index(raw, ":")+1:])
		if name == "" {
			return RepoRef{}, fmt.Errorf("cannot derive repository name from %q", raw)
		}
		return RepoRef{CloneURL: raw, Name: name, Branch: "main"}, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return RepoRef{}, fmt.Errorf("parse repository URL %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return RepoRef{}, fmt.Errorf("not an absolute repository URL: %q", raw)
	}

	segments := make([]string, 0)
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return RepoRef{}, fmt.Errorf("repository URL %q has no path", raw)
	}

	marker := -1
	for index, segment := range segments {
		if segment == "tree" || segment == "blob" {
			marker = index
			break
		}
	}

	repoSegments := segments
	branch := ""
	skillPath := ""
	if marker >= 0 {
		repoSegments = segments[:marker]
		// GitLab inserts a "-" separator before tree/blob.
		if n := len(repoSegments); n > 0 && repoSegments[n-1] == "-" {
			repoSegments = repoSegments[:n-1]
		}
		if marker+1 < len(segments) {
			branch = segments[marker+1]
		}
		if marker+2 < len(segments) {
			skillPath = strings.Join(segments[marker+2:], "/")
		}
	}
	if len(repoSegments) == 0 {
		return RepoRef{}, fmt.Errorf("repository URL %q is missing owner/repo", raw)
	}

	repoSegments[len(repoSegments)-1] = strings.TrimSuffix(repoSegments[len(repoSegments)-1], ".git")
	name := repoSegments[len(repoSegments)-1]
	if name == "" {
		return RepoRef{}, fmt.Errorf("cannot derive repository name from %q", raw)
	}
	if branch == "" {
		branch = "main"
	}
	host := parsed.Host
	if parsed.User != nil {
		host = parsed.User.String() + "@" + host
	}
	clone := parsed.Scheme + "://" + host + "/" + strings.Join(repoSegments, "/") + ".git"
	return RepoRef{CloneURL: clone, Name: name, Branch: branch, SkillPath: skillPath}, nil
}

func repoName(path string) string {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
