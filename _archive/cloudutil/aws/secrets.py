"""AWS Secrets Manager utilities."""

import json
from typing import Any, Dict, List, Optional

from pydantic import BaseModel

from ..helper.fzf_view import FzfView
from ..utils import console
from .common import get_aws_client


class Secret(BaseModel):
    """Secret model."""

    name: str
    value: str
    arn: Optional[str] = None
    description: Optional[str] = None


def get_secrets_client(
    profile_name: Optional[str] = None, region_name: Optional[str] = None
):
    """Get Secrets Manager client with optional profile and region."""
    return get_aws_client("secretsmanager", profile_name, region_name)


def list_secrets(
    name_filter: Optional[str] = None,
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> List[str]:
    """List Secrets Manager secret names."""
    secrets_client = get_secrets_client(profile_name, region_name)
    secrets = []
    next_token = None

    while True:
        kwargs: dict = {"MaxResults": 100}
        if name_filter is not None:
            kwargs["Filters"] = [{"Key": "name", "Values": [name_filter]}]
        if next_token:
            kwargs["NextToken"] = next_token

        response = secrets_client.list_secrets(**kwargs)
        for secret in response["SecretList"]:
            secrets.append(secret["Name"])
        next_token = response.get("NextToken")
        if not next_token:
            break

    return secrets


def get_secret(
    name: str,
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> Secret:
    """Get a specific secret by name."""
    secrets_client = get_secrets_client(profile_name, region_name)
    describe_response = secrets_client.describe_secret(SecretId=name)
    value_response = secrets_client.get_secret_value(SecretId=name)

    return Secret(
        name=describe_response["Name"],
        value=value_response["SecretString"],
        arn=describe_response["ARN"],
        description=describe_response.get("Description"),
    )


def get_secret_json(
    name: str,
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> Dict[str, Any]:
    """Get a secret value and parse it as JSON."""
    secret = get_secret(name, profile_name, region_name)
    try:
        return json.loads(secret.value)
    except json.JSONDecodeError as e:
        console.print(
            f"[bold red][!] ERROR: Failed to parse secret as JSON: {e}[/bold red]"
        )
        raise


# ── FzfView subclass ──────────────────────────────────────────────────────────


class AwsSecretsView(FzfView[str]):
    """
    Interactive fzf viewer for AWS Secrets Manager.

    Lists secret names, lets the user pick via fzf, then fetches and
    returns the selected ``Secret`` objects.
    """

    item_type_name = "AWS secret"
    multi_select = True

    def __init__(
        self,
        name_filter: Optional[str] = None,
        profile_name: Optional[str] = None,
        region_name: Optional[str] = None,
    ) -> None:
        self._name_filter = name_filter
        self._profile_name = profile_name
        self._region_name = region_name
        self._selected_secrets: List[Secret] = []

    def list_items(self) -> List[str]:
        console.print(
            "[*] Listing secrets"
            + (
                f" with filter: [bold cyan]{self._name_filter}[/bold cyan]"
                if self._name_filter
                else ""
            )
        )
        return list_secrets(self._name_filter, self._profile_name, self._region_name)

    def item_label(self, item: str) -> str:
        return item

    def display_item(self, item: str) -> dict[str, dict]:
        secret = get_secret(item, self._profile_name, self._region_name)
        self._selected_secrets.append(secret)
        return {secret.name: json.loads(secret.value)}


# ── Convenience function (backwards-compatible) ───────────────────────────────


def search_secrets_with_fzf(
    name_filter: Optional[str] = None,
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> List[Secret]:
    """
    Search secrets using fzf for interactive selection.

    Returns:
        List of selected Secret objects.
    """
    view = AwsSecretsView(
        name_filter=name_filter,
        profile_name=profile_name,
        region_name=region_name,
    )
    view.run()
