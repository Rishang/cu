"""Common AWS utilities and session management."""

import boto3
from typing import Optional


def get_aws_session(
    profile_name: Optional[str] = None, region_name: Optional[str] = None
) -> boto3.Session:
    """
    Get AWS session with optional profile and region.

    Args:
        profile_name: AWS profile to use
        region_name: AWS region to use

    Returns:
        Configured boto3 Session
    """
    if profile_name and region_name:
        return boto3.Session(profile_name=profile_name, region_name=region_name)
    elif profile_name:
        return boto3.Session(profile_name=profile_name)
    elif region_name:
        return boto3.Session(region_name=region_name)
    else:
        return boto3.Session()


def get_aws_client(
    service_name: str,
    profile_name: Optional[str] = None,
    region_name: Optional[str] = None,
):
    """
    Get AWS service client with optional profile and region.

    Args:
        service_name: AWS service name (e.g., 'ssm', 'secretsmanager')
        profile_name: AWS profile to use
        region_name: AWS region to use

    Returns:
        Configured boto3 client for the specified service
    """
    session = get_aws_session(profile_name, region_name)
    return session.client(service_name)
