package awsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/Rishang/cloudutil/internal/ui"
)

const federationEndpoint = "https://signin.aws.amazon.com/federation"

// FederationInput describes a console sign-in request.
type FederationInput struct {
	Duration    time.Duration
	Policy      json.RawMessage // optional inline policy to scope the session down
	Destination string
}

// FederatedConsoleURL builds an AWS Management Console sign-in URL from the
// current credentials using STS GetFederationToken.
func FederatedConsoleURL(ctx context.Context, cfg aws.Config, in FederationInput) (string, error) {
	client := sts.NewFromConfig(cfg)

	identity, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("could not resolve caller identity: %w", err)
	}
	arn := aws.ToString(identity.Arn)
	name := arn[strings.LastIndex(arn, "/")+1:]
	// GetFederationToken caps the federated name at 32 characters.
	if len(name) > 32 {
		name = name[:32]
	}

	seconds := int32(in.Duration.Seconds())
	ui.Info("Requesting federation token for '%s' (duration: %ds)...",
		ui.Yellow.Render(name), seconds)

	tokenInput := &sts.GetFederationTokenInput{
		Name:            aws.String(name),
		DurationSeconds: aws.Int32(seconds),
	}
	if len(in.Policy) > 0 {
		tokenInput.Policy = aws.String(string(in.Policy))
		ui.Info("Applying inline policy to the federated session.")
	}

	token, err := client.GetFederationToken(ctx, tokenInput)
	if err != nil {
		return "", fmt.Errorf("could not get federation token: %w", err)
	}
	ui.Ok("Federation token received.")

	session, err := json.Marshal(map[string]string{
		"sessionId":    aws.ToString(token.Credentials.AccessKeyId),
		"sessionKey":   aws.ToString(token.Credentials.SecretAccessKey),
		"sessionToken": aws.ToString(token.Credentials.SessionToken),
	})
	if err != nil {
		return "", err
	}

	ui.Info("Requesting sign-in token from the AWS federation endpoint...")
	signinToken, err := fetchSigninToken(ctx, string(session), seconds)
	if err != nil {
		return "", err
	}
	ui.Ok("Sign-in token received.")

	params := url.Values{
		"Action":      {"login"},
		"Destination": {in.Destination},
		"SigninToken": {signinToken},
	}
	ui.Ok("Console login URL generated (session valid for %ds).", seconds)
	return federationEndpoint + "?" + params.Encode(), nil
}

func fetchSigninToken(ctx context.Context, session string, seconds int32) (string, error) {
	params := url.Values{
		"Action":          {"getSigninToken"},
		"Session":         {session},
		"SessionDuration": {strconv.Itoa(int(seconds))},
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
