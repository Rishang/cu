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
	paginator := ssm.NewGetParametersByPathPaginator(client, &ssm.GetParametersByPathInput{
		Path:           aws.String(prefix),
		Recursive:      aws.Bool(true),
		WithDecryption: aws.Bool(true),
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

// ListInstances returns running instances that carry an IAM instance profile,
// which is what SSM needs to reach them.
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

// StartSession replaces this process with `aws ssm start-session`, which owns
// the terminal for the duration of the session.
//
// The aws CLI is used rather than the SDK because an interactive session also
// needs the separately-installed session-manager-plugin.
func StartSession(instanceID string, t Tunnel) error {
	binary, err := exec.LookPath("aws")
	if err != nil {
		return errors.New("aws CLI not found in PATH — required for SSM sessions")
	}

	argv := []string{"aws", "ssm", "start-session", "--target", instanceID}
	if t.Enabled {
		if t.RemoteHost == "" || t.RemotePort == 0 || t.LocalPort == 0 {
			return errors.New("--remote-host, --remote-port and --local-port are all required for tunneling")
		}
		// Marshalled as a single argv entry: no shell, so no quoting to get wrong.
		params, err := json.Marshal(map[string][]string{
			"host":            {t.RemoteHost},
			"portNumber":      {strconv.Itoa(t.RemotePort)},
			"localPortNumber": {strconv.Itoa(t.LocalPort)},
		})
		if err != nil {
			return fmt.Errorf("could not encode tunnel parameters: %w", err)
		}
		argv = append(argv, "--document-name", portForwardDocument, "--parameters", string(params))
	}

	return syscall.Exec(binary, argv, os.Environ())
}
