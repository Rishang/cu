import subprocess
from typing import List, Optional, Tuple
from rich.console import Console


console = Console(stderr=True)


class ShellRunner:
    """Utility class for running shell commands."""

    def __init__(self):
        self.console = console

    def run_command(
        self,
        command: List[str],
        input_text: Optional[str] = None,
        capture_output: bool = True,
        text: bool = True,
    ) -> Tuple[bool, str, str]:
        """
        Run a shell command and return success status and output.

        Args:
            command: Command to run as list of strings
            input_text: Input to pass to the command
            capture_output: Whether to capture stdout/stderr
            text: Whether to treat input/output as text

        Returns:
            Tuple of (success, stdout, stderr)
        """
        try:
            result = subprocess.run(
                command,
                input=input_text,
                capture_output=capture_output,
                text=text,
                check=False,  # Don't raise exception on non-zero exit
            )

            success = result.returncode == 0
            stdout = result.stdout or ""
            stderr = result.stderr or ""

            return success, stdout, stderr

        except FileNotFoundError:
            return False, "", f"Command not found: {' '.join(command)}"
        except Exception as e:
            return False, "", str(e)


# Global shell runner instance
shell = ShellRunner()
