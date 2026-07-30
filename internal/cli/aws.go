package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

			// Explicit --region wins; otherwise use whatever the profile or
			// environment resolved to, falling back to us-east-1.
			region := creds.region
			if region == "" {
				region = cfg.Region
			}
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
			selected, err := pickStrings(names, "SSM parameter",
				pick.Options{Multi: true, Prompt: "parameter> "})
			if err != nil || len(selected) == 0 {
				return err
			}

			payload := map[string]string{}
			for _, name := range selected {
				param, err := awsx.GetParameter(ctx, cfg, name)
				if err != nil {
					return err
				}
				payload[param.Name] = param.Value
			}
			return ui.PrintJSON(payload)
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
			selected, err := pickStrings(names, "AWS secret",
				pick.Options{Multi: true, Prompt: "secret> "})
			if err != nil || len(selected) == 0 {
				return err
			}

			payload := map[string]any{}
			for _, name := range selected {
				secret, err := awsx.GetSecret(ctx, cfg, name)
				if err != nil {
					return err
				}
				payload[secret.Name] = decodeSecretValue(secret.Value)
			}
			return ui.PrintJSON(payload)
		},
	}

	creds.register(cmd)
	cmd.Flags().StringVar(&nameFilter, "filter", "", "Filter secrets by name prefix.")
	return cmd
}

// decodeSecretValue nests JSON secrets in the output and passes anything else
// through as a plain string.
func decodeSecretValue(value string) any {
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) == nil {
		if _, isObject := parsed.(map[string]any); isObject {
			return parsed
		}
	}
	return value
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
				edited, err := captureFromEditor("encoded_message.txt", "")
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

// captureFromEditor opens $EDITOR on a temp file and returns what was written.
func captureFromEditor(name, initial string) (string, error) {
	dir, err := os.MkdirTemp("", "cu-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		return "", err
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %q failed: %w", editor, err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// openBrowser opens a URL with the platform's default handler.
func openBrowser(url string) error {
	openers := []string{"xdg-open", "open", "wslview"}
	for _, opener := range openers {
		if binary, err := exec.LookPath(opener); err == nil {
			ui.Info("Opening URL in your default browser (%s)...", opener)
			return exec.Command(binary, url).Start()
		}
	}
	return fmt.Errorf("no browser opener found (tried %s)", strings.Join(openers, ", "))
}
