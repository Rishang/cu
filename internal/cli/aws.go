package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/awsx"
	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

func newAWSCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aws",
		Short: "AWS-related commands",
	}
	cmd.AddCommand(
		newAWSLoginCommand(),
		newSSMParametersCommand(),
		newEC2SSMCommand(),
		newAWSSecretsCommand(),
		newDecodeMessageCommand(),
	)
	return cmd
}

// awsProfileFlags are the credential overrides shared by most aws subcommands.
type awsProfileFlags struct {
	profile string
	region  string
}

func (f *awsProfileFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.profile, "profile", "p", "", "AWS CLI profile name to use.")
	cmd.Flags().StringVarP(&f.region, "region", "r", "", "AWS region to use.")
}

func newAWSLoginCommand() *cobra.Command {
	var (
		creds      awsProfileFlags
		duration   int
		policyFile string
		noOpen     bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Generate an AWS console login URL and open it",
		Long: "Generates an AWS Management Console sign-in URL using STS " +
			"GetFederationToken and opens it in your browser.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if policyFile == "" {
				ui.Error("No policy file provided, use -f <policy_file>.json")
				return exitWith(1)
			}
			if duration < 1 || duration > 24 {
				ui.Error("--duration must be between 1 and 24 hours, got %d.", duration)
				return exitWith(1)
			}

			policy, err := os.ReadFile(policyFile)
			if err != nil {
				ui.Error("Could not read policy file %q: %v", policyFile, err)
				return exitWith(1)
			}
			if !json.Valid(policy) {
				ui.Error("Policy file %q is not valid JSON.", policyFile)
				return exitWith(1)
			}
			ui.Info("Using policy from file: %s", ui.Cyan.Render(policyFile))

			ctx := cmd.Context()
			cfg, err := awsx.LoadConfig(ctx, creds.profile, creds.region)
			if err != nil {
				return err
			}

			// LoadConfig already applied --region, so cfg.Region is the
			// resolved one; fall back only when nothing resolved at all.
			region := cfg.Region
			if region == "" {
				region = "us-east-1"
			}

			profileLabel := creds.profile
			if profileLabel == "" {
				profileLabel = os.Getenv("AWS_PROFILE")
			}
			if profileLabel == "" {
				profileLabel = "default"
			}
			ui.Info("Using AWS (profile: %s, region: %s)",
				ui.Cyan.Render(profileLabel), ui.Cyan.Render(region))

			url, err := awsx.FederatedConsoleURL(ctx, cfg, awsx.FederationInput{
				Duration:    time.Duration(duration) * time.Hour,
				Policy:      policy,
				Destination: fmt.Sprintf("https://%s.console.aws.amazon.com/", region),
			})
			if err != nil {
				return err
			}

			if noOpen {
				ui.Print(ui.BoldYellow.Render("\nGenerated Console Login URL:"))
				fmt.Fprintln(ui.Out, url)
				return nil
			}
			if err := openBrowser(url); err != nil {
				ui.Warn("Could not open a browser (%v). Copy the URL manually:", err)
				fmt.Fprintln(ui.Out, url)
				return nil
			}
			ui.Ok("Done. Check your browser.")
			return nil
		},
	}

	creds.register(cmd)
	cmd.Flags().IntVarP(&duration, "duration", "d", 2,
		"Duration for temporary credentials in hours (1-24).")
	cmd.Flags().StringVarP(&policyFile, "policy-file", "f", "",
		"Path to a JSON file containing an IAM policy to scope down permissions.")
	cmd.Flags().BoolVar(&noOpen, "no-open", false,
		"Print the URL instead of opening a browser.")
	return cmd
}

func newSSMParametersCommand() *cobra.Command {
	var (
		creds  awsProfileFlags
		prefix string
	)

	cmd := &cobra.Command{
		Use:   "ssm-parameters",
		Short: "Search SSM parameters interactively and print the selection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := awsx.LoadConfig(ctx, creds.profile, creds.region)
			if err != nil {
				return err
			}

			ui.Info("Listing SSM parameters with prefix: %s", ui.Cyan.Render(prefix))
			names, err := awsx.ListParameters(ctx, cfg, prefix)
			if err != nil {
				return err
			}
			return pickAndPrint(names, itself, "SSM parameter", "parameter> ",
				func(name string) (string, string, error) {
					param, err := awsx.GetParameter(ctx, cfg, name)
					return param.Name, param.Value, err
				})
		},
	}

	creds.register(cmd)
	cmd.Flags().StringVar(&prefix, "prefix", "/", "SSM path prefix to search.")
	return cmd
}

func newEC2SSMCommand() *cobra.Command {
	var (
		creds  awsProfileFlags
		tunnel awsx.Tunnel
	)

	cmd := &cobra.Command{
		Use:   "ec2-ssm",
		Short: "Start an SSM session or tunnel to a running EC2 instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := awsx.LoadConfig(ctx, creds.profile, creds.region)
			if err != nil {
				return err
			}

			instances, err := awsx.ListInstances(ctx, cfg)
			if err != nil {
				return err
			}
			selected, err := pickFrom(instances, awsx.Instance.Label, "EC2 instance",
				pick.Options{Prompt: "instance> "})
			if err != nil || len(selected) == 0 {
				return err
			}

			// Replaces this process, so nothing after this line runs.
			return awsx.StartSession(selected[0].ID, tunnel)
		},
	}

	creds.register(cmd)
	flags := cmd.Flags()
	flags.BoolVar(&tunnel.Enabled, "tunnel", false, "Tunnel to the instance.")
	flags.StringVar(&tunnel.RemoteHost, "remote-host", "", "Remote host to tunnel to.")
	flags.IntVar(&tunnel.RemotePort, "remote-port", 0, "Remote port to tunnel to.")
	flags.IntVar(&tunnel.LocalPort, "local-port", 0, "Local port to forward.")
	return cmd
}

func newAWSSecretsCommand() *cobra.Command {
	var (
		creds      awsProfileFlags
		nameFilter string
	)

	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Search Secrets Manager secrets interactively and print the selection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := awsx.LoadConfig(ctx, creds.profile, creds.region)
			if err != nil {
				return err
			}

			if nameFilter != "" {
				ui.Info("Listing secrets with filter: %s", ui.Cyan.Render(nameFilter))
			} else {
				ui.Info("Listing secrets")
			}
			names, err := awsx.ListSecrets(ctx, cfg, nameFilter)
			if err != nil {
				return err
			}
			// A JSON secret is nested in the output rather than escaped into a
			// string, so jq can walk into it.
			return pickAndPrint(names, itself, "AWS secret", "secret> ",
				func(name string) (string, any, error) {
					secret, err := awsx.GetSecret(ctx, cfg, name)
					return secret.Name, decodeSecretValue(secret.Value), err
				})
		},
	}

	creds.register(cmd)
	cmd.Flags().StringVar(&nameFilter, "filter", "", "Filter secrets by name prefix.")
	return cmd
}

func newDecodeMessageCommand() *cobra.Command {
	var (
		creds   awsProfileFlags
		message string
	)

	cmd := &cobra.Command{
		Use:   "decode-message",
		Short: "Decode an AWS authorization failure message",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			encoded := strings.TrimSpace(message)
			if encoded == "" {
				edited, err := captureFromEditor("encoded_message.txt")
				if err != nil {
					return err
				}
				encoded = strings.TrimSpace(edited)
			}
			if encoded == "" {
				ui.Error("No encoded message provided.")
				return exitWith(1)
			}

			ctx := cmd.Context()
			cfg, err := awsx.LoadConfig(ctx, creds.profile, creds.region)
			if err != nil {
				return err
			}
			decoded, err := awsx.DecodeAuthorizationMessage(ctx, cfg, encoded)
			if err != nil {
				return err
			}
			fmt.Fprintln(ui.Out, decoded)
			return nil
		},
	}

	creds.register(cmd)
	cmd.Flags().StringVar(&message, "message", "", "Encoded authorization failure message.")
	return cmd
}
