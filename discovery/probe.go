package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// probeTimeout is the HTTP timeout for probing a peer's /operator/status endpoint.
const probeTimeout = 5 * time.Second

// statusResponse is the subset of the /operator/status JSON body we parse
// during probing. Only the fields needed for peer metadata are included.
type statusResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

// probePeer contacts a peer's /operator/status endpoint and returns the
// peer's status and version. On success the peer is marked online; on any
// error it is marked offline.
func probePeer(ctx context.Context, address string) (status string, version string) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	url := fmt.Sprintf("http://%s/operator/status", address)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return StatusOffline, ""
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return StatusOffline, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return StatusOffline, ""
	}

	var sr statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return StatusOffline, ""
	}
	if !sr.OK {
		return StatusOffline, sr.Version
	}
	return StatusOnline, sr.Version
}
