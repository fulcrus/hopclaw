package discovery

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	mdnsQueryTimeout = 5 * time.Second
	mdnsDefaultPort  = 16280
)

type mdnsResolver struct {
	mu           sync.Mutex // guards server and stopped
	service      string
	instanceName string
	port         int
	iface        string
	server       *mdns.Server
	stopped      bool
}

func newMDNSResolver(cfg Config) (*mdnsResolver, error) {
	svc := cfg.Service
	if svc == "" {
		svc = defaultServiceName
	}
	port := cfg.Port
	if port == 0 {
		port = mdnsDefaultPort
	}
	return &mdnsResolver{
		service:      svc,
		instanceName: cfg.InstanceName,
		port:         port,
		iface:        cfg.Interface,
	}, nil
}

func (m *mdnsResolver) Announce(ctx context.Context, self Peer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return ErrResolverStopped
	}

	// Build TXT records with metadata.
	info := []string{
		fmt.Sprintf("id=%s", self.ID),
		fmt.Sprintf("version=%s", self.Version),
	}

	service, err := mdns.NewMDNSService(
		m.instanceName,
		m.service,
		"", // domain
		"", // host
		m.port,
		nil,  // IPs (auto-detect)
		info, // TXT records
	)
	if err != nil {
		return fmt.Errorf("mdns: create service: %w", err)
	}

	serverCfg := &mdns.Config{
		Zone: service,
	}

	// If a specific interface is configured, bind to it.
	if m.iface != "" {
		iface, ifErr := net.InterfaceByName(m.iface)
		if ifErr != nil {
			return fmt.Errorf("mdns: interface %q: %w", m.iface, ifErr)
		}
		serverCfg.Iface = iface
	}

	server, err := mdns.NewServer(serverCfg)
	if err != nil {
		return fmt.Errorf("mdns: start server: %w", err)
	}

	// Shut down previous server if re-announcing.
	if m.server != nil {
		_ = m.server.Shutdown()
	}
	m.server = server
	return nil
}

func (m *mdnsResolver) Discover(ctx context.Context) ([]Peer, error) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil, ErrResolverStopped
	}
	m.mu.Unlock()

	// Determine query timeout from context deadline or use default.
	queryTimeout := mdnsQueryTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < queryTimeout {
			queryTimeout = remaining
		}
	}

	entriesCh := make(chan *mdns.ServiceEntry, 16)
	var entries []*mdns.ServiceEntry

	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for entry := range entriesCh {
			entries = append(entries, entry)
		}
	}()

	params := &mdns.QueryParam{
		Service:             m.service,
		Domain:              "",
		Timeout:             queryTimeout,
		Entries:             entriesCh,
		WantUnicastResponse: false,
	}

	// If a specific interface is configured, bind to it.
	if m.iface != "" {
		iface, netErr := net.InterfaceByName(m.iface)
		if netErr != nil {
			close(entriesCh)
			<-collectDone
			return nil, fmt.Errorf("mdns: interface %q: %w", m.iface, netErr)
		}
		params.Interface = iface
	}

	// Run the blocking query in a goroutine so we can respect context cancellation.
	queryDone := make(chan error, 1)
	go func() {
		queryDone <- mdns.Query(params)
	}()

	select {
	case err := <-queryDone:
		<-collectDone
		if err != nil {
			return nil, fmt.Errorf("mdns: query failed: %w", err)
		}
	case <-ctx.Done():
		// Context cancelled; the query goroutine will finish on its own timeout.
		// Drain the collector to prevent leaks.
		go func() {
			<-queryDone
			<-collectDone
		}()
		return nil, ctx.Err()
	}

	peers := make([]Peer, 0, len(entries))
	now := time.Now().UTC()

	for _, entry := range entries {
		addr := entry.AddrV4
		if addr == nil {
			addr = entry.AddrV6
		}
		if addr == nil {
			continue
		}
		address := fmt.Sprintf("%s:%d", addr.String(), entry.Port)

		// Parse TXT records for metadata.
		var peerID, version string
		for _, txt := range entry.InfoFields {
			if strings.HasPrefix(txt, "id=") {
				peerID = strings.TrimPrefix(txt, "id=")
			} else if strings.HasPrefix(txt, "version=") {
				version = strings.TrimPrefix(txt, "version=")
			}
		}

		// Probe the peer to check liveness.
		status, probeVersion := probePeer(ctx, address)
		if probeVersion != "" {
			version = probeVersion
		}

		peers = append(peers, Peer{
			ID:      peerID,
			Name:    entry.Name,
			Address: address,
			Version: version,
			Status:  status,
			SeenAt:  now,
		})
	}

	return peers, nil
}

func (m *mdnsResolver) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return nil
	}
	m.stopped = true

	if m.server != nil {
		return m.server.Shutdown()
	}
	return nil
}
