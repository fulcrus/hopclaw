// Package version holds build-time version metadata injected via ldflags.
package version

// Values are set via -ldflags at build time:
//
//	-X github.com/fulcrus/hopclaw/internal/version.Version=$(VERSION)
//	-X github.com/fulcrus/hopclaw/internal/version.GitCommit=$(GIT_COMMIT)
//	-X github.com/fulcrus/hopclaw/internal/version.BuildDate=$(BUILD_DATE)
//	-X github.com/fulcrus/hopclaw/internal/version.Channel=$(CHANNEL)
var (
	Version   = "dev"
	GitCommit = ""
	BuildDate = ""
	Channel   = "stable"
)

const (
	ProductName        = "HopClaw"
	DefaultWebsiteURL  = "https://hopclaw.com"
	DefaultRepository  = "https://github.com/fulcrus/hopclaw"
	DefaultReleasesURL = DefaultRepository + "/releases"
	DefaultManifestURL = DefaultWebsiteURL + "/releases/manifest.json"
)

// Full returns a human-readable version string including commit and date
// when available.
func Full() string {
	v := Version
	if Channel != "" && Channel != "stable" {
		v += " [" + Channel + "]"
	}
	if GitCommit != "" {
		v += " (" + GitCommit + ")"
	}
	if BuildDate != "" {
		v += " built " + BuildDate
	}
	return v
}
