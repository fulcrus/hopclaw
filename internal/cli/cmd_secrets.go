package cli

import (
	"encoding/json"
	"fmt"

	"github.com/fulcrus/hopclaw/keychain"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// secrets command group
// ---------------------------------------------------------------------------

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets in the platform keychain",
	}

	cmd.AddCommand(
		newSecretsSetCmd(),
		newSecretsGetCmd(),
		newSecretsDeleteCmd(),
		newSecretsListCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// secrets set
// ---------------------------------------------------------------------------

func newSecretsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Store a secret in the keychain",
		Long:  "Store a secret in the platform keychain. Example: hopclaw secrets set openai-api-key sk-...",
		Args:  cobra.ExactArgs(2),
		RunE:  runSecretsSet,
	}
}

func runSecretsSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	if err := keychain.SaveSecret(key, value); err != nil {
		return fmt.Errorf("save secret: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "stored %q in keychain\n", key)
	return nil
}

// ---------------------------------------------------------------------------
// secrets get
// ---------------------------------------------------------------------------

func newSecretsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Retrieve a secret from the keychain",
		Long:  "Retrieve a secret from the platform keychain. Example: hopclaw secrets get openai-api-key",
		Args:  cobra.ExactArgs(1),
		RunE:  runSecretsGet,
	}
}

func runSecretsGet(cmd *cobra.Command, args []string) error {
	val, err := keychain.GetSecret(args[0])
	if err != nil {
		return fmt.Errorf("get secret: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), val)
	return nil
}

// ---------------------------------------------------------------------------
// secrets delete
// ---------------------------------------------------------------------------

func newSecretsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <key>",
		Short: "Remove a secret from the keychain",
		Long:  "Remove a secret from the platform keychain. Example: hopclaw secrets delete openai-api-key",
		Args:  cobra.ExactArgs(1),
		RunE:  runSecretsDelete,
	}
}

func runSecretsDelete(cmd *cobra.Command, args []string) error {
	if err := keychain.DeleteSecret(args[0]); err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "deleted %q from keychain\n", args[0])
	return nil
}

// ---------------------------------------------------------------------------
// secrets list
// ---------------------------------------------------------------------------

// wellKnownKeys lists the keys that HopClaw may store in the keychain.
// The list command probes each key to determine whether it exists.
var wellKnownKeys = []string{
	"openai-api-key",
	"anthropic-api-key",
	"google-api-key",
	"telegram-bot-token",
	"slack-bot-token",
	"slack-app-token",
	"discord-bot-token",
	"server-auth-token",
	"search-api-key",
	"speech-api-key",
	"email-password",
}

func newSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored secret keys (not values)",
		Long:  "List well-known secret keys that exist in the keychain. Values are not displayed.",
		RunE:  runSecretsList,
	}
}

type secretEntry struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

func runSecretsList(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	var entries []secretEntry

	for _, key := range wellKnownKeys {
		_, err := keychain.GetSecret(key)
		if err == nil {
			entries = append(entries, secretEntry{Key: key, Status: "stored"})
		}
	}

	if flagJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(w, "no secrets stored in keychain")
		return nil
	}

	for _, e := range entries {
		fmt.Fprintf(w, "  %s\n", e.Key)
	}
	return nil
}
