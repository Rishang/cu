//go:build windows

package proc

import (
	"os"
	"os/exec"
)

// Replace runs binary to completion with cu's own stdio. Windows has no exec,
// so cu stays alive as the parent; a non-zero exit comes back as
// *exec.ExitError, which main passes through as cu's own exit code. argv[0]
// is the program name.
func Replace(binary string, argv []string) error {
	cmd := exec.Command(binary, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
