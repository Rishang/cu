"""AWS console login URL generation using STS GetFederationToken."""

import os
import boto3
import botocore.exceptions
import requests
import json
import urllib.parse
from typing import Optional

from ..utils import console


def generate_federated_console_url(
    profile_name: Optional[str] = None,
    region_name: Optional[str] = "us-east-1",
    duration_seconds: int = 3600,
    policy_document: Optional[dict] = None,
    destination_url: str = "https://console.aws.amazon.com/",
) -> Optional[str]:
    """
    Generate an AWS Management Console sign-in URL using STS GetFederationToken.

    Args:
        profile_name: AWS CLI profile name to use
        region_name: AWS region for STS client (default: us-east-1)
        duration_seconds: Duration for temporary credentials in seconds (900-43200)
        policy_document: Optional IAM policy to scope down permissions
        destination_url: Target console URL after login

    Returns:
        AWS console login URL if successful, None otherwise
    """
    try:
        profile_msg = f"profile: [bold cyan]{profile_name if profile_name else os.getenv('AWS_PROFILE')}[/]"
        region_msg = f", region: [bold cyan]{region_name if region_name else 'default (from profile/env)'}[/]"
        console.print(f"[*] Using AWS ({profile_msg}{region_msg})")

        session_kwargs = {}
        if profile_name:
            session_kwargs["profile_name"] = profile_name
        if region_name:
            # Note: STS is a global service, but client can be regional for endpoint discovery.
            # Explicitly setting region for STS client is often not needed unless sourcing creds from a specific region.
            session_kwargs["region_name"] = region_name

        aws_session = boto3.Session(**session_kwargs)
        sts_client = aws_session.client("sts")
        caller_identity = sts_client.get_caller_identity()
        iam_arn = caller_identity["Arn"]
        iam_username = iam_arn.split("/")[-1]
        # Ensure Name length <= 32 characters for federation token requirement
        federated_name = iam_username if len(iam_username) <= 32 else iam_username[:32]

        console.print(
            f"[*] Requesting federation token for IAM user '[bold yellow]{iam_username}[/]' (duration: {duration_seconds}s)..."
        )
        sts_params = {
            "Name": federated_name,
            "DurationSeconds": duration_seconds,
        }
        if policy_document:
            sts_params["Policy"] = json.dumps(policy_document)
            console.print("[*] Applying inline policy to the federated session.")

        response = sts_client.get_federation_token(**sts_params)
        creds = response["Credentials"]
        console.print("[green][+][/green] Federation token received.")

        session_credentials = {
            "sessionId": creds["AccessKeyId"],
            "sessionKey": creds["SecretAccessKey"],
            "sessionToken": creds["SessionToken"],
        }

        session_json = json.dumps(session_credentials)

        console.print("[*] Requesting sign-in token from AWS federation endpoint...")
        signin_token_request_url = "https://signin.aws.amazon.com/federation"
        signin_token_params = {
            "Action": "getSigninToken",
            "Session": session_json,
            "SessionDuration": duration_seconds,  # Can be redundant if already in token time an
        }

        r = requests.get(signin_token_request_url, params=signin_token_params)
        r.raise_for_status()

        signin_token = r.json()["SigninToken"]
        console.print("[green][+][/green] Sign-in token received.")

        login_url_params = {
            "Action": "login",
            "Destination": destination_url,
            "SigninToken": signin_token,
        }
        login_url = (
            f"{signin_token_request_url}?{urllib.parse.urlencode(login_url_params)}"
        )

        console.print(
            f"[green][+][/green] Console login URL generated (short-lived for login, session valid for {duration_seconds}s)."
        )
        return login_url

    except botocore.exceptions.NoCredentialsError:
        console.print(
            "[bold red][!] ERROR: AWS credentials not found. Configure AWS CLI or set environment variables.[/bold red]"
        )
        return None
    except botocore.exceptions.BotoCoreError as e:
        console.print(f"[bold red][!] ERROR (Boto3): {e}[/bold red]")
        return None
    except requests.exceptions.RequestException as e:
        console.print(f"[bold red][!] ERROR (Requests): {e}[/bold red]")
        return None
    except KeyError as e:
        console.print(
            f"[bold red][!] ERROR: Could not parse response (KeyError: {e}). Check AWS response format.[/bold red]"
        )
        return None
    except Exception as e:
        console.print(f"[bold red][!] An unexpected error occurred: {e}[/bold red]")
        return None
