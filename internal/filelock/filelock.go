//go:build !windows

package filelock

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// WithExclusive serializes a read-modify-write transaction across processes.
// The lock file is intentionally persistent; the kernel releases the advisory
// lock automatically when the process exits.
func WithExclusive(target string, operation func() error) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve lock target: %w", err)
	}
	lockRoot := filepath.Join(os.TempDir(), "skills-switch-locks")
	if err := os.MkdirAll(lockRoot, 0o700); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}
	digest := sha256.Sum256([]byte(absolute))
	lockPath := filepath.Join(lockRoot, fmt.Sprintf("%x.lock", digest))
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock %s: %w", lockPath, err)
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock %s: %w", lockPath, err)
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN) //nolint:errcheck
	return operation()
}
