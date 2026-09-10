package source

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// SourceRef is a normalized reference to a vendor source derived from a
// user-supplied input: the clone URL, source name, tracked branch, and (for a
// tree/blob link or an owner/repo/subpath shorthand) a subfolder to scope the
// source to. The subfolder is handed to discovery as a container --skill-path,
// so it may name a plugin directory, a skills/ tree, or a single skill.
//
// Name is the source's scope-relative path, "owner/repo", so two repositories
// that share a repository name stay distinct. A remote whose path holds a single
// segment has no owner to qualify it with and keeps the bare repository name.
type SourceRef struct {
	CloneURL string
	Name     string
	Branch   string
	Subpath  string
}

// ParseSourceRef normalizes the ways a user can name a source into a SourceRef.
// Forms are recognized in this order (order matters so a shorthand never
// swallows a URL or an scp remote):
//
//   - github:owner/repo, gitlab:owner/repo         prefix, rewritten to a URL
//   - https://host/owner/repo[/tree|blob/<b>/<p>]  web link (incl. gitlab /-/tree/)
//   - https://host/owner/repo.git, ssh://…         plain URL (userinfo/port kept)
//   - git@host:owner/repo(.git)                    scp-style SSH remote
//   - owner/repo, owner/repo/sub/path              GitHub shorthand
//
// Name defaults to the remote's last two path segments, owner/repo; Branch
// defaults to main unless a tree/blob link names one; Subpath is the subfolder
// with any ".." segment rejected. It errors when no repository name can be
// derived. Local filesystem paths are intentionally not accepted here — author
// local skills with `skills create`.
func ParseSourceRef(input string) (SourceRef, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return SourceRef{}, fmt.Errorf("empty source reference")
	}
	if rest, ok := strings.CutPrefix(input, "github:"); ok {
		return parseShorthand("github.com", rest)
	}
	if rest, ok := strings.CutPrefix(input, "gitlab:"); ok {
		return parseShorthand("gitlab.com", rest)
	}
	switch {
	case strings.Contains(input, "://"):
		return parseSourceURL(input)
	case isSCPRemote(input):
		return parseSCPRemote(input)
	case isShorthand(input):
		return parseShorthand("github.com", input)
	}
	return SourceRef{}, fmt.Errorf("unrecognized source reference %q: use owner/repo, a URL, or a git remote", input)
}

// isSCPRemote reports whether input is an scp-style git remote. The userinfo is
// optional — `host:owner/repo` is as valid a remote as `git@host:owner/repo` —
// so this shares one definition with remote identity rather than keeping a
// second, stricter rule that would make a valid remote unrecognizable here.
func isSCPRemote(input string) bool {
	_, _, ok := cutSCPRemote(input)
	return ok
}

// isShorthand reports whether input is an owner/repo[/subpath] GitHub shorthand.
// The guards exclude URLs, scp remotes, Windows paths (all contain ':'), and
// local paths (leading '.'/'/'), and require at least an owner/repo pair.
func isShorthand(input string) bool {
	if strings.ContainsAny(input, ":") || strings.HasPrefix(input, ".") || strings.HasPrefix(input, "/") {
		return false
	}
	return strings.Count(strings.Trim(input, "/"), "/") >= 1
}

func parseShorthand(host, rest string) (SourceRef, error) {
	segments := splitPathSegments(rest)
	if len(segments) < 2 || segments[0] == "" {
		return SourceRef{}, fmt.Errorf("shorthand %q must be owner/repo", rest)
	}
	owner := segments[0]
	repo := strings.TrimSuffix(segments[1], ".git")
	if repo == "" {
		return SourceRef{}, fmt.Errorf("shorthand %q must be owner/repo", rest)
	}
	subpath, err := sanitizeSubpath(strings.Join(segments[2:], "/"))
	if err != nil {
		return SourceRef{}, err
	}
	return SourceRef{
		CloneURL: "https://" + host + "/" + owner + "/" + repo + ".git",
		Name:     owner + "/" + repo,
		Branch:   "main",
		Subpath:  subpath,
	}, nil
}

func parseSCPRemote(input string) (SourceRef, error) {
	_, path, _ := cutSCPRemote(input)
	segments := splitPathSegments(strings.TrimSuffix(path, ".git"))
	name := qualifiedName(segments)
	if name == "" {
		return SourceRef{}, fmt.Errorf("cannot derive repository name from %q", input)
	}
	return SourceRef{CloneURL: input, Name: name, Branch: "main"}, nil
}

func parseSourceURL(input string) (SourceRef, error) {
	parsed, err := url.Parse(input)
	if err != nil {
		return SourceRef{}, fmt.Errorf("parse repository URL %q: %w", input, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return SourceRef{}, fmt.Errorf("not an absolute repository URL: %q", input)
	}
	segments := splitPathSegments(parsed.Path)
	if len(segments) == 0 {
		return SourceRef{}, fmt.Errorf("repository URL %q has no path", input)
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
	rawSubpath := ""
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
			rawSubpath = strings.Join(segments[marker+2:], "/")
		}
	}
	if len(repoSegments) == 0 {
		return SourceRef{}, fmt.Errorf("repository URL %q is missing owner/repo", input)
	}
	repoSegments[len(repoSegments)-1] = strings.TrimSuffix(repoSegments[len(repoSegments)-1], ".git")
	name := qualifiedName(repoSegments)
	if name == "" {
		return SourceRef{}, fmt.Errorf("cannot derive repository name from %q", input)
	}
	subpath, err := sanitizeSubpath(rawSubpath)
	if err != nil {
		return SourceRef{}, err
	}
	if branch == "" {
		branch = "main"
	}
	host := parsed.Host
	if parsed.User != nil {
		host = parsed.User.String() + "@" + host
	}
	clone := parsed.Scheme + "://" + host + "/" + strings.Join(repoSegments, "/") + ".git"
	return SourceRef{CloneURL: clone, Name: name, Branch: branch, Subpath: subpath}, nil
}

// sanitizeSubpath cleans a slash-separated subpath and rejects any ".." segment.
// A link that lands on a SKILL.md file names the skill it belongs to, so the
// subpath is the file's directory: discovery scopes to directories, never files.
func sanitizeSubpath(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	cleaned := path.Clean(raw)
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == ".." {
			return "", fmt.Errorf("subpath %q must not contain %q", raw, "..")
		}
	}
	if path.Base(cleaned) == "SKILL.md" {
		cleaned = path.Dir(cleaned)
		if cleaned == "." {
			return "", nil
		}
	}
	return cleaned, nil
}

func splitPathSegments(raw string) []string {
	segments := make([]string, 0)
	for _, segment := range strings.Split(raw, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

// qualifiedName joins a remote's owner and repository segments into the
// scope-relative source name. A single-segment path has no owner to qualify the
// repository with and keeps the bare name; an empty repository segment yields ""
// so the caller can reject the reference.
func qualifiedName(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	repo := segments[len(segments)-1]
	if repo == "" {
		return ""
	}
	if len(segments) == 1 || segments[len(segments)-2] == "" {
		return repo
	}
	return segments[len(segments)-2] + "/" + repo
}
