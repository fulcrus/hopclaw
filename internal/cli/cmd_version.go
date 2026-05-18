package cli

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/fulcrus/hopclaw/internal/edition"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE:  runVersion,
	}
}

type versionInfo struct {
	Version    string `json:"version"`
	Channel    string `json:"channel,omitempty"`
	GitCommit  string `json:"git_commit,omitempty"`
	BuildDate  string `json:"build_date,omitempty"`
	Edition    string `json:"edition"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Website    string `json:"website,omitempty"`
	Repository string `json:"repository,omitempty"`
}

func runVersion(cmd *cobra.Command, _ []string) error {
	info := versionInfo{
		Version:    version.Version,
		Channel:    version.Channel,
		GitCommit:  version.GitCommit,
		BuildDate:  version.BuildDate,
		Edition:    edition.Edition,
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Website:    version.DefaultWebsiteURL,
		Repository: version.DefaultRepository,
	}

	w := cmd.OutOrStdout()

	if flagJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	fmt.Fprintf(w, "hopclaw %s\n", version.Full())
	fmt.Fprintf(w, "edition:  %s\n", info.Edition)
	fmt.Fprintf(w, "channel:  %s\n", info.Channel)
	fmt.Fprintf(w, "go:       %s\n", info.GoVersion)
	fmt.Fprintf(w, "os/arch:  %s/%s\n", info.OS, info.Arch)
	return nil
}
