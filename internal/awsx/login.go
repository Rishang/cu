package awsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/Rishang/cloudutil/internal/ui"
)

const federationEndpoint = "https://signin.aws.amazon.com/federation"

// FederatedConsoleURL builds an AWS Management Console sign-in URL for region
// from the current credentials, using STS GetFederationToken. policy is an
// optional inline policy scoping the session down.
func FederatedConsoleURL(ctx context.Context, cfg aws.Config, region string,
	duration time.Duration, policy json.RawMessage,
) (string, error) {
	client := sts.NewFromConfig(cfg)
	credentials, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return "", fmt.Errorf("could not retrieve AWS credentials: %w", err)
	}

	seconds := strconv.Itoa(int(duration.Seconds()))
	var session []byte
	if credentials.CanExpire {
		// GetFederationToken rejects session credentials, including SSO and roles.
		ui.Warn("Using temporary AWS credentials; the policy file cannot scope this console session.")
		ui.Info("Using existing AWS session credentials (requested duration: %ss)...", seconds)
		session, err = json.Marshal(map[string]string{
			"sessionId":    credentials.AccessKeyID,
			"sessionKey":   credentials.SecretAccessKey,
			"sessionToken": credentials.SessionToken,
		})
		if err != nil {
			return "", err
		}
	} else {
		identity, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", fmt.Errorf("could not resolve caller identity: %w", err)
		}
		// GetFederationToken caps the federated name at 32 characters.
		name := path.Base(aws.ToString(identity.Arn))
		name = name[:min(len(name), 32)]
		ui.Info("Requesting federation token for '%s' (duration: %ss)...",
			ui.Yellow.Render(name), seconds)

		tokenInput := &sts.GetFederationTokenInput{
			Name:            aws.String(name),
			DurationSeconds: aws.Int32(int32(duration.Seconds())),
		}
		if len(policy) > 0 {
			tokenInput.Policy = aws.String(string(policy))
		}

		token, err := client.GetFederationToken(ctx, tokenInput)
		if err != nil {
			return "", fmt.Errorf("could not get federation token: %w", err)
		}

		session, err = json.Marshal(map[string]string{
			"sessionId":    aws.ToString(token.Credentials.AccessKeyId),
			"sessionKey":   aws.ToString(token.Credentials.SecretAccessKey),
			"sessionToken": aws.ToString(token.Credentials.SessionToken),
		})
		if err != nil {
			return "", err
		}
	}

	signinToken, err := fetchSigninToken(ctx, string(session), seconds)
	if err != nil {
		return "", err
	}

	params := url.Values{
		"Action":      {"login"},
		"Destination": {fmt.Sprintf("https://%s.console.aws.amazon.com/", region)},
		"SigninToken": {signinToken},
	}
	ui.Ok("Console login URL generated (session valid for %ss).", seconds)
	return federationEndpoint + "?" + params.Encode(), nil
}

func fetchSigninToken(ctx context.Context, session, seconds string) (string, error) {
	params := url.Values{
		"Action":          {"getSigninToken"},
		"Session":         {session},
		"SessionDuration": {seconds},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		federationEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("federation endpoint request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("federation endpoint returned %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		SigninToken string `json:"SigninToken"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("could not parse federation response: %w", err)
	}
	if payload.SigninToken == "" {
		return "", errors.New("federation response contained no SigninToken")
	}
	return payload.SigninToken, nil
}
