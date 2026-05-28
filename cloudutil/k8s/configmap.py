"""Kubernetes ConfigMap utilities with fzf-powered viewing."""

from __future__ import annotations

from typing import List, Optional

from pydantic import BaseModel, computed_field

from cloudutil.helper import fzf_select
from cloudutil.k8s.util import _kubectl_json
from cloudutil.helper.fzf_view import FzfView
from cloudutil.utils import console


class K8sConfigMapRef(BaseModel):
    namespace: str
    name: str

    @computed_field
    @property
    def display(self) -> str:
        return f"{self.namespace}/{self.name}"


class K8sConfigMapKeyRef(BaseModel):
    """A single ConfigMap data key for fzf (one row per key)."""

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


def list_configmap_key_refs(
    all_namespaces: bool = True,
    namespace: Optional[str] = None,
) -> List[K8sConfigMapKeyRef]:
    """One entry per data/binaryData key (fzf line: ns/name/key)."""
    args: List[str] = ["get", "configmaps", "-o", "json"]
    if all_namespaces:
        args.insert(1, "-A")
    elif namespace:
        args.extend(["-n", namespace])

    data = _kubectl_json(args)
    result: List[K8sConfigMapKeyRef] = []
    for item in data.get("items", []):
        if not item.get("metadata", {}).get("name"):
            continue
        ns = item["metadata"].get("namespace", "default")
        name = item["metadata"]["name"]
        raw_data = item.get("data") or {}
        raw_binary = item.get("binaryData") or {}
        for key in sorted(set(raw_data) | set(raw_binary)):
            result.append(K8sConfigMapKeyRef(namespace=ns, name=name, key=key))
    return result


def get_configmap(ref: K8sConfigMapRef) -> dict:
    """Fetch a single ConfigMap as JSON."""
    return _kubectl_json(
        ["get", "configmap", ref.name, "-n", ref.namespace, "-o", "json"]
    )


class K8sConfigMapsView(FzfView[K8sConfigMapKeyRef]):
    """Interactive fzf viewer for Kubernetes ConfigMaps (one row per key)."""

    item_type_name = "k8s configmap key"
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

    def list_items(self) -> List[K8sConfigMapKeyRef]:
        ns = self._resolve_namespace()
        return list_configmap_key_refs(
            all_namespaces=self._all_namespaces and ns is None, namespace=ns
        )

    def item_label(self, item: K8sConfigMapKeyRef) -> str:
        return item.display

    def display_item(self, item: K8sConfigMapKeyRef) -> dict[str, str]:
        raw = get_configmap(K8sConfigMapRef(namespace=item.namespace, name=item.name))
        data = raw.get("data") or {}
        binary = raw.get("binaryData") or {}
        parent = f"{item.namespace}/{item.name}"
        flat_key = f"{parent}/{item.key}"
        if item.key in data:
            return {flat_key: data[item.key]}
        if item.key in binary:
            b64 = binary[item.key]
            return {flat_key: f"<binaryData base64, length {len(b64)} chars>"}
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


def view_configmaps_with_fzf(
    all_namespaces: bool = True,
    namespace: Optional[str] = None,
    select_namespace: bool = False,
) -> None:
    K8sConfigMapsView(
        all_namespaces=all_namespaces,
        namespace=namespace,
        select_namespace=select_namespace,
    ).run()
