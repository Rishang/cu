"""Kubernetes Secrets utilities with fzf-powered viewing."""

from __future__ import annotations

import base64
from typing import List, Optional

from pydantic import BaseModel, computed_field

from cloudutil.helper import fzf_select
from cloudutil.k8s.util import _kubectl_json
from cloudutil.helper.fzf_view import FzfView
from cloudutil.utils import console


class K8sSecretRef(BaseModel):
    namespace: str
    name: str

    @computed_field
    @property
    def display(self) -> str:
        return f"{self.namespace}/{self.name}"


class K8sSecretKeyRef(BaseModel):
    """A single Secret data key for fzf (one row per key)."""

    namespace: str
    name: str
    key: str

    @computed_field
    @property
    def display(self) -> str:
        return f"{self.namespace}/{self.name}/{self.key}"


def _list_namespaces() -> List[str]:
    data = _kubectl_json(["get", "namespaces", "-o", "json"])
    return sorted(
        {
            item.get("metadata", {}).get("name", "default")
            for item in data.get("items", [])
        }
    )


def _decode_secret_data(data: dict) -> dict[str, str]:
    decoded: dict[str, str] = {}
    for key, value in (data or {}).items():
        try:
            decoded[key] = base64.b64decode(value).decode("utf-8", errors="replace")
        except Exception:
            decoded[key] = f"<b64-decode-error> raw={value!r}"
    return decoded


def list_secret_key_refs(
    all_namespaces: bool = True,
    namespace: Optional[str] = None,
) -> List[K8sSecretKeyRef]:
    """One entry per data/stringData key (fzf line: ns/name/key)."""
    args: List[str] = ["get", "secrets", "-o", "json"]
    if all_namespaces:
        args.insert(1, "-A")
    elif namespace:
        args.extend(["-n", namespace])

    data = _kubectl_json(args)
    result: List[K8sSecretKeyRef] = []
    for item in data.get("items", []):
        if not item.get("metadata", {}).get("name"):
            continue
        ns = item["metadata"].get("namespace", "default")
        name = item["metadata"]["name"]
        raw_data = item.get("data") or {}
        raw_string = item.get("stringData") or {}
        for key in sorted(set(raw_data) | set(raw_string)):
            result.append(K8sSecretKeyRef(namespace=ns, name=name, key=key))
    return result


def get_secret(ref: K8sSecretRef) -> dict:
    """Fetch a single secret as JSON."""
    return _kubectl_json(["get", "secret", ref.name, "-n", ref.namespace, "-o", "json"])


class K8sSecretsView(FzfView[K8sSecretKeyRef]):
    """Interactive fzf viewer for Kubernetes Secrets (one row per key, base64-decoded)."""

    item_type_name = "k8s secret key"
    multi_select = True

    def __init__(
        self,
        all_namespaces: bool = True,
        namespace: Optional[str] = None,
        select_namespace: bool = False,
    ) -> None:
        self._all_namespaces = all_namespaces
        self._namespace = namespace
        self._select_namespace = select_namespace

    def list_items(self) -> List[K8sSecretKeyRef]:
        ns = self._resolve_namespace()
        return list_secret_key_refs(
            all_namespaces=self._all_namespaces and ns is None, namespace=ns
        )

    def item_label(self, item: K8sSecretKeyRef) -> str:
        return item.display

    def display_item(self, item: K8sSecretKeyRef) -> dict[str, str]:
        raw = get_secret(K8sSecretRef(namespace=item.namespace, name=item.name))
        parent = f"{item.namespace}/{item.name}"
        flat_key = f"{parent}/{item.key}"
        decoded = _decode_secret_data(raw.get("data", {}))
        string_data = raw.get("stringData") or {}
        if item.key in decoded:
            return {flat_key: decoded[item.key]}
        if item.key in string_data:
            return {flat_key: string_data[item.key]}
        return {flat_key: "<key not found>"}

    def _resolve_namespace(self) -> Optional[str]:
        if not self._select_namespace or self._namespace:
            return self._namespace
        namespaces = _list_namespaces()
        if not namespaces:
            console.print("[yellow][!] No namespaces found.[/yellow]")
            return None
        selected = fzf_select(namespaces, "k8s namespace", multi_select=False)
        if not selected:
            return None
        self._all_namespaces = False
        return selected[0]


def view_secrets_with_fzf(
    all_namespaces: bool = True,
    namespace: Optional[str] = None,
    select_namespace: bool = False,
) -> None:
    K8sSecretsView(
        all_namespaces=all_namespaces,
        namespace=namespace,
        select_namespace=select_namespace,
    ).run()
