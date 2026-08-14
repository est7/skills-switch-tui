package filelock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// The lock keys on the absolute target path, and both platform
// implementations lock a distinct open file description per call, so two
// in-process calls contend exactly like two processes do.
func TestWithExclusiveSerializesConcurrentCriticalSections(t *testing.T) {
	target := filepath.Join(t.TempDir(), "catalog.yaml")
	holderIn := make(chan struct{})
	holderRelease := make(chan struct{})
	holderDone := make(chan error, 1)
	waiterIn := make(chan struct{})
	waiterDone := make(chan error, 1)

	go func() {
		holderDone <- WithExclusive(target, func() error {
			close(holderIn)
			<-holderRelease
			return nil
		})
	}()
	<-holderIn
	go func() {
		waiterDone <- WithExclusive(target, func() error {
			close(waiterIn)
			return nil
		})
	}()

	select {
	case <-waiterIn:
		t.Fatal("second WithExclusive entered the critical section while the first still held the lock")
	case <-time.After(100 * time.Millisecond):
	}

	close(holderRelease)
	for _, done := range []chan error{holderDone, waiterDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("WithExclusive: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("WithExclusive did not finish after the lock was released")
		}
	}
	select {
	case <-waiterIn:
	default:
		t.Fatal("second WithExclusive never ran its operation")
	}
}

func TestWithExclusiveDistinctTargetsDoNotContend(t *testing.T) {
	directory := t.TempDir()
	holderIn := make(chan struct{})
	holderRelease := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- WithExclusive(filepath.Join(directory, "a.yaml"), func() error {
			close(holderIn)
			<-holderRelease
			return nil
		})
	}()
	<-holderIn

	otherDone := make(chan error, 1)
	go func() {
		otherDone <- WithExclusive(filepath.Join(directory, "b.yaml"), func() error { return nil })
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatalf("WithExclusive on a distinct target: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a distinct target blocked behind an unrelated lock")
	}

	close(holderRelease)
	if err := <-holderDone; err != nil {
		t.Fatalf("WithExclusive: %v", err)
	}
}

func TestWithExclusivePropagatesOperationErrorAndReleasesLock(t *testing.T) {
	target := filepath.Join(t.TempDir(), "catalog.yaml")
	sentinel := errors.New("operation failed")
	if err := WithExclusive(target, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("WithExclusive error = %v, want %v", err, sentinel)
	}
	// The failed operation must not leave the target locked.
	done := make(chan error, 1)
	go func() {
		done <- WithExclusive(target, func() error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("relock after failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock was not released after a failed operation")
	}
}
