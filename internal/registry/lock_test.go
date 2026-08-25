package registry

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryLockExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	lockPath := path + ".lock"

	var held atomic.Bool
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	run := func() {
		defer wg.Done()
		errCh <- withRegistryLock(path, func() error {
			if !held.CompareAndSwap(false, true) {
				return errors.New("lock already held")
			}
			time.Sleep(50 * time.Millisecond)
			held.Store(false)
			return nil
		})
	}

	wg.Add(2)
	go run()
	go run()
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("withRegistryLock: %v", err)
		}
	}
	if held.Load() {
		t.Fatal("lock still marked held after both goroutines finished")
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file %s to exist: %v", lockPath, err)
	}
}
