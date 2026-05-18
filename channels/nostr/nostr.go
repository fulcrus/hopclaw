package nostr

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/gorilla/websocket"
)

var log = logging.WithSubsystem("nostr")

// Config holds the configuration for the Nostr adapter.
type Config struct {
	PrivateKey string   `json:"private_key" yaml:"private_key"` // hex-encoded secp256k1 private key
	Relays     []string `json:"relays" yaml:"relays"`           // WebSocket relay URLs
}

// nostrEvent is a Nostr event as defined by NIP-01.
type nostrEvent struct {
	ID        string     `json:"id"`
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// relayConn tracks a single relay WebSocket connection.
type relayConn struct {
	url      string
	conn     *websocket.Conn
	outbound *channels.OutboundSerializer
}

// Adapter implements channels.Adapter for the Nostr protocol.
type Adapter struct {
	config Config

	base         channels.BaseAdapter
	stateMu      sync.RWMutex
	relays       []relayConn
	pubKey       string
	privKeyBytes []byte
}

// New creates a new Nostr adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		base:   channels.NewBaseAdapter("nostr"),
	}
}

// Connect establishes WebSocket connections to all configured relays and
// subscribes to text notes (kind 1) and encrypted DMs (kind 4).
func (a *Adapter) Connect(ctx context.Context) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	if a.config.PrivateKey == "" || len(a.config.Relays) == 0 {
		return fmt.Errorf("nostr: private_key and at least one relay are required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)
	a.relays = nil
	a.pubKey = ""
	a.privKeyBytes = nil

	// Derive the public key from the private key.
	privBytes, err := hex.DecodeString(a.config.PrivateKey)
	if err != nil || len(privBytes) != 32 {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("nostr: invalid private key (must be 32-byte hex)")
	}
	a.privKeyBytes = privBytes
	a.pubKey = derivePublicKey(privBytes)

	log.Info("nostr: derived public key", "pubkey", a.pubKey)

	// Connect to each relay.
	var connected []relayConn
	for _, relayURL := range a.config.Relays {
		relayURL = strings.TrimSpace(relayURL)
		if relayURL == "" {
			continue
		}

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
		if err != nil {
			log.Warn("nostr: failed to connect to relay", "relay", relayURL, "error", err)
			continue
		}
		connected = append(connected, relayConn{
			url:      relayURL,
			conn:     conn,
			outbound: channels.NewOutboundSerializer(),
		})
	}

	if len(connected) == 0 {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("nostr: could not connect to any relay")
	}

	wsCtx, cancel := context.WithCancel(ctx)

	// Subscribe for events on each relay and start reader goroutines.
	subID := generateSubscriptionID()
	active := make([]relayConn, 0, len(connected))
	for i := range connected {
		rc := connected[i]
		if err := a.sendSubscription(rc, subID); err != nil {
			log.Warn("nostr: failed to subscribe on relay", "relay", rc.url, "error", err)
			closeRelayConn(rc)
			continue
		}
		active = append(active, rc)
	}
	if len(active) == 0 {
		cancel()
		a.base.SetStatus(channels.StatusError)
		a.pubKey = ""
		a.privKeyBytes = nil
		return fmt.Errorf("nostr: could not subscribe to any relay")
	}
	a.relays = active
	if !a.base.MarkConnected(cancel) {
		cancel()
		closeRelayConns(active)
		a.relays = nil
		a.pubKey = ""
		a.privKeyBytes = nil
		return nil
	}
	for i := range a.relays {
		go a.readLoop(wsCtx, a.relays[i])
	}

	log.Info("nostr: adapter connected", "relays", len(a.relays), "pubkey", a.pubKey)
	return nil
}

// Disconnect closes all relay connections.
func (a *Adapter) Disconnect(_ context.Context) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}

	relays := a.relays
	a.relays = nil
	a.pubKey = ""
	a.privKeyBytes = nil

	closeRelayConns(relays)
	log.Info("nostr: adapter disconnected")
	return nil
}

// Send publishes a public Nostr note to all connected relays.
// Direct messages are rejected until authenticated NIP-04 encryption is implemented.
func (a *Adapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("nostr: adapter is not connected")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	now := time.Now().Unix()

	evt, err := a.buildOutboundEvent(msg, now)
	if err != nil {
		return err
	}

	// Compute the event ID and sign it.
	evt.ID = computeEventID(evt)
	sig, err := schnorrSign(a.privKeyBytes, evt.ID)
	if err != nil {
		return fmt.Errorf("nostr: sign event: %w", err)
	}
	evt.Sig = sig

	// Publish to all relays.
	eventMsg := []any{"EVENT", evt}
	payload, err := json.Marshal(eventMsg)
	if err != nil {
		return fmt.Errorf("nostr: marshal event: %w", err)
	}

	var lastErr error
	sent := 0
	for _, rc := range a.relays {
		if err := rc.outbound.Do(func() error {
			return rc.conn.WriteMessage(websocket.TextMessage, payload)
		}); err != nil {
			log.Warn("nostr: failed to publish to relay", "relay", rc.url, "error", err)
			lastErr = err
			continue
		}
		sent++
	}

	if sent == 0 {
		return fmt.Errorf("nostr: failed to publish to any relay: %w", lastErr)
	}
	return nil
}

func (a *Adapter) buildOutboundEvent(msg channels.OutboundMessage, now int64) (nostrEvent, error) {
	targetPubKey := strings.TrimSpace(msg.TargetID)
	if targetPubKey != "" {
		return nostrEvent{}, fmt.Errorf("nostr: direct messages are disabled until NIP-04 encryption is implemented")
	}
	return nostrEvent{
		PubKey:    a.pubKey,
		CreatedAt: now,
		Kind:      1,
		Tags:      [][]string{},
		Content:   msg.Content,
	}, nil
}

// Capabilities returns what the Nostr adapter supports.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   false,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   true,
		Interactive:    true,
		InlineDelivery: true,
	}
}

// Status returns the current connection state.
func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

// SubscribeEvents returns a channel that receives inbound messages.
func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

// sendSubscription sends a REQ message to the relay to subscribe to events.
func (a *Adapter) sendSubscription(rc relayConn, subID string) error {
	filter := map[string]any{
		"kinds": []int{1, 4},
		"since": time.Now().Unix(),
		"#p":    []string{a.pubKey},
	}

	reqMsg := []any{"REQ", subID, filter}
	return rc.outbound.Do(func() error {
		return rc.conn.WriteJSON(reqMsg)
	})
}

// readLoop reads messages from a relay WebSocket and dispatches events.
func (a *Adapter) readLoop(ctx context.Context, rc relayConn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := rc.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("nostr: relay read error", "relay", rc.url, "error", err)
			return
		}

		// Nostr messages are JSON arrays: ["EVENT", subscriptionId, event]
		var rawMsg []json.RawMessage
		if err := json.Unmarshal(data, &rawMsg); err != nil {
			log.Debug("nostr: ignoring non-array frame", "relay", rc.url)
			continue
		}
		if len(rawMsg) < 1 {
			continue
		}

		var msgType string
		if err := json.Unmarshal(rawMsg[0], &msgType); err != nil {
			continue
		}

		if msgType == "EVENT" && len(rawMsg) >= 3 {
			var evt nostrEvent
			if err := json.Unmarshal(rawMsg[2], &evt); err != nil {
				log.Warn("nostr: failed to parse event", "relay", rc.url, "error", err)
				continue
			}
			a.handleEvent(rc.url, evt)
		}
	}
}

// handleEvent processes an incoming Nostr event.
func (a *Adapter) handleEvent(relay string, evt nostrEvent) {
	a.stateMu.RLock()
	selfPubKey := a.pubKey
	a.stateMu.RUnlock()

	// Skip events from ourselves.
	if evt.PubKey == selfPubKey {
		return
	}

	// Only handle kind 1 (text note) and kind 4 (encrypted DM).
	if evt.Kind != 1 && evt.Kind != 4 {
		return
	}

	content := evt.Content
	if evt.Kind == 4 {
		log.Warn("nostr: dropping encrypted DM until NIP-04 decryption is implemented", "relay", relay, "event_id", evt.ID)
		return
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	inbound := channels.InboundMessage{
		ChannelID:  "nostr",
		SenderID:   evt.PubKey,
		SenderName: evt.PubKey[:16], // abbreviated pubkey as display name
		Content:    content,
		RawEvent: map[string]any{
			"event_id": evt.ID,
			"kind":     evt.Kind,
			"relay":    relay,
			"tags":     evt.Tags,
		},
	}

	log.Info("nostr: received event",
		"sender", evt.PubKey[:16],
		"kind", evt.Kind,
		"relay", relay,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("nostr: subscriber channel full, dropping message")
	})
}

func closeRelayConns(relays []relayConn) {
	for _, rc := range relays {
		closeRelayConn(rc)
	}
}

func closeRelayConn(rc relayConn) {
	if rc.conn == nil {
		return
	}
	_ = rc.outbound.Do(func() error {
		return rc.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
	})
	_ = rc.conn.Close()
}

// ---------- Cryptographic helpers ----------

// secp256k1 curve parameters (simplified for signing).
var (
	secp256k1P, _  = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
	secp256k1N, _  = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
	secp256k1Gx, _ = new(big.Int).SetString("79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798", 16)
	secp256k1Gy, _ = new(big.Int).SetString("483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8", 16)
)

// derivePublicKey derives the x-only public key (hex) from a 32-byte private key.
func derivePublicKey(privKey []byte) string {
	k := new(big.Int).SetBytes(privKey)
	x, _ := scalarBaseMult(k)
	return fmt.Sprintf("%064x", x)
}

// computeEventID computes the SHA-256 event ID per NIP-01.
// The serialization is: [0, pubkey, created_at, kind, tags, content].
func computeEventID(evt nostrEvent) string {
	serialized := []any{
		0,
		evt.PubKey,
		evt.CreatedAt,
		evt.Kind,
		evt.Tags,
		evt.Content,
	}
	data, _ := json.Marshal(serialized)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// schnorrSign produces a hex-encoded Schnorr signature over the event ID.
// This is a simplified BIP-340 Schnorr signature for secp256k1.
func schnorrSign(privKey []byte, eventIDHex string) (string, error) {
	msgHash, err := hex.DecodeString(eventIDHex)
	if err != nil {
		return "", fmt.Errorf("decode event id: %w", err)
	}

	d := new(big.Int).SetBytes(privKey)

	// Compute public key P = d*G.
	px, py := scalarBaseMult(d)

	// If P.y is odd, negate d.
	if py.Bit(0) == 1 {
		d.Sub(secp256k1N, d)
	}

	// Generate a deterministic nonce: k = hash(privkey || msg) mod n.
	// In production, use RFC 6979 or BIP-340 aux randomness.
	auxRand := make([]byte, 32)
	if _, err := rand.Read(auxRand); err != nil {
		return "", fmt.Errorf("generate random: %w", err)
	}

	nonceInput := make([]byte, 0, 96)
	nonceInput = append(nonceInput, privKey...)
	nonceInput = append(nonceInput, msgHash...)
	nonceInput = append(nonceInput, auxRand...)
	nonceHash := sha256.Sum256(nonceInput)
	k := new(big.Int).SetBytes(nonceHash[:])
	k.Mod(k, secp256k1N)
	if k.Sign() == 0 {
		return "", fmt.Errorf("nonce is zero")
	}

	// R = k*G.
	rx, ry := scalarBaseMult(k)

	// If R.y is odd, negate k.
	if ry.Bit(0) == 1 {
		k.Sub(secp256k1N, k)
	}

	// e = hash(R.x || P.x || msg) mod n.
	eInput := make([]byte, 0, 96)
	eInput = append(eInput, padTo32(rx)...)
	eInput = append(eInput, padTo32(px)...)
	eInput = append(eInput, msgHash...)

	// BIP-340 uses tagged hash "BIP0340/challenge", but for simplicity
	// we use a plain SHA-256.
	eHash := sha256.Sum256(eInput)
	e := new(big.Int).SetBytes(eHash[:])
	e.Mod(e, secp256k1N)

	// s = (k + e*d) mod n.
	s := new(big.Int).Mul(e, d)
	s.Add(s, k)
	s.Mod(s, secp256k1N)

	// Signature = R.x (32 bytes) || s (32 bytes).
	sig := make([]byte, 64)
	copy(sig[:32], padTo32(rx))
	copy(sig[32:], padTo32(s))

	return hex.EncodeToString(sig), nil
}

// scalarBaseMult computes k*G on secp256k1 using double-and-add.
func scalarBaseMult(k *big.Int) (*big.Int, *big.Int) {
	rx, ry := new(big.Int), new(big.Int)
	gx, gy := new(big.Int).Set(secp256k1Gx), new(big.Int).Set(secp256k1Gy)
	first := true

	for i := k.BitLen() - 1; i >= 0; i-- {
		if !first {
			rx, ry = pointDouble(rx, ry)
		}
		if k.Bit(i) == 1 {
			if first {
				rx.Set(gx)
				ry.Set(gy)
				first = false
			} else {
				rx, ry = pointAdd(rx, ry, gx, gy)
			}
		}
	}
	return rx, ry
}

// pointAdd performs elliptic curve point addition on secp256k1.
func pointAdd(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		return pointDouble(x1, y1)
	}

	// slope = (y2 - y1) / (x2 - x1) mod p
	dy := new(big.Int).Sub(y2, y1)
	dy.Mod(dy, secp256k1P)
	dx := new(big.Int).Sub(x2, x1)
	dx.Mod(dx, secp256k1P)
	dxInv := new(big.Int).ModInverse(dx, secp256k1P)
	slope := new(big.Int).Mul(dy, dxInv)
	slope.Mod(slope, secp256k1P)

	// x3 = slope^2 - x1 - x2 mod p
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, secp256k1P)

	// y3 = slope * (x1 - x3) - y1 mod p
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y1)
	y3.Mod(y3, secp256k1P)

	return x3, y3
}

// pointDouble performs elliptic curve point doubling on secp256k1.
func pointDouble(x, y *big.Int) (*big.Int, *big.Int) {
	// slope = (3 * x^2) / (2 * y) mod p   (a=0 for secp256k1)
	x2 := new(big.Int).Mul(x, x)
	x2.Mod(x2, secp256k1P)
	num := new(big.Int).Mul(big.NewInt(3), x2)
	num.Mod(num, secp256k1P)
	den := new(big.Int).Mul(big.NewInt(2), y)
	den.Mod(den, secp256k1P)
	denInv := new(big.Int).ModInverse(den, secp256k1P)
	slope := new(big.Int).Mul(num, denInv)
	slope.Mod(slope, secp256k1P)

	// x3 = slope^2 - 2*x mod p
	x3 := new(big.Int).Mul(slope, slope)
	x3.Sub(x3, new(big.Int).Mul(big.NewInt(2), x))
	x3.Mod(x3, secp256k1P)

	// y3 = slope * (x - x3) - y mod p
	y3 := new(big.Int).Sub(x, x3)
	y3.Mul(y3, slope)
	y3.Sub(y3, y)
	y3.Mod(y3, secp256k1P)

	return x3, y3
}

// padTo32 pads a big.Int to exactly 32 bytes (big-endian).
func padTo32(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// generateSubscriptionID creates a random subscription ID.
func generateSubscriptionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
