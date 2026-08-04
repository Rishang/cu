"""Load JSON, YAML, and TOML files into Python objects."""

import json
import subprocess
import tomllib
from pathlib import Path
from typing import Any

import hcl2
import yaml

SUPPORTED_EXTENSIONS = {".json", ".yaml", ".yml", ".toml", ".tf", ".hcl", ".tfvars"}
HCL_EXTENSIONS = {".tf", ".hcl", ".tfvars"}


class GitBranchError(ValueError):
    """Raised when a path is not on a named Git branch."""


def require_git_branch(path: str | Path) -> str:
    """Return the branch for *path* or raise ``GitBranchError`` with context."""
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"],
            cwd=Path(path).resolve().parent,
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError as exc:
        raise GitBranchError(
            f"Could not determine git branch for '{path}': {exc.stderr.strip()}"
        ) from exc
    except FileNotFoundError as exc:
        raise GitBranchError(
            f"Could not determine git branch for '{path}': git executable not found"
        ) from exc

    if (branch := result.stdout.strip()) and branch != "HEAD":
        return branch
    raise GitBranchError("detached HEAD — no branch name available")


def get_git_branch(path: str | Path) -> str | None:
    """Return the current git branch for the repo containing *path*, or None."""
    try:
        return require_git_branch(path)
    except GitBranchError:
        return None


def load_file(path: str | Path) -> Any:
    """Parse a structured config file (JSON / YAML / TOML) into a Python object."""
    p = Path(path)

    if not p.exists():
        raise FileNotFoundError(f"File not found: {path}")

    suffix = p.suffix.lower()
    if suffix not in SUPPORTED_EXTENSIONS:
        raise ValueError(
            f"Unsupported format {suffix!r}. Supported: {', '.join(sorted(SUPPORTED_EXTENSIONS))}"
        )

    try:
        match suffix:
            case ".toml":
                with p.open("rb") as fh:
                    return tomllib.load(fh)
            case ".json":
                return json.loads(p.read_text(encoding="utf-8"))
            case ".tf" | ".hcl" | ".tfvars":
                with p.open("r", encoding="utf-8") as fh:
                    return hcl2.load(fh)
            case _:  # .yaml / .yml
                return yaml.safe_load(p.read_text(encoding="utf-8"))
    except tomllib.TOMLDecodeError as exc:
        raise ValueError(f"Invalid TOML in {path!r}: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"Invalid JSON in {path!r}: {exc}") from exc
    except yaml.YAMLError as exc:
        raise ValueError(f"Invalid YAML in {path!r}: {exc}") from exc
    except Exception as exc:
        raise ValueError(f"Invalid HCL in {path!r}: {exc}") from exc
