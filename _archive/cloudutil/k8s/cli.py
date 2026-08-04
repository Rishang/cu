"""CLI for Kubernetes utilities (secrets, configmaps)."""

from __future__ import annotations

import os
from typing import Optional

import typer

from cloudutil.helper import fzf_select
from cloudutil.k8s.util import _list_kube_contexts
from cloudutil.k8s.configmap import view_configmaps_with_fzf
from cloudutil.k8s.secrets import view_secrets_with_fzf
from cloudutil.utils import console

app = typer.Typer(
    pretty_exceptions_enable=False,
    help="Kubernetes-related commands",
)


@app.command("secrets")
def k8s_secrets(
    all_namespaces: bool = typer.Option(
        False,
        "--all-namespaces",
        "-A",
        help="Search secrets across all namespaces.",
    ),
    namespace: Optional[str] = typer.Option(
        None,
        "--namespace",
        "-n",
        help="Namespace to search (ignored if --all-namespaces is set).",
    ),
    select_namespace: bool = typer.Option(
        False,
        "--select-namespace",
        help="Use fzf to select a namespace instead of scanning all namespaces.",
    ),
) -> None:
    """
    Interactive fzf view for Kubernetes secrets (decodes base64 data before printing).
    """
    if all_namespaces:
        namespace = None
    view_secrets_with_fzf(
        all_namespaces=all_namespaces,
        namespace=namespace,
        select_namespace=select_namespace,
    )


@app.command("configmaps")
def k8s_configmaps(
    all_namespaces: bool = typer.Option(
        False,
        "--all-namespaces",
        "-A",
        help="Search ConfigMaps across all namespaces.",
    ),
    namespace: Optional[str] = typer.Option(
        None,
        "--namespace",
        "-n",
        help="Namespace to search (ignored if --all-namespaces is set).",
    ),
    select_namespace: bool = typer.Option(
        False,
        "--select-namespace",
        help="Use fzf to select a namespace instead of scanning all namespaces.",
    ),
) -> None:
    """
    Interactive fzf view for Kubernetes ConfigMaps.
    """
    if all_namespaces:
        namespace = None
    view_configmaps_with_fzf(
        all_namespaces=all_namespaces,
        namespace=namespace,
        select_namespace=select_namespace,
    )


@app.command("ctx")
def kubectx() -> None:
    """
    Switch between Kubernetes contexts interactively.

    Replaces this process with ``kubectl config use-context`` for normal TTY/signal behavior.
    """
    try:
        contexts = _list_kube_contexts()
    except RuntimeError as exc:
        console.print(f"[red]{exc}[/red]")
        raise typer.Exit(1) from exc
    if not contexts:
        console.print("[yellow][!] No contexts found.[/yellow]")
        raise typer.Exit(0)
    selected = fzf_select(contexts, "k8s context", multi_select=False)
    if not selected:
        raise typer.Exit(0)
    os.execvp("kubectl", ["kubectl", "config", "use-context", selected[0]])
