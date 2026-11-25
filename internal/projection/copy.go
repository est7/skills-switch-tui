package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	managedCopyMarkerFile    = ".skills-switch-managed.json"
	managedCopyMarkerVersion = 1
)

// managedCopyMarker identifies a directory materialized by skills-switch in
// copy mode (for clients like Antigravity whose workspace scanner ignores
// symlinks resolving outside the repository root). Keeping ownership and
// content digest inside the projected directory preserves the invariant that
// there is no per-project manifest to drift.
type managedCopyMarker struct {
	Version     int    `json:"version"`
	SourcePath  string `json:"sourcePath"`
	ContentHash string `json:"contentHash"`
}

type entryKind int

const (
	entryAbsent entryKind = iota
	entrySymlink
	entryBrokenSymlink
	entryManagedCopy
	entryModifiedManagedCopy
	entryUnmanaged
)

type inspectedEntry struct {
	kind           entryKind
	rawLinkTarget  string
	resolvedTarget string
	contentHash    string
	cause          error
}

// canonicalPath resolves symlinks in p, or in the deepest existing ancestor of
// p when p itself does not exist (reattaching the cleaned remaining tail). This
// keeps macOS /tmp <-> /private/tmp and /var <-> /private/var aliases and
// symlinked resource roots comparing equal without losing stale/orphan checks
// for missing paths.
func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	clean := filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved)
	}
	var tail []string
	current := clean
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return clean
		}
		tail = append(tail, filepath.Base(current))
		current = parent
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			parts := make([]string, 0, len(tail)+1)
			parts = append(parts, resolved)
			for i := len(tail) - 1; i >= 0; i-- {
				parts = append(parts, tail[i])
			}
			return filepath.Clean(filepath.Join(parts...))
		}
	}
}

// canonicalSlotPath canonicalizes the parent directory of a projection slot
// without dereferencing the slot entry itself (which may be a symlink pointing
// at the skill SSOT).
func canonicalSlotPath(p string) string {
	if p == "" {
		return ""
	}
	clean := filepath.Clean(p)
	return filepath.Join(canonicalPath(filepath.Dir(clean)), filepath.Base(clean))
}

func inspectEntry(path string) (inspectedEntry, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return inspectedEntry{kind: entryAbsent}, nil
	}
	if err != nil {
		return inspectedEntry{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		raw, err := os.Readlink(path)
		if err != nil {
			return inspectedEntry{
				kind:  entryBrokenSymlink,
				cause: fmt.Errorf("read projection target: %w", err),
			}, nil
		}
		resolved := raw
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(canonicalPath(filepath.Dir(path)), resolved)
		}
		return inspectedEntry{
			kind:           entrySymlink,
			rawLinkTarget:  raw,
			resolvedTarget: canonicalPath(resolved),
		}, nil
	}
	if !info.IsDir() {
		return inspectedEntry{
			kind:  entryUnmanaged,
			cause: errors.New("path exists and is not a symlink or managed copy"),
		}, nil
	}
	markerPath := filepath.Join(path, managedCopyMarkerFile)
	markerInfo, err := os.Lstat(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return inspectedEntry{
			kind:  entryUnmanaged,
			cause: errors.New("path exists and is not a symlink"),
		}, nil
	}
	if err != nil {
		return inspectedEntry{}, err
	}
	if markerInfo.Mode()&os.ModeSymlink != 0 || !markerInfo.Mode().IsRegular() {
		return inspectedEntry{
			kind:  entryModifiedManagedCopy,
			cause: errors.New("managed skill copy marker is invalid"),
		}, nil
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return inspectedEntry{
			kind:  entryModifiedManagedCopy,
			cause: fmt.Errorf("read managed skill copy marker: %w", err),
		}, nil
	}
	var marker managedCopyMarker
	if err := json.Unmarshal(data, &marker); err != nil || marker.Version != managedCopyMarkerVersion || strings.TrimSpace(marker.SourcePath) == "" || strings.TrimSpace(marker.ContentHash) == "" {
		return inspectedEntry{
			kind:  entryModifiedManagedCopy,
			cause: errors.New("managed skill copy marker is corrupt"),
		}, nil
	}
	sourcePath := canonicalPath(marker.SourcePath)
	actualHash, err := hashSkillDir(path)
	if err != nil {
		return inspectedEntry{
			kind:           entryModifiedManagedCopy,
			resolvedTarget: sourcePath,
			contentHash:    marker.ContentHash,
			cause:          fmt.Errorf("managed skill copy cannot be verified: %w", err),
		}, nil
	}
	if actualHash != marker.ContentHash {
		return inspectedEntry{
			kind:           entryModifiedManagedCopy,
			resolvedTarget: sourcePath,
			contentHash:    marker.ContentHash,
			cause:          errors.New("managed skill copy was modified locally"),
		}, nil
	}
	return inspectedEntry{
		kind:           entryManagedCopy,
		resolvedTarget: sourcePath,
		contentHash:    marker.ContentHash,
	}, nil
}

func hasManagedCopyMarker(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, managedCopyMarkerFile))
	return err == nil
}

// resolveSkillDirRoot resolves symlinks on a Skill root directory and confirms
// it points to a real directory so WalkDir descends into symlinked roots.
func resolveSkillDirRoot(root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved = filepath.Clean(resolved)
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("skill path %s is not a directory", root)
	}
	return resolved, nil
}

// validateInternalSymlink enforces that linkPath (inside resolvedRoot) is a
// relative, non-escaping, cycle-free, non-dangling symlink whose resolved
// target stays strictly within resolvedRoot.
func validateInternalSymlink(resolvedRoot, linkPath string) error {
	rawTarget, err := os.Readlink(linkPath)
	if err != nil {
		return fmt.Errorf("read symlink %s: %w", linkPath, err)
	}
	if rawTarget == "" {
		return fmt.Errorf("symlink %s has empty target", linkPath)
	}
	if filepath.IsAbs(rawTarget) || strings.HasPrefix(rawTarget, "/") {
		return fmt.Errorf("symlink %s uses absolute target %q", linkPath, rawTarget)
	}
	relLink, err := filepath.Rel(resolvedRoot, linkPath)
	if err != nil || relLink == ".." || strings.HasPrefix(relLink, ".."+string(filepath.Separator)) {
		return fmt.Errorf("symlink %s is outside skill root %s", linkPath, resolvedRoot)
	}
	relDir := filepath.ToSlash(filepath.Dir(relLink))
	var curParts []string
	if relDir != "." && relDir != "" {
		curParts = strings.Split(relDir, "/")
	}
	pending := splitSlashSegments(rawTarget)
	visited := map[string]bool{linkPath + "\x00": true}
	const maxSymlinkHops = 40
	hops := 0

	for len(pending) > 0 {
		seg := pending[0]
		pending = pending[1:]
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(curParts) == 0 {
				return fmt.Errorf("symlink %s escapes skill directory %s", linkPath, resolvedRoot)
			}
			curParts = curParts[:len(curParts)-1]
		default:
			curParts = append(curParts, seg)
			candidate := joinRootParts(resolvedRoot, curParts)
			info, statErr := os.Lstat(candidate)
			if errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("symlink %s is dangling (missing %s)", linkPath, candidate)
			}
			if statErr != nil {
				return fmt.Errorf("inspect symlink target %s for %s: %w", candidate, linkPath, statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				hops++
				stateKey := candidate + "\x00" + strings.Join(pending, "/")
				if hops > maxSymlinkHops || visited[stateKey] {
					return fmt.Errorf("symlink %s forms a cycle at %s", linkPath, candidate)
				}
				visited[stateKey] = true
				nextTarget, readErr := os.Readlink(candidate)
				if readErr != nil {
					return fmt.Errorf("read chained symlink %s: %w", candidate, readErr)
				}
				if nextTarget == "" {
					return fmt.Errorf("chained symlink %s has empty target", candidate)
				}
				if filepath.IsAbs(nextTarget) || strings.HasPrefix(nextTarget, "/") {
					return fmt.Errorf("chained symlink %s uses absolute target %q", candidate, nextTarget)
				}
				curParts = curParts[:len(curParts)-1]
				nextParts := splitSlashSegments(nextTarget)
				pending = append(nextParts, pending...)
			}
		}
	}

	finalPath := joinRootParts(resolvedRoot, curParts)
	if finalPath != resolvedRoot && !underRoot(finalPath, resolvedRoot) {
		return fmt.Errorf("symlink %s escapes skill directory %s", linkPath, resolvedRoot)
	}
	if _, err := os.Lstat(finalPath); err != nil {
		return fmt.Errorf("symlink %s target %s is unavailable: %w", linkPath, finalPath, err)
	}
	return nil
}

func splitSlashSegments(raw string) []string {
	parts := strings.Split(filepath.ToSlash(raw), "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func joinRootParts(root string, parts []string) string {
	if len(parts) == 0 {
		return root
	}
	all := make([]string, 0, len(parts)+1)
	all = append(all, root)
	all = append(all, parts...)
	return filepath.Join(all...)
}

// hashSkillDir computes a deterministic SHA-256 digest of a Skill directory's
// structure, permissions (executable bit), symlinks, and file contents,
// excluding the top-level managed copy marker file.
func hashSkillDir(root string) (string, error) {
	resolvedRoot, err := resolveSkillDirRoot(root)
	if err != nil {
		return "", fmt.Errorf("hash skill directory %s: %w", root, err)
	}
	type item struct {
		rel  string
		full string
	}
	items := make([]item, 0)
	err = filepath.WalkDir(resolvedRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == resolvedRoot {
			return nil
		}
		rel, err := filepath.Rel(resolvedRoot, current)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == managedCopyMarkerFile {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		items = append(items, item{rel: relSlash, full: current})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash skill directory %s: %w", root, err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].rel < items[j].rel })

	hasher := sha256.New()
	for _, entry := range items {
		info, err := os.Lstat(entry.full)
		if err != nil {
			return "", fmt.Errorf("stat %s while hashing: %w", entry.full, err)
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if err := validateInternalSymlink(resolvedRoot, entry.full); err != nil {
				return "", err
			}
			target, err := os.Readlink(entry.full)
			if err != nil {
				return "", fmt.Errorf("readlink %s while hashing: %w", entry.full, err)
			}
			_, _ = io.WriteString(hasher, "L\x00"+entry.rel+"\x00"+target+"\x00")
		case info.IsDir():
			_, _ = io.WriteString(hasher, "D\x00"+entry.rel+"\x00")
		case info.Mode().IsRegular():
			execFlag := "0"
			if info.Mode()&0o111 != 0 {
				execFlag = "1"
			}
			_, _ = io.WriteString(hasher, "F\x00"+entry.rel+"\x00"+execFlag+"\x00"+strconv.FormatInt(info.Size(), 10)+"\x00")
			file, err := os.Open(entry.full)
			if err != nil {
				return "", fmt.Errorf("open %s while hashing: %w", entry.full, err)
			}
			_, copyErr := io.Copy(hasher, file)
			closeErr := file.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return "", fmt.Errorf("read %s while hashing: %w", entry.full, err)
			}
			_, _ = io.WriteString(hasher, "\x00")
		default:
			return "", fmt.Errorf("unsupported file type %s at %s", info.Mode().Type(), entry.full)
		}
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// stageManagedCopy materializes sourceDir into a fresh sibling temp directory
// inside parentDir and stamps its managed marker file. Callers atomically
// rename the returned directory into its final slot and remove it on error.
func stageManagedCopy(sourceDir, parentDir string) (stagingDir string, marker managedCopyMarker, err error) {
	if err := validateSkillSource(sourceDir); err != nil {
		return "", managedCopyMarker{}, err
	}
	stagingDir, err = os.MkdirTemp(parentDir, ".skills-switch-copy-*")
	if err != nil {
		return "", managedCopyMarker{}, fmt.Errorf("create managed copy staging directory: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	if err = os.Chmod(stagingDir, 0o755); err != nil {
		return "", managedCopyMarker{}, fmt.Errorf("prepare managed copy staging directory: %w", err)
	}
	if err = copyManagedSkillContents(sourceDir, stagingDir); err != nil {
		return "", managedCopyMarker{}, err
	}
	contentHash, err := hashSkillDir(stagingDir)
	if err != nil {
		return "", managedCopyMarker{}, err
	}
	marker = managedCopyMarker{
		Version:     managedCopyMarkerVersion,
		SourcePath:  canonicalPath(sourceDir),
		ContentHash: contentHash,
	}
	if err = writeManagedCopyMarker(stagingDir, marker); err != nil {
		return "", managedCopyMarker{}, err
	}
	return stagingDir, marker, nil
}

func validateSkillSource(sourceDir string) error {
	info, err := os.Stat(filepath.Join(sourceDir, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("source SKILL.md is unavailable: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source SKILL.md is a directory: %s", sourceDir)
	}
	return nil
}

func copyManagedSkillContents(sourceDir, stagingDir string) error {
	resolvedSource, err := resolveSkillDirRoot(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve skill source %s: %w", sourceDir, err)
	}
	type dirMode struct {
		path string
		mode fs.FileMode
	}
	dirs := make([]dirMode, 0)
	err = filepath.WalkDir(resolvedSource, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == resolvedSource {
			return nil
		}
		rel, err := filepath.Rel(resolvedSource, current)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == managedCopyMarkerFile {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dest := filepath.Join(stagingDir, rel)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if err := validateInternalSymlink(resolvedSource, current); err != nil {
				return err
			}
			linkTarget, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, dest)
		case info.IsDir():
			if err := os.Mkdir(dest, 0o700); err != nil {
				return err
			}
			// Keep directories user-writable so later cleanup or refresh can
			// remove them without permission failures.
			dirs = append(dirs, dirMode{path: dest, mode: info.Mode().Perm() | 0o700})
			return nil
		case info.Mode().IsRegular():
			return copyAdoptionFile(current, dest, info.Mode().Perm()|0o200)
		default:
			return fmt.Errorf("unsupported file type %s at %s", info.Mode().Type(), current)
		}
	})
	if err != nil {
		return err
	}
	for index := len(dirs) - 1; index >= 0; index-- {
		if err := os.Chmod(dirs[index].path, dirs[index].mode); err != nil {
			return err
		}
	}
	return nil
}

func writeManagedCopyMarker(dir string, marker managedCopyMarker) error {
	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("encode managed copy marker: %w", err)
	}
	encoded = append(encoded, '\n')
	tmp, err := os.CreateTemp(dir, ".skills-switch-marker-*")
	if err != nil {
		return fmt.Errorf("create managed copy marker temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write managed copy marker: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod managed copy marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close managed copy marker: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, managedCopyMarkerFile)); err != nil {
		return fmt.Errorf("install managed copy marker: %w", err)
	}
	return nil
}
