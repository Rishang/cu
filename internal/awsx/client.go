// Package awsx wraps the AWS SDK calls cu needs.
package awsx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// LoadConfig resolves AWS credentials, honoring optional profile and region
// overrides and otherwise falling back to the usual environment and config
// files. An empty override is not a value: the SDK's own getRegion and
// getSharedConfigProfile skip it and carry on down the chain.
func LoadConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx,
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region))
}
