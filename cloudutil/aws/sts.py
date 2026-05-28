from .common import get_aws_client


def decode_authorization_failure_message(encoded_message: str):
    # Initialize a Boto3 client for IAM
    client = get_aws_client("sts")

    try:
        # Call the decode_authorization_message API
        response = client.decode_authorization_message(EncodedMessage=encoded_message)

        # The decoded message will be in the 'DecodedMessage' field
        return response["DecodedMessage"]

    except Exception as e:
        # Handle any exceptions (e.g., invalid encoded message, issues with AWS credentials)
        return f"Error decoding message: {str(e)}"
