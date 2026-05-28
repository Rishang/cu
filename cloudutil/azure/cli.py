"""Azure CLI interface."""

import json
from typing import Optional

import typer

from cloudutil.utils import console
from .secrets import search_secrets_with_fzf

app = typer.Typer(
    pretty_exceptions_enable=False,
)


@app.command()
def secrets(
    vault: str = typer.Option(..., "--vault", "-v", help="Name of the Azure Key Vault"),
    name_filter: Optional[str] = typer.Option(
        None, "--filter", help="Filter secrets by name prefix"
    ),
    output: str = typer.Option(
        "text", "--output", "-o", help="Output format (text/json)"
    ),
):
    """
    Search Key Vault secrets interactively using fzf for selection.
    """
    try:
        quiet = output.lower() == "json"
        selected = search_secrets_with_fzf(vault_name=vault, name_filter=name_filter)

        if not selected:
            raise typer.Exit(code=1)

        if quiet:
            console.print(
                json.dumps([s.model_dump() for s in selected], indent=2, default=str)
            )
        else:
            for s in selected:
                console.print(f"Name: '{s.name}'")
                if s.description:
                    console.print(f"Description: '{s.description}'")
                if s.id:
                    console.print(f"ID: '{s.id}'")
                try:
                    parsed = json.loads(s.value)
                    console.print("Value (JSON):")
                    console.print(json.dumps(parsed, indent=2))
                except json.JSONDecodeError:
                    console.print(f"Value: {s.value}")
                console.print("-" * 80)
                console.print()

    except typer.Exit:
        raise
    except Exception as e:
        console.print(f"[bold red][!] ERROR: {e}[/bold red]")
        raise typer.Exit(code=1)
