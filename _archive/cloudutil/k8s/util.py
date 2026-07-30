import json
from cloudutil.utils import shell


def _kubectl_json(args: list[str]) -> dict:
    ok, out, err = shell.run_command(["kubectl", *args])
    if not ok:
        raise RuntimeError(f"kubectl {' '.join(args)} failed: {err or out}")
    try:
        return json.loads(out)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Failed to parse kubectl output as JSON: {exc}") from exc


def _list_kube_contexts() -> list[str]:
    """Return context names from the current kubeconfig."""
    data = _kubectl_json(["config", "view", "-o", "json"])
    raw = data.get("contexts") or []
    names: list[str] = []
    for item in raw:
        if isinstance(item, dict) and item.get("name"):
            names.append(str(item["name"]))
    return names
