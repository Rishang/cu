#!/usr/bin/env python3

# Pre-requisits
# pip install typer rich requests
# Setup download the file and shift it to ~/.local/bin/pwpush; chmod +x ~/.local/bin/pwpush

# Example usage:
# pwpush --help

# Share a secret
# pwpush send --note project_a_secret

# ref: https://docs.pwpush.com/docs/json-api/#python

import os
import json
import tempfile
import subprocess
from pathlib import Path
import secrets
import string
import requests
import typer
import rich
from rich.table import Table
from typing import Optional

app = typer.Typer(help="Password Pusher CLI tool", pretty_exceptions_enable=False)


CONFIG_DIR = Path.home() / ".config" / "cu"
CONFIG_FILE = CONFIG_DIR / "psst.json"


def ensure_config_dir():
    """Ensure the config directory exists."""
    if not CONFIG_DIR.exists():
        CONFIG_DIR.mkdir(parents=True, exist_ok=True)


def load_config():
    """Load configuration or exit if missing."""
    if not CONFIG_FILE.exists():
        typer.echo("No configuration found. Please run 'config' first.")
        raise typer.Exit(1)
    with open(CONFIG_FILE, "r") as f:
        return json.load(f)


@app.command()
def config(
    token: str = typer.Option(
        ..., "--token", help="Your API token for Password Pusher"
    ),
    email: Optional[str] = typer.Option(
        ...,
        "--email",
        help="Your email (for self-hosted legacy auth) eg: user@example.com",
    ),
    source: str = typer.Option(
        ..., "--source", help="Base URL of the instance eg: https://pwpush.com"
    ),
):
    """Authenticate by saving your token (and optional email for legacy auth)."""
    ensure_config_dir()
    cfg = {"token": token, "source": source.rstrip("/")}
    if email:
        cfg["email"] = email
    with open(CONFIG_FILE, "w") as f:
        json.dump(cfg, f)
    typer.echo(f"Saved auth config to {CONFIG_FILE}")


@app.command()
def list_active():
    """List active passwords."""
    config = load_config()
    url = f"{config['source']}/p/active.json"
    headers = {
        "X-User-Email": config["email"],
        "X-User-Token": config["token"],
        "Accept": "application/json",
    }
    response = requests.get(url, headers=headers)
    data = response.json()
    # rich.print(data)
    table = Table(title="Active Passwords")
    table.add_column("ID", justify="right")
    table.add_column("Note", justify="left")
    table.add_column("URL", justify="left")
    table.add_column("Expires After Days", justify="right")
    table.add_column("Views Remaining", justify="right")

    for count, password in enumerate(data):
        if password["days_remaining"] <= 0:
            continue

        table.add_row(
            str(count),
            str(password["note"]),
            f"{config['source']}/p/{password['url_token']}",
            str(password["days_remaining"]),
            str(password["views_remaining"]),
        )
    rich.print(table)


@app.command()
def send(
    days: int = typer.Option(7, "--days", "-d", help="Expire after days"),
    views: int = typer.Option(5, "--views", "-v", help="Expire after views"),
    note: Optional[str] = typer.Option(None, "--note", "-n", help="Optional note"),
    deletable_by_viewer: bool = typer.Option(True, "--deletable/--not-deletable"),
    file: Optional[str] = typer.Option(None, "--file", "-f", help="File to upload"),
    kind: str = typer.Option(
        "password", "--kind", "-k", help="Type: password, url, or qr"
    ),
):
    """Send a password push, auto-selecting Bearer or legacy auth based on config."""
    config = load_config()
    retrievable_by_viewer = True
    # Prepare payload in temp file
    url = f"{config['source']}/p.json"

    # Determine headers
    if config.get("email"):
        # Legacy self-hosted auth
        headers = {
            "X-User-Email": config["email"],
            "X-User-Token": config["token"],
            "Accept": "application/json",
        }
    else:
        # Bearer token auth
        headers = {
            "Authorization": f"Bearer {config['token']}",
            "Accept": "application/json",
        }

    try:
        if not file:
            with tempfile.NamedTemporaryFile(suffix=".txt", delete=False) as tmp:
                temp_path = tmp.name

            subprocess.run([os.environ.get("EDITOR", "vim"), temp_path])
            payload = Path(temp_path).read_text().strip()
            if not payload:
                typer.echo("No content entered. Aborting.")
                raise typer.Exit(1)
        else:
            payload = Path(file).read_text().strip()

        payload_data = {
            "password": {
                "payload": payload,
                "expire_after_days": days,
                "expire_after_views": views,
                "retrievable_by_viewer": 1 if retrievable_by_viewer else 0,
                "deletable_by_viewer": 1 if deletable_by_viewer else 0,
                "kind": kind,
            }
        }

        if note:
            payload_data["password"]["note"] = note

        response = requests.post(
            url,
            headers=headers,
            json=payload_data,
        )

        if response.status_code in (200, 201):
            data = response.json()
            token = data.get("url_token") or data.get("password", {}).get("url_token")
            if token:
                rich.print(f"{config['source']}/p/{token}")
            else:
                rich.print(f"Success but no URL token returned: {data}")
        else:
            typer.echo(f"Error {response.status_code}: {response.text}")
            raise typer.Exit(1)
    except Exception as e:
        typer.echo(f"Error: {e}")
        raise typer.Exit(1)
    finally:
        try:
            os.unlink(temp_path)
        except OSError:
            pass
        except UnboundLocalError:
            pass


@app.command()
def pwgen(
    length: int = typer.Option(16, "--length", "-l"),
    no_symbols: bool = typer.Option(False, "--no-symbols"),
    no_uppercase: bool = typer.Option(False, "--no-uppercase"),
    no_lowercase: bool = typer.Option(False, "--no-lowercase"),
    no_digits: bool = typer.Option(False, "--no-digits"),
):
    """Generate a random password."""
    chars = ""
    if not no_uppercase:
        chars += string.ascii_uppercase
    if not no_lowercase:
        chars += string.ascii_lowercase
    if not no_digits:
        chars += string.digits
    if not no_symbols:
        chars += string.punctuation
    if not chars:
        typer.echo("Error: no character types selected.")
        raise typer.Exit(1)
    password = "".join(secrets.choice(chars) for _ in range(length))
    typer.echo(password)


# Other commands (list_active, list_expired, expire, retrieve) unchanged
if __name__ == "__main__":
    app()
