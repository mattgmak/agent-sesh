// Package version reports the agent-sesh build version.
package version

import (
	"fmt"
	"io"
	"runtime/debug"
)

// Version is stamped at build time via
// -ldflags "-X github.com/mattgmak/agent-sesh/internal/version.Version=<v>".
var Version = ""

// String returns the build version, falling back to the module version
// recorded by the Go toolchain (go install path), then to "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// Write prints "agent-sesh <version>" to w.
func Write(w io.Writer) error {
	_, err := fmt.Fprintf(w, "agent-sesh %s\n", String())
	return err
}
