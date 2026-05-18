package server

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"

	"github.com/gorilla/websocket"
)

func TestWSHubRegisterUnregister(t *testing.T) {
	t.Parallel()

	hub := NewWSHub(nil)
	hub.Start()
	defer hub.Stop()

	// Create a mock connection to register.
	mockConn := &WSConn{
		id:   "conn-1",
		done: make(chan struct{}),
		clientInfo: ConnectClientInfo{
			ID:       "client-1",
			Platform: "test",
		},
		role:        "backend",
		connectedAt: time.Now().UTC(),
		sendCh:      make(chan []byte, wsSendChannelSize),
	}

	if err := hub.Register(mockConn); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount())
	}

	hub.Unregister("conn-1")
	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections after unregister, got %d", hub.ConnectionCount())
	}
}

func TestWSHubBroadcast(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	}, Config{})

	conn1 := wsConnect(t, ts, nil)
	conn2 := wsConnect(t, ts, nil)

	waitForWSHubConnectionCount(t, hub, 2)

	// Broadcast directly through the hub.
	hub.Broadcast("test.event", map[string]string{"key": "value"})

	// Both connections should receive the event.
	readTestEvent := func(conn *websocket.Conn) {
		t.Helper()
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("failed to set read deadline: %v", err)
		}
		var evt EventFrame
		if err := conn.ReadJSON(&evt); err != nil {
			t.Fatalf("failed to read event: %v", err)
		}
		if evt.Event != "test.event" {
			t.Fatalf("expected event test.event, got %q", evt.Event)
		}
	}

	readTestEvent(conn1)
	readTestEvent(conn2)
}

func TestWSHubBroadcastTo(t *testing.T) {
	t.Parallel()

	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	conn1 := wsConnect(t, ts, nil)
	conn2 := wsConnect(t, ts, nil)

	waitForWSHubConnectionCount(t, hub, 2)

	// Find the connection IDs.
	infos := hub.Connections()
	if len(infos) != 2 {
		t.Fatalf("expected 2 connection infos, got %d", len(infos))
	}

	// Send only to the first connection.
	targetID := infos[0].ConnID
	hub.BroadcastTo([]string{targetID}, "targeted.event", map[string]string{"to": "one"})

	// The targeted connection should receive the event.
	readTargetedEvent := func(conn *websocket.Conn, expectEvent bool) {
		t.Helper()
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatalf("failed to set read deadline: %v", err)
		}
		var evt EventFrame
		err := conn.ReadJSON(&evt)
		if expectEvent {
			if err != nil {
				t.Fatalf("expected event but got error: %v", err)
			}
			if evt.Event != "targeted.event" {
				t.Fatalf("expected targeted.event, got %q", evt.Event)
			}
		} else {
			if err == nil {
				t.Fatalf("expected no event, got %+v", evt)
			}
		}
	}

	// We don't know which physical conn maps to which ID, so we verify
	// that exactly one connection received the event via the hub's broadcast.
	// Since BroadcastTo was called with one ID, one conn gets it.
	// We verify the event reached at least one.
	var gotEvent1, gotEvent2 bool

	if err := conn1.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	var evt1 EventFrame
	if err := conn1.ReadJSON(&evt1); err == nil && evt1.Event == "targeted.event" {
		gotEvent1 = true
	}

	if err := conn2.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	var evt2 EventFrame
	if err := conn2.ReadJSON(&evt2); err == nil && evt2.Event == "targeted.event" {
		gotEvent2 = true
	}

	if !gotEvent1 && !gotEvent2 {
		t.Fatal("expected at least one connection to receive the targeted event")
	}
	if gotEvent1 && gotEvent2 {
		t.Fatal("expected only one connection to receive the targeted event")
	}

	_ = readTargetedEvent // suppress unused warning from helper defined above
}

func TestWSHubMaxConnections(t *testing.T) {
	t.Parallel()

	hub := NewWSHub(nil)
	hub.Start()
	defer hub.Stop()

	// Fill up the hub to the max.
	for i := 0; i < wsMaxConnections; i++ {
		conn := &WSConn{
			id:     "fill-" + time.Now().Format(time.RFC3339Nano) + "-" + string(rune('a'+i%26)),
			done:   make(chan struct{}),
			sendCh: make(chan []byte, wsSendChannelSize),
		}
		if err := hub.Register(conn); err != nil {
			t.Fatalf("Register(%d) error = %v", i, err)
		}
	}

	if hub.ConnectionCount() != wsMaxConnections {
		t.Fatalf("expected %d connections, got %d", wsMaxConnections, hub.ConnectionCount())
	}

	// Next registration should fail.
	overflow := &WSConn{
		id:     "overflow",
		done:   make(chan struct{}),
		sendCh: make(chan []byte, wsSendChannelSize),
	}
	if err := hub.Register(overflow); err == nil {
		t.Fatal("expected registration to fail at max capacity")
	}
}

func TestWSHubConnectionCount(t *testing.T) {
	t.Parallel()

	hub := NewWSHub(nil)
	hub.Start()
	defer hub.Stop()

	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0, got %d", hub.ConnectionCount())
	}

	c1 := &WSConn{id: "count-1", done: make(chan struct{}), sendCh: make(chan []byte, wsSendChannelSize)}
	c2 := &WSConn{id: "count-2", done: make(chan struct{}), sendCh: make(chan []byte, wsSendChannelSize)}

	_ = hub.Register(c1)
	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1, got %d", hub.ConnectionCount())
	}

	_ = hub.Register(c2)
	if hub.ConnectionCount() != 2 {
		t.Fatalf("expected 2, got %d", hub.ConnectionCount())
	}

	hub.Unregister("count-1")
	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1 after unregister, got %d", hub.ConnectionCount())
	}

	hub.Unregister("count-2")
	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 after unregister, got %d", hub.ConnectionCount())
	}
}

func TestWSHubConnectionInfo(t *testing.T) {
	t.Parallel()

	hub := NewWSHub(nil)
	hub.Start()
	defer hub.Stop()

	now := time.Now().UTC()
	conn := &WSConn{
		id: "info-1",
		clientInfo: ConnectClientInfo{
			ID:          "client-info-1",
			DisplayName: "Test Client",
			Platform:    "linux",
		},
		role:        "backend",
		connectedAt: now,
		done:        make(chan struct{}),
		sendCh:      make(chan []byte, wsSendChannelSize),
	}
	_ = hub.Register(conn)

	infos := hub.Connections()
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}
	info := infos[0]
	if info.ConnID != "info-1" {
		t.Fatalf("ConnID = %q", info.ConnID)
	}
	if info.ClientID != "client-info-1" {
		t.Fatalf("ClientID = %q", info.ClientID)
	}
	if info.ClientName != "Test Client" {
		t.Fatalf("ClientName = %q", info.ClientName)
	}
	if info.Role != "backend" {
		t.Fatalf("Role = %q", info.Role)
	}
	if info.Platform != "linux" {
		t.Fatalf("Platform = %q", info.Platform)
	}
}

func TestWSHubEventBusBroadcast(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	}, Config{})

	conn := wsConnect(t, ts, nil)

	// Publish an event to the bus.
	if err := bus.Publish(t.Context(), eventbus.Event{
		Type:  eventbus.EventRunStarted,
		RunID: "hub-bus-test",
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Client should receive it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatalf("failed to set read deadline: %v", err)
		}
		var evt EventFrame
		if err := conn.ReadJSON(&evt); err != nil {
			continue
		}
		if evt.Event == string(eventbus.EventRunStarted) {
			return // success
		}
	}
	t.Fatal("timed out waiting for event bus broadcast")
}

func TestWSHubStopClosesAll(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	hub := NewWSHub(bus)
	hub.Start()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	})
	srv := New(svc, Config{WSHub: hub})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const numConns = 3
	conns := make([]*websocket.Conn, numConns)
	for i := 0; i < numConns; i++ {
		conns[i] = wsConnect(t, ts, nil)
	}

	deadline := time.Now().Add(2 * time.Second)
	count := hub.ConnectionCount()
	for time.Now().Before(deadline) {
		if count == numConns {
			break
		}
		time.Sleep(10 * time.Millisecond)
		count = hub.ConnectionCount()
	}
	if count != numConns {
		t.Fatalf("expected %d connections, got %d", numConns, count)
	}

	hub.Stop()

	deadline = time.Now().Add(2 * time.Second)
	count = hub.ConnectionCount()
	for time.Now().Before(deadline) {
		if count == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
		count = hub.ConnectionCount()
	}
	if count != 0 {
		t.Fatalf("expected 0 connections after stop, got %d", count)
	}

	// All connections should be closed.
	var wg sync.WaitGroup
	for i, conn := range conns {
		wg.Add(1)
		go func(idx int, c *websocket.Conn) {
			defer wg.Done()
			if err := c.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				return
			}
			_, _, readErr := c.ReadMessage()
			if readErr == nil {
				t.Errorf("conn %d: expected read error after hub stop", idx)
			}
		}(i, conn)
	}
	wg.Wait()
}
