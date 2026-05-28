"""AWS SSM (Systems Manager) Parameter Store utilities."""

import os
from typing import List, Optional

from pydantic import BaseModel

from ..helper.fzf_view import FzfView
from ..utils import console
from .common import get_aws_client


class SSMParameter(BaseModel):
    """SSM Parameter model."""

    name: str
    value: str


def get_ssm_client(
    profile_name: Optional[str] = None, region_name: Optional[str] = None
):
    """Get SSM client with optional profile and region."""
    return get_aws_client("ssm", profile_name, region_name)


def list_parameters(
    prefix: str = "/",
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> List[str]:
    """List SSM parameters by path prefix."""
    ssm_client = get_ssm_client(profile_name, region_name)
    parameters = []
    next_token = None

    while True:
        kwargs: dict = {
            "Path": prefix,
            "Recursive": True,
            "WithDecryption": True,
            "MaxResults": 10,
        }
        if next_token:
            kwargs["NextToken"] = next_token

        response = ssm_client.get_parameters_by_path(**kwargs)
        for param in response["Parameters"]:
            parameters.append(param["Name"])
        next_token = response.get("NextToken")
        if not next_token:
            break

    return parameters


def get_parameter(
    name: str,
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> SSMParameter:
    """Get a specific SSM parameter by name."""
    ssm_client = get_ssm_client(profile_name, region_name)
    response = ssm_client.get_parameter(Name=name, WithDecryption=True)
    param = response["Parameter"]
    return SSMParameter(name=param["Name"], value=param["Value"])


# ── FzfView subclass ──────────────────────────────────────────────────────────


class SSMParametersView(FzfView[str]):
    """
    Interactive fzf viewer for AWS SSM Parameter Store.

    Lists parameter names, lets the user pick via fzf, then fetches and
    returns the selected ``SSMParameter`` objects.
    """

    item_type_name = "SSM parameter"
    multi_select = True

    def __init__(
        self,
        prefix: str = "/",
        profile_name: Optional[str] = None,
        region_name: Optional[str] = None,
    ) -> None:
        self._prefix = prefix
        self._profile_name = profile_name
        self._region_name = region_name
        self._selected_params: List[SSMParameter] = []

    def list_items(self) -> List[str]:
        console.print(
            f"[*] Listing SSM parameters with prefix: [bold cyan]{self._prefix}[/bold cyan]"
        )
        return list_parameters(self._prefix, self._profile_name, self._region_name)

    def item_label(self, item: str) -> str:
        return item

    def display_item(self, item: str) -> dict[str, str]:
        param = get_parameter(item, self._profile_name, self._region_name)
        self._selected_params.append(param)
        return {param.name: param.value}


# ── EC2 / SSM Session helpers (unchanged) ────────────────────────────────────


def ssm_instance(
    instance_id: str,
    tunnel: bool = False,
    document_name: str = "AWS-StartPortForwardingSessionToRemoteHost",
    remote_host: str = "",
    remote_port: int = 0,
    local_port: int = 0,
) -> None:
    if tunnel:
        if not remote_host or remote_port == 0 or local_port == 0:
            console.print(
                "[bold red][!] ERROR: Remote host, remote port, and local port "
                "are required for tunneling.[/bold red]"
            )
            return

        os.system(
            f"aws ssm start-session --target {instance_id} "
            f"--document-name {document_name} "
            f"--parameters '{{'host': ['{remote_host}'], "
            f"'portNumber': ['{remote_port}'], "
            f"'localPortNumber': ['{local_port}']}}'",
        )
    else:
        os.system(f"aws ssm start-session --target {instance_id}")


def list_ssm_instances() -> List[dict[str, str]]:
    ec2 = get_aws_client("ec2")
    response = ec2.describe_instances(Filters=[{"Name": "tag:Name", "Values": ["*"]}])
    instances = []
    for reservation in response["Reservations"]:
        for instance in reservation["Instances"]:
            if "IamInstanceProfile" not in instance:
                continue
            name = ""
            for tag in instance.get("Tags", []):
                if tag["Key"] == "Name":
                    name = tag["Value"]
                    break
            instances.append({"instance_id": instance["InstanceId"], "name": name})
    return instances


# ── FzfView subclass for EC2 instance selection ───────────────────────────────


class EC2InstanceView(FzfView[dict]):
    """
    Interactive fzf viewer for EC2 instances reachable via SSM.

    Lists instances, lets the user pick one via fzf, then starts an SSM
    session (or tunnel) to the selected instance.
    """

    item_type_name = "EC2 instance"
    multi_select = False

    def __init__(
        self,
        tunnel: bool = False,
        remote_host: str = "",
        remote_port: int = 0,
        local_port: int = 0,
    ) -> None:
        self._tunnel = tunnel
        self._remote_host = remote_host
        self._remote_port = remote_port
        self._local_port = local_port

    def list_items(self) -> List[dict]:
        return list_ssm_instances()

    def item_label(self, item: dict) -> str:
        return f"{item['instance_id']} | {item['name']}"

    def display_item(self, item: dict) -> None:
        ssm_instance(
            instance_id=item["instance_id"],
            tunnel=self._tunnel,
            remote_host=self._remote_host,
            remote_port=self._remote_port,
            local_port=self._local_port,
        )


# ── Convenience function (backwards-compatible) ───────────────────────────────


def search_parameters_with_fzf(
    prefix: str = "/",
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
) -> None:
    """
    Search SSM parameters using fzf for interactive selection.

    Returns:
        List of selected SSMParameter objects.
    """
    SSMParametersView(
        prefix=prefix,
        profile_name=profile_name,
        region_name=region_name,
    ).run()
