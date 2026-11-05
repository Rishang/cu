//go:build !windows

package proc

import (
	"os"
	"syscall"
)

// Replace replaces cu with binary, so the program owns the TTY, its signals
// and its exit code directly. argv[0] is the program name. It returns only if
// the exec itself fails.
func Replace(binary string, argv []string) error {
	return syscall.Exec(binary, argv, os.Environ())
}
