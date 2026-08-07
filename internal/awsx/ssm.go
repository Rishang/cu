package awsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// portForwardDocument is the SSM document used for tunneling to a remote host.
const portForwardDocument = "AWS-StartPortForwardingSessionToRemoteHost"

// Parameter is one SSM Parameter Store entry.
type Parameter struct {
	Name  string
	Value string
}

// ListParameters returns every parameter name under prefix.
func ListParameters(ctx context.Context, cfg aws.Config, prefix string) ([]string, error) {
	client := ssm.NewFromConfig(cfg)
	// No WithDecryption: only names are used here, and decrypting every
	// SecureString on the path would need kms:Decrypt just to list.
	paginator := ssm.NewGetParametersByPathPaginator(client, &ssm.GetParametersByPathInput{
		Path:      aws.String(prefix),
		Recursive: aws.Bool(true),
	})

	var names []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range page.Parameters {
			names = append(names, aws.ToString(p.Name))
		}
	}
	return names, nil
}

// GetParameter fetches one decrypted parameter.
func GetParameter(ctx context.Context, cfg aws.Config, name string) (Parameter, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return Parameter{}, err
	}
	return Parameter{
		Name:  aws.ToString(out.Parameter.Name),
		Value: aws.ToString(out.Parameter.Value),
	}, nil
}

// Instance is a running EC2 instance reachable through SSM.
type Instance struct {
	ID   string
	Name string
}

// Label is the fzf line for this instance.
func (i Instance) Label() string { return i.ID + " | " + i.Name }

// ListInstances returns running, Name-tagged instances that carry an IAM
// instance profile, which is what SSM needs to reach them.
func ListInstances(ctx context.Context, cfg aws.Config) ([]Instance, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:Name"), Values: []string{"*"}},
			{Name: aws.String("instance-state-name"), Values: []string{"running"}},
		},
	})

	var instances []Instance
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				if instance.IamInstanceProfile == nil {
					continue
				}
				name := ""
				for _, tag := range instance.Tags {
					if aws.ToString(tag.Key) == "Name" {
						name = aws.ToString(tag.Value)
						break
					}
				}
				instances = append(instances, Instance{
					ID:   aws.ToString(instance.InstanceId),
					Name: name,
				})
			}
		}
	}
	return instances, nil
}

// Tunnel describes an optional port-forwarding session.
type Tunnel struct {
	Enabled    bool
	RemoteHost string
	RemotePort int
	LocalPort  int
}

// StartSession opens an SSM session via the SDK and replaces this process
// with session-manager-plugin, which owns the terminal for the session's
// duration and speaks the actual data-channel protocol.
//
// session-manager-plugin is a separate install (there's no Go client for its
// protocol), but the aws CLI itself is not needed: the SDK starts the session
// and the plugin is invoked with the same argv the CLI would pass it.
func StartSession(ctx context.Context, cfg aws.Config, instanceID string, t Tunnel) error {
	binary, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return errors.New("session-manager-plugin not found in PATH — required for SSM sessions")
	}

	input := &ssm.StartSessionInput{Target: aws.String(instanceID)}
	if t.Enabled {
		if t.RemoteHost == "" || t.RemotePort == 0 || t.LocalPort == 0 {
			return errors.New("--remote-host, --remote-port and --local-port are all required for tunneling")
		}
		input.DocumentName = aws.String(portForwardDocument)
		input.Parameters = map[string][]string{
			"host":            {t.RemoteHost},
			"portNumber":      {strconv.Itoa(t.RemotePort)},
			"localPortNumber": {strconv.Itoa(t.LocalPort)},
		}
	}

	client := ssm.NewFromConfig(cfg)
	out, err := client.StartSession(ctx, input)
	if err != nil {
		return fmt.Errorf("ssm start-session failed: %w", err)
	}

	response, err := json.Marshal(sessionResponse{
		SessionId:  aws.ToString(out.SessionId),
		TokenValue: aws.ToString(out.TokenValue),
		StreamUrl:  aws.ToString(out.StreamUrl),
	})
	if err != nil {
		return fmt.Errorf("could not encode session response: %w", err)
	}
	request, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("could not encode session request: %w", err)
	}

	// ponytail: assumes the standard AWS partition endpoint format; wrong for
	// China/GovCloud, fix by resolving via the ssm client's endpoint resolver
	// if that's ever needed.
	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", cfg.Region)
	argv := []string{binary, string(response), cfg.Region, "StartSession", "", string(request), endpoint}
	return syscall.Exec(binary, argv, os.Environ())
}

// sessionResponse mirrors the JSON session-manager-plugin expects as its
// first argument, which does not match ssm.StartSessionOutput's field tags.
type sessionResponse struct {
	SessionId  string `json:"SessionId"`
	TokenValue string `json:"TokenValue"`
	StreamUrl  string `json:"StreamUrl"`
}
