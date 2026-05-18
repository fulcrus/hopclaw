package gateway

import (
	"net/http"

	"github.com/fulcrus/hopclaw/discovery"
)

type discoveryPeersResponse struct {
	Items []discovery.Peer `json:"items"`
	Count int              `json:"count"`
}

type discoveryStatusResponse struct {
	MDNS        bool `json:"mdns"`
	StaticPeers bool `json:"static_peers"`
}

type operatorDiscoverySurface struct {
	discovery discovery.Resolver
}

func newOperatorDiscoverySurface(resolver discovery.Resolver) *operatorDiscoverySurface {
	return &operatorDiscoverySurface{discovery: resolver}
}

func (s *operatorDiscoverySurface) RegisterRoutes(mux *http.ServeMux, mountAuthed func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request))) {
	if mux == nil || mountAuthed == nil {
		return
	}
	mountAuthed(mux, "GET /operator/discovery/peers", s.handleDiscoveryPeers)
	mountAuthed(mux, "GET /operator/discovery/status", s.handleDiscoveryStatus)
}

func (s *operatorDiscoverySurface) handleDiscoveryPeers(w http.ResponseWriter, r *http.Request) {
	if s.discovery == nil {
		gwError(w, http.StatusServiceUnavailable, "discovery not configured")
		return
	}
	peers, err := s.discovery.Discover(r.Context())
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if peers == nil {
		peers = make([]discovery.Peer, 0)
	}
	gwJSON(w, http.StatusOK, discoveryPeersResponse{Items: peers, Count: len(peers)})
}

func (s *operatorDiscoverySurface) handleDiscoveryStatus(w http.ResponseWriter, _ *http.Request) {
	gwJSON(w, http.StatusOK, discoveryStatusResponse{
		MDNS:        s.discovery != nil,
		StaticPeers: false,
	})
}
