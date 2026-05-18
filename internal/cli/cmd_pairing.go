package cli

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/spf13/cobra"
)

const operatorPairingBasePath = "/operator/pairing"

type pairingListCLIResponse struct {
	Items []pairing.PairingRecord `json:"items"`
	Count int                     `json:"count"`
}

type pairingRecordCLIResponse struct {
	Record pairing.PairingRecord `json:"record"`
}

type pairingInitiateCLIRequest struct {
	Channel     string `json:"channel"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name,omitempty"`
}

type pairingVerifyCLIRequest struct {
	Code string `json:"code"`
}

func newPairingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairing",
		Short: "Manage channel pairing records",
		Long:  "List, initiate, verify, and revoke channel pairing records on the running gateway.",
	}
	cmd.AddCommand(
		newPairingListCmd(),
		newPairingInitiateCmd(),
		newPairingVerifyCmd(),
		newPairingRevokeCmd(),
	)
	return cmd
}

func newPairingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pairing records",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPairingList(cmd.Context())
		},
	}
}

func newPairingInitiateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "initiate <channel> <user-id>",
		Short: "Create or refresh a pairing code",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingInitiate(cmd.Context(), args[0], args[1], displayName)
		},
	}
	cmd.Flags().StringVar(&displayName, "name", "", "optional display name for the pairing record")
	return cmd
}

func newPairingVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <code>",
		Short: "Verify a pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingVerify(cmd.Context(), args[0])
		},
	}
}

func newPairingRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <channel> <user-id>",
		Short: "Revoke a pairing record",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingRevoke(cmd.Context(), args[0], args[1])
		},
	}
}

func runPairingList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp pairingListCLIResponse
	if err := client.Get(ctx, operatorPairingBasePath, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no pairing records found")
		return nil
	}
	fmt.Printf("%-20s  %-20s  %-10s  %-10s  %s\n", "CHANNEL", "USER", "STATUS", "CODE", "CREATED")
	for _, item := range resp.Items {
		fmt.Printf("%-20s  %-20s  %-10s  %-10s  %s\n", item.Channel, item.UserID, item.Status, item.Code, formatTime(item.CreatedAt))
	}
	return nil
}

func runPairingInitiate(ctx context.Context, channel, userID, displayName string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp pairingRecordCLIResponse
	if err := client.Post(ctx, operatorPairingBasePath+"/initiate", pairingInitiateCLIRequest{
		Channel:     channel,
		UserID:      userID,
		DisplayName: displayName,
	}, &resp); err != nil {
		return err
	}
	return outputPairingRecord(resp)
}

func runPairingVerify(ctx context.Context, code string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp pairingRecordCLIResponse
	if err := client.Post(ctx, operatorPairingBasePath+"/verify", pairingVerifyCLIRequest{Code: code}, &resp); err != nil {
		return err
	}
	return outputPairingRecord(resp)
}

func runPairingRevoke(ctx context.Context, channel, userID string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := client.Delete(ctx, operatorPairingBasePath+"/"+channel+"/"+userID, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("pairing revoked for %s/%s\n", channel, userID)
	return nil
}

func outputPairingRecord(resp pairingRecordCLIResponse) error {
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("Channel: %s\n", resp.Record.Channel)
	fmt.Printf("User:    %s\n", resp.Record.UserID)
	fmt.Printf("Status:  %s\n", resp.Record.Status)
	if resp.Record.Code != "" {
		fmt.Printf("Code:    %s\n", resp.Record.Code)
	}
	if !resp.Record.CodeExpiresAt.IsZero() {
		fmt.Printf("Expires: %s\n", formatTime(resp.Record.CodeExpiresAt))
	}
	return nil
}
