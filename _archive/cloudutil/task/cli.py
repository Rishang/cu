import os
import typer


def app(
    ctx: typer.Context,
    yaml_file: str = typer.Option(
        f"{os.getenv('HOME')}/.config/cu/Taskfile.yml", "--taskfile", "-t"
    ),
    directory: str = typer.Option(os.getcwd(), "--directory", "-d"),
) -> None:
    """
    # ref: https://taskfile.dev/docs/getting-started
    # ~/.config/cu/Taskfile.yaml
    ```yaml
    version: '3'

    vars:
      GREETING: Hello, World!

    tasks:
      default:
        desc: Print a greeting message
        cmds:
          - echo "{{.GREETING}}"
        silent: true
    ```

    Example:
    cu task default

    """

    # Replace this process with `task` for full interactive TTY behavior.
    os.execvp("task", ["task", "-t", yaml_file, "-d", directory, *ctx.args])
