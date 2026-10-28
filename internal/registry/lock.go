package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockPollInterval = 25 * time.Millisecond
const lockTimeout = 5 * time.Second

// withRegistryLock uses advisory flock on path+".lock". The pi-agent-sesh
// extension uses the same lock file via koffi (see registry-lock.ts).
func withRegistryLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	// The state dir may not exist yet on a fresh install; the lock file needs it.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create registry dir %s: %w", filepath.Dir(path), err)
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return fmt.Errorf("open registry lock %s: %w", lockPath, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			defer func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}()
			return fn()
		}
		_ = f.Close()
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for registry lock %s", lockPath)
		}
		time.Sleep(lockPollInterval)
	}
}
