package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/spf13/cobra"

	"github.com/Rishang/cloudutil/internal/pick"
	"github.com/Rishang/cloudutil/internal/ui"
)

func newAzureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "az",
		Short: "Azure-related commands",
	}
	cmd.AddCommand(newAzureSecretsCommand())
	return cmd
}

// azureSecret is one Key Vault secret.
type azureSecret struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	ID          string `json:"id,omitempty"`
	Description string `json:"description,omitempty"`
}

func newAzureSecretsCommand() *cobra.Command {
	var (
		vault      string
		nameFilter string
		output     string
	)

	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Search Key Vault secrets interactively and print the selection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output != "text" && output != "json" {
				ui.Error("Invalid --output %q: expected text or json.", output)
				return exitWith(1)
			}

			client, err := newKeyVaultClient(vault)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			if nameFilter != "" {
				ui.Info("Listing secrets from vault %s with filter: %s",
					ui.Cyan.Render(vault), ui.Cyan.Render(nameFilter))
			} else {
				ui.Info("Listing secrets from vault %s", ui.Cyan.Render(vault))
			}

			names, err := listKeyVaultSecrets(ctx, client, nameFilter)
			if err != nil {
				return err
			}
			selected, err := pickStrings(names, "Azure Key Vault secret",
				pick.Options{Multi: true, Prompt: "secret> "})
			if err != nil || len(selected) == 0 {
				return err
			}

			secrets := make([]azureSecret, 0, len(selected))
			for _, name := range selected {
				secret, err := getKeyVaultSecret(ctx, client, name)
				if err != nil {
					return err
				}
				secrets = append(secrets, secret)
			}

			if output == "json" {
				return ui.PrintJSON(secrets)
			}
			printAzureSecretsText(secrets)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&vault, "vault", "v", "", "Name of the Azure Key Vault.")
	flags.StringVar(&nameFilter, "filter", "", "Filter secrets by name prefix.")
	flags.StringVarP(&output, "output", "o", "text", "Output format (text/json).")
	_ = cmd.MarkFlagRequired("vault")
	return cmd
}

func newKeyVaultClient(vault string) (*azsecrets.Client, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("could not resolve Azure credentials: %w", err)
	}
	url := fmt.Sprintf("https://%s.vault.azure.net/", vault)
	return azsecrets.NewClient(url, credential, nil)
}

func listKeyVaultSecrets(ctx context.Context, client *azsecrets.Client, nameFilter string) ([]string, error) {
	pager := client.NewListSecretPropertiesPager(nil)
	var names []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, property := range page.Value {
			if property == nil || property.ID == nil {
				continue
			}
			name := property.ID.Name()
			if nameFilter != "" && !strings.HasPrefix(name, nameFilter) {
				continue
			}
			names = append(names, name)
		}
	}
	return names, nil
}

func getKeyVaultSecret(ctx context.Context, client *azsecrets.Client, name string) (azureSecret, error) {
	resp, err := client.GetSecret(ctx, name, "", nil)
	if err != nil {
		return azureSecret{}, err
	}

	secret := azureSecret{Name: name}
	if resp.Value != nil {
		secret.Value = *resp.Value
	}
	if resp.ID != nil {
		secret.ID = string(*resp.ID)
	}
	if resp.ContentType != nil {
		secret.Description = *resp.ContentType
	}
	return secret, nil
}

func printAzureSecretsText(secrets []azureSecret) {
	for _, secret := range secrets {
		ui.Printf("Name: '%s'", secret.Name)
		if secret.Description != "" {
			ui.Printf("Description: '%s'", secret.Description)
		}
		if secret.ID != "" {
			ui.Printf("ID: '%s'", secret.ID)
		}

		// decodeSecretValue hands back the raw string unless the value is a
		// JSON object, in which case it is printed as nested JSON.
		switch parsed := decodeSecretValue(secret.Value).(type) {
		case string:
			fmt.Fprintf(ui.Out, "Value: %s\n", parsed)
		default:
			ui.Print("Value (JSON):")
			if err := ui.PrintJSON(parsed); err != nil {
				return
			}
		}
		ui.Rule("", ui.Dim)
		ui.Print("")
	}
}
