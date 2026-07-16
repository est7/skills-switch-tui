//go:build windows

package filelock

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

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
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		return fmt.Errorf("lock %s: %w", lockPath, err)
	}
	defer windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped) //nolint:errcheck
	return operation()
}
