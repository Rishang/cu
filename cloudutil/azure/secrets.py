"""Azure Key Vault secrets utilities."""

from typing import List, Optional

from azure.identity import DefaultAzureCredential
from azure.keyvault.secrets import SecretClient
from pydantic import BaseModel

from ..helper.fzf_view import FzfView

from ..utils import console


class Secret(BaseModel):
    """Secret model."""

    name: str
    value: str
    id: Optional[str] = None
    description: Optional[str] = None


def get_secret_client(vault_name: str) -> SecretClient:
    """Get Key Vault client."""
    vault_url = f"https://{vault_name}.vault.azure.net/"
    credential = DefaultAzureCredential()
    return SecretClient(vault_url=vault_url, credential=credential)


def list_secrets(vault_name: str, name_filter: Optional[str] = None) -> List[str]:
    """List Key Vault secret names."""
    client = get_secret_client(vault_name)
    secrets = []
    for secret_prop in client.list_properties_of_secrets():
        if name_filter and not secret_prop.name.startswith(name_filter):
            continue
        secrets.append(secret_prop.name)
    return secrets


def get_secret(vault_name: str, name: str) -> Secret:
    """Get a specific secret by name."""
    client = get_secret_client(vault_name)
    bundle = client.get_secret(name)
    return Secret(
        name=bundle.name,
        value=bundle.value,
        id=bundle.id,
        description=bundle.properties.content_type,
    )


# ── FzfView subclass ──────────────────────────────────────────────────────────


class AzureSecretsView(FzfView[str]):
    """
    Interactive fzf viewer for Azure Key Vault secrets.

    Lists secret names, lets the user pick via fzf, then fetches and
    returns the selected ``Secret`` objects.
    """

    item_type_name = "Azure Key Vault secret"
    multi_select = True

    def __init__(
        self,
        vault_name: str,
        name_filter: Optional[str] = None,
    ) -> None:
        self._vault_name = vault_name
        self._name_filter = name_filter
        self._selected_secrets: List[Secret] = []

    def list_items(self) -> List[str]:
        console.print(
            f"[*] Listing secrets from vault [bold cyan]{self._vault_name}[/bold cyan]"
            + (
                f" with filter: [bold cyan]{self._name_filter}[/bold cyan]"
                if self._name_filter
                else ""
            )
        )
        return list_secrets(self._vault_name, self._name_filter)

    def item_label(self, item: str) -> str:
        return item

    def display_item(self, item: str) -> dict[str, str]:
        secret = get_secret(self._vault_name, item)
        self._selected_secrets.append(secret)
        return {secret.name: secret.value}


# ── Convenience function (backwards-compatible) ───────────────────────────────


def search_secrets_with_fzf(
    vault_name: str,
    name_filter: Optional[str] = None,
) -> List[Secret]:
    """
    Search Key Vault secrets using fzf for interactive selection.

    Returns:
        List of selected Secret objects.
    """
    view = AzureSecretsView(vault_name=vault_name, name_filter=name_filter)
    view.run()
