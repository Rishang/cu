"""CLI for os_utils."""

import os
import subprocess

import typer

app = typer.Typer(
    name="os",
    help="OS-related utilities.",
    rich_markup_mode="rich",
    no_args_is_help=True,
)


@app.command()
def history():
    """Search shell history with fzf."""
    shell = os.environ.get("SHELL", "")

    if "zsh" in shell:
        cmd = r"""cat ~/.zsh_history | sed 's/^: [0-9]*:[0-9]*;//' | sort -u | fzf -e -m"""
    elif "bash" in shell:
        cmd = r"""cat ~/.bash_history | sort -u | fzf -e"""
    else:
        typer.echo(
            f"[ERROR] Unsupported shell: {shell!r}. Supported: zsh, bash.", err=True
        )
        raise typer.Exit(1)

    subprocess.run(["bash", "-c", cmd])


if __name__ == "__main__":
    app()
