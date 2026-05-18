package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	qrDefaultPairingPath = "/dashboard"
	qrBoxWidth           = 52
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type qrConfig struct {
	URL     string `json:"url"`
	Token   string `json:"token,omitempty"`
	Session string `json:"session,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newQRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qr",
		Short: "Generate QR code URLs for pairing",
		Long:  "Generate pairing URLs for mobile devices and display connection information.",
	}
	cmd.AddCommand(
		newQRGenerateCmd(),
		newQRShowCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// qr generate
// ---------------------------------------------------------------------------

func newQRGenerateCmd() *cobra.Command {
	var (
		session string
		channel string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a pairing URL",
		Long: `Generate a pairing URL for mobile devices and display it with instructions.

Examples:
  hopclaw qr generate
  hopclaw qr generate --session my-session
  hopclaw qr generate --channel webchat`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runQRGenerate(cmd.Context(), session, channel)
		},
	}

	cmd.Flags().StringVar(&session, "session", "", "session key to embed in the pairing URL")
	cmd.Flags().StringVar(&channel, "channel", "", "channel name for the pairing URL")

	return cmd
}

func runQRGenerate(_ context.Context, session, channel string) error {
	access, err := resolveGatewayAccess()
	if err != nil {
		return err
	}
	return runQRGenerateWithAccess(access, session, channel)
}

func runQRGenerateWithAccess(access gatewayAccess, session, channel string) error {
	pairingURL := buildGatewayURL(access.BaseURL, qrDefaultPairingPath, buildQRQuery(session, channel))

	qrCfg := qrConfig{
		URL:     pairingURL,
		Token:   access.AuthToken,
		Session: session,
	}

	if flagJSON {
		return printJSON(qrCfg)
	}

	printQRDisplay(pairingURL, access.AuthToken)
	return nil
}

// ---------------------------------------------------------------------------
// qr show
// ---------------------------------------------------------------------------

func newQRShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show gateway connection URL",
		Long:  "Display the current gateway's connection URL with pairing instructions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runQRShow(cmd.Context())
		},
	}
}

func runQRShow(_ context.Context) error {
	access, err := resolveGatewayAccess()
	if err != nil {
		return err
	}
	return runQRShowWithAccess(access)
}

func runQRShowWithAccess(access gatewayAccess) error {
	qrCfg := qrConfig{
		URL:   access.BaseURL,
		Token: access.AuthToken,
	}

	if flagJSON {
		return printJSON(qrCfg)
	}

	printQRDisplay(access.BaseURL, access.AuthToken)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func printQRDisplay(url, authToken string) {
	border := strings.Repeat("=", qrBoxWidth)

	fmt.Println()
	fmt.Println(border)
	fmt.Println("  SCAN / OPEN THIS URL ON YOUR DEVICE")
	fmt.Println(border)
	fmt.Println()
	fmt.Printf("  %s\n", url)
	fmt.Println()
	if authToken != "" {
		fmt.Printf("  Auth token: %s\n", maskDisplayToken(authToken))
		fmt.Println()
	}
	fmt.Println(border)
	fmt.Println()
	fmt.Println("Open this URL in a browser or scan it with a QR code")
	fmt.Println("reader app on your mobile device to connect.")
	fmt.Println()
}
