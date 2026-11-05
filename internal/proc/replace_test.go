package proc

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

// TestReplaceKeepsChildExitCode re-runs the test binary as a helper that calls
// Replace, since a successful Replace never returns to the test. The child's
// exit code must reach the caller unchanged: cu task and ec2-ssm report
// failure through it.
func TestReplaceKeepsChildExitCode(t *testing.T) {
	if os.Getenv("CU_PROC_HELPER") == "1" {
		sh, err := exec.LookPath("sh")
		if err != nil {
			os.Exit(99)
		}
		_ = Replace(sh, []string{"sh", "-c", "echo replaced; exit 3"})
		os.Exit(98) // only reached when Replace itself failed
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestReplaceKeepsChildExitCode$")
	cmd.Env = append(os.Environ(), "CU_PROC_HELPER=1")
	out, err := cmd.Output()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("exit = %v, want the child's code 3; callers would misreport the task's result", err)
	}
	if string(out) != "replaced\n" {
		t.Errorf("stdout = %q, want the child's own output; it did not get cu's stdio", out)
	}
}
