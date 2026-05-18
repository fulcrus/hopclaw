package gateway

import (
	"fmt"
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/gorilla/websocket"
)

const (
	wsReadBufSize  = 1024
	wsWriteBufSize = 1024
)

func (g *Gateway) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if g.wsHandler == nil {
		gwError(w, http.StatusServiceUnavailable, "websocket not available")
		return
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  wsReadBufSize,
		WriteBufferSize: wsWriteBufSize,
		CheckOrigin: func(r *http.Request) bool {
			return websocketOriginAllowed(r, g.config.CORS.AllowedOrigins)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}
	platform := r.URL.Query().Get("platform")
	deviceCtx, _ := deviceauth.DeviceFromContext(r.Context())
	g.wsHandler.ServeClient(conn, wsClientOptions{
		Context:    r.Context(),
		ID:         clientID,
		Platform:   platform,
		RemoteAddr: r.RemoteAddr,
		Device:     deviceCtx,
		AuthScope:  authScopeFromIdentity(AuthIdentityFromContext(r.Context())),
	})
}
