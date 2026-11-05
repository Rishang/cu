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

// SSORegion returns the IAM Identity Center region of the profile cfg was
// loaded from, or "" when it has none. It reads the shared config LoadConfig
// already resolved instead of re-reading the files: the SDK has picked the
// profile and tolerated a missing one, so env-only credentials with no
// [default] section yield "" rather than an error.
func SSORegion(cfg aws.Config) string {
	for _, src := range cfg.ConfigSources {
		if shared, ok := src.(config.SharedConfig); ok {
			if shared.SSOSession != nil && shared.SSOSession.SSORegion != "" {
				return shared.SSOSession.SSORegion
			}
			return shared.SSORegion
		}
	}
	return ""
}
