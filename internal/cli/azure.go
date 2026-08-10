package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/spf13/cobra"

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

func newAzureSecretsCommand() *cobra.Command {
	var (
		vault      string
		nameFilter string
	)

	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Search Key Vault secrets interactively and print the selection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newKeyVaultClient(vault)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			ui.Info("Listing secrets from vault %s", ui.Cyan.Render(vault))
			if nameFilter != "" {
				ui.Info("Filtering by name prefix: %s", ui.Cyan.Render(nameFilter))
			}
			names, err := listKeyVaultSecrets(ctx, client, nameFilter)
			if err != nil {
				return err
			}

			return pickAndPrint(names, itself, "Azure Key Vault secret", "secret> ",
				func(name string) (string, any, error) {
					value, err := getKeyVaultSecret(ctx, client, name)
					return name, value, err
				})
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&vault, "vault", "v", "", "Name of the Azure Key Vault.")
	flags.StringVar(&nameFilter, "filter", "", "Filter secrets by name prefix.")
	_ = cmd.MarkFlagRequired("vault")
	return cmd
}

func newKeyVaultClient(vault string) (*azsecrets.Client, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("could not resolve Azure credentials: %w", err)
	}
	url := fmt.Sprintf("https://%s.vault.azure.net/", vault)
	client, err := azsecrets.NewClient(url, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create a Key Vault client for %q: %w", vault, err)
	}
	return client, nil
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

// getKeyVaultSecret reads a secret's current value. Same as AWS and Vault: a
// value that is itself a JSON object is nested in the output rather than
// escaped into a string, so jq can walk into it.
func getKeyVaultSecret(ctx context.Context, client *azsecrets.Client, name string) (any, error) {
	resp, err := client.GetSecret(ctx, name, "", nil)
	if err != nil {
		return nil, err
	}
	if resp.Value == nil {
		return "", nil
	}
	return decodeSecretValue(*resp.Value), nil
}
