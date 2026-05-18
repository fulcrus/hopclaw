package cli

import (
	"github.com/fulcrus/hopclaw/tui"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// tui command
// ---------------------------------------------------------------------------

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:    "tui",
		Short:  "Launch the legacy gateway monitor TUI",
		Long:   "Launch the legacy gateway monitor TUI. The default `hopclaw` command is the primary interactive terminal.",
		Hidden: true,
		RunE:   runTUI,
	}
}

func runTUI(_ *cobra.Command, _ []string) error {
	access, err := resolveGatewayAccess()
	if err != nil {
		return err
	}

	app := tui.New(access.Address, access.AuthToken)
	return app.Run()
}
