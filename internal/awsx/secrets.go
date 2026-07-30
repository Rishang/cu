package awsx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Secret is one Secrets Manager entry.
type Secret struct {
	Name  string
	Value string
}

// ListSecrets returns secret names, optionally filtered by name prefix.
func ListSecrets(ctx context.Context, cfg aws.Config, nameFilter string) ([]string, error) {
	client := secretsmanager.NewFromConfig(cfg)
	input := &secretsmanager.ListSecretsInput{MaxResults: aws.Int32(100)}
	if nameFilter != "" {
		input.Filters = []smtypes.Filter{{
			Key:    smtypes.FilterNameStringTypeName,
			Values: []string{nameFilter},
		}}
	}

	paginator := secretsmanager.NewListSecretsPaginator(client, input)
	var names []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, secret := range page.SecretList {
			names = append(names, aws.ToString(secret.Name))
		}
	}
	return names, nil
}

// GetSecret fetches a secret's current value. GetSecretValue already returns the
// resolved name, so there is no DescribeSecret call and no secretsmanager:DescribeSecret
// permission to grant.
func GetSecret(ctx context.Context, cfg aws.Config, id string) (Secret, error) {
	out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx,
		&secretsmanager.GetSecretValueInput{SecretId: aws.String(id)})
	if err != nil {
		return Secret{}, err
	}
	return Secret{
		Name:  aws.ToString(out.Name),
		Value: aws.ToString(out.SecretString),
	}, nil
}

// DecodeAuthorizationMessage expands an encoded STS authorization failure message.
func DecodeAuthorizationMessage(ctx context.Context, cfg aws.Config, encoded string) (string, error) {
	out, err := sts.NewFromConfig(cfg).DecodeAuthorizationMessage(ctx,
		&sts.DecodeAuthorizationMessageInput{EncodedMessage: aws.String(encoded)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.DecodedMessage), nil
}
