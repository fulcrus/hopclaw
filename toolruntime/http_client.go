package toolruntime

import (
	"fmt"
	"net/http"

	"github.com/fulcrus/hopclaw/config"
)

const defaultRedirectLimit = 10

func newSSRFProtectedHTTPClient(constraints config.NetConstraints) *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= defaultRedirectLimit {
				return fmt.Errorf("too many redirects")
			}
			if err := checkURLSSRF(req.URL.String(), constraints); err != nil {
				return err
			}
			return nil
		},
	}
}
