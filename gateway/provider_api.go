package gateway

import "github.com/fulcrus/hopclaw/model"

func canonicalProviderAPI(api string) string {
	return string(model.NormalizeProviderAPI(model.ProviderAPI(api)))
}
