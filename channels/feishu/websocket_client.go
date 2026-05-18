package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	ws "github.com/gorilla/websocket"
	larkcache "github.com/larksuite/oapi-sdk-go/v3/cache"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

const (
	feishuReconnectInitialBackoff = 2 * time.Second
	feishuReconnectMaxBackoff     = 2 * time.Minute
	feishuDefaultPingInterval     = 2 * time.Minute
)

type websocketClient interface {
	Start(ctx context.Context) error
	Close() error
}

type managedWebsocketClient struct {
	appID        string
	appSecret    string
	domain       string
	eventHandler *dispatcher.EventDispatcher
	httpClient   *http.Client
	dialer       *ws.Dialer
	cache        *larkcache.Cache

	mu           sync.Mutex
	pingInterval time.Duration
	conns        map[*ws.Conn]struct{}
}

func newManagedWebsocketClient(appID, appSecret, domain string, eventHandler *dispatcher.EventDispatcher) websocketClient {
	return &managedWebsocketClient{
		appID:        appID,
		appSecret:    appSecret,
		domain:       strings.TrimRight(strings.TrimSpace(domain), "/"),
		eventHandler: eventHandler,
		httpClient:   http.DefaultClient,
		dialer:       ws.DefaultDialer,
		cache:        larkcache.New(30 * time.Second),
		pingInterval: feishuDefaultPingInterval,
		conns:        make(map[*ws.Conn]struct{}),
	}
}

func (c *managedWebsocketClient) Start(ctx context.Context) error {
	backoff := feishuReconnectInitialBackoff
	for {
		connected, err := c.runSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			var clientErr *larkws.ClientError
			if errors.As(err, &clientErr) {
				return err
			}
			log.Warn("feishu: websocket session ended", "error", err, "domain", c.domain)
		}
		if connected {
			backoff = feishuReconnectInitialBackoff
		}
		delay := jitterReconnectDelay(backoff)
		log.Info("feishu: websocket reconnect scheduled", "delay", delay, "domain", c.domain)
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil
		}
		backoff = nextReconnectBackoff(backoff)
	}
}

func (c *managedWebsocketClient) runSession(ctx context.Context) (bool, error) {
	connURL, config, err := c.fetchConnURL(ctx)
	if err != nil {
		return false, err
	}
	c.applyClientConfig(config)

	conn, resp, err := c.dialer.DialContext(ctx, connURL, nil)
	if err != nil {
		if resp != nil {
			return false, parseManagedWSError(resp)
		}
		return false, err
	}
	c.trackConn(conn)
	defer c.untrackConn(conn)
	defer conn.Close()

	parsedURL, _ := url.Parse(connURL)
	serviceID, _ := strconv.ParseInt(parsedURL.Query().Get(larkws.ServiceID), 10, 32)

	outbound := channels.NewOutboundSerializer()
	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go c.pingLoop(pingCtx, conn, int32(serviceID), outbound)

	for {
		if ctx.Err() != nil {
			return true, nil
		}
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return true, err
		}
		if messageType != ws.BinaryMessage {
			continue
		}
		if err := c.handleMessage(ctx, conn, msg, outbound); err != nil {
			log.Warn("feishu: websocket message handling failed", "error", err)
		}
	}
}

func (c *managedWebsocketClient) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	conns := make([]*ws.Conn, 0, len(c.conns))
	for conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()

	var firstErr error
	for _, conn := range conns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *managedWebsocketClient) fetchConnURL(ctx context.Context) (string, *larkws.ClientConfig, error) {
	body := map[string]string{
		"AppID":     c.appID,
		"AppSecret": c.appSecret,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.domain+larkws.GenEndpointUri, bytes.NewBuffer(payload))
	if err != nil {
		return "", nil, err
	}
	req.Header.Add("locale", "zh")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, larkws.NewServerError(resp.StatusCode, "system busy")
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	var endpointResp larkws.EndpointResp
	if err := json.Unmarshal(respBody, &endpointResp); err != nil {
		return "", nil, err
	}

	switch endpointResp.Code {
	case larkws.OK:
	case larkws.SystemBusy:
		return "", nil, larkws.NewServerError(endpointResp.Code, "system busy")
	case larkws.InternalError:
		return "", nil, larkws.NewServerError(endpointResp.Code, endpointResp.Msg)
	default:
		return "", nil, larkws.NewClientError(endpointResp.Code, endpointResp.Msg)
	}

	if endpointResp.Data == nil || endpointResp.Data.Url == "" {
		return "", nil, larkws.NewServerError(http.StatusInternalServerError, "endpoint is null")
	}
	return endpointResp.Data.Url, endpointResp.Data.ClientConfig, nil
}

func (c *managedWebsocketClient) applyClientConfig(conf *larkws.ClientConfig) {
	if conf == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if conf.PingInterval > 0 {
		c.pingInterval = time.Duration(conf.PingInterval) * time.Second
	}
}

func (c *managedWebsocketClient) currentPingInterval() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pingInterval <= 0 {
		return feishuDefaultPingInterval
	}
	return c.pingInterval
}

func (c *managedWebsocketClient) trackConn(conn *ws.Conn) {
	if c == nil || conn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conns == nil {
		c.conns = make(map[*ws.Conn]struct{})
	}
	c.conns[conn] = struct{}{}
}

func (c *managedWebsocketClient) untrackConn(conn *ws.Conn) {
	if c == nil || conn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.conns, conn)
}

func (c *managedWebsocketClient) pingLoop(ctx context.Context, conn *ws.Conn, serviceID int32, outbound *channels.OutboundSerializer) {
	interval := c.currentPingInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			frame := larkws.NewPingFrame(serviceID)
			payload, err := frame.Marshal()
			if err != nil {
				continue
			}
			err = outbound.Do(func() error {
				return conn.WriteMessage(ws.BinaryMessage, payload)
			})
			if err != nil {
				return
			}
		}
	}
}

func (c *managedWebsocketClient) handleMessage(ctx context.Context, conn *ws.Conn, msg []byte, outbound *channels.OutboundSerializer) error {
	var frame larkws.Frame
	if err := frame.Unmarshal(msg); err != nil {
		return err
	}

	switch larkws.FrameType(frame.Method) {
	case larkws.FrameTypeControl:
		return c.handleControlFrame(frame)
	case larkws.FrameTypeData:
		return c.handleDataFrame(ctx, conn, frame, outbound)
	default:
		return nil
	}
}

func (c *managedWebsocketClient) handleControlFrame(frame larkws.Frame) error {
	headers := larkws.Headers(frame.Headers)
	if larkws.MessageType(headers.GetString(larkws.HeaderType)) != larkws.MessageTypePong {
		return nil
	}
	if len(frame.Payload) == 0 {
		return nil
	}
	var conf larkws.ClientConfig
	if err := json.Unmarshal(frame.Payload, &conf); err != nil {
		return err
	}
	c.applyClientConfig(&conf)
	return nil
}

func (c *managedWebsocketClient) handleDataFrame(ctx context.Context, conn *ws.Conn, frame larkws.Frame, outbound *channels.OutboundSerializer) error {
	headers := larkws.Headers(frame.Headers)
	sum := headers.GetInt(larkws.HeaderSum)
	seq := headers.GetInt(larkws.HeaderSeq)
	messageID := headers.GetString(larkws.HeaderMessageID)
	messageType := headers.GetString(larkws.HeaderType)

	payload := frame.Payload
	if sum > 1 {
		payload = c.combine(messageID, sum, seq, payload)
		if payload == nil {
			return nil
		}
	}

	start := time.Now()
	var (
		respPayload any
		handlerErr  error
	)
	switch larkws.MessageType(messageType) {
	case larkws.MessageTypeEvent:
		respPayload, handlerErr = c.eventHandler.Do(ctx, payload)
	case larkws.MessageTypeCard:
		return nil
	default:
		return nil
	}
	headers.Add(larkws.HeaderBizRt, strconv.FormatInt(time.Since(start).Milliseconds(), 10))

	response := larkws.NewResponseByCode(http.StatusOK)
	if handlerErr != nil {
		response = larkws.NewResponseByCode(http.StatusInternalServerError)
	} else if respPayload != nil {
		data, err := json.Marshal(respPayload)
		if err != nil {
			response = larkws.NewResponseByCode(http.StatusInternalServerError)
		} else {
			response.Data = data
		}
	}

	responseBody, _ := json.Marshal(response)
	frame.Payload = responseBody
	frame.Headers = headers
	wire, err := frame.Marshal()
	if err != nil {
		return err
	}

	return outbound.Do(func() error {
		return conn.WriteMessage(ws.BinaryMessage, wire)
	})
}

func (c *managedWebsocketClient) combine(messageID string, sum, seq int, payload []byte) []byte {
	value := c.cache.Get(messageID)
	if value == nil {
		buf := make([][]byte, sum)
		buf[seq] = payload
		c.cache.Set(messageID, buf, 5*time.Second)
		return nil
	}

	buf := value.([][]byte)
	buf[seq] = payload
	totalLen := 0
	for _, item := range buf {
		if len(item) == 0 {
			c.cache.Set(messageID, buf, 5*time.Second)
			return nil
		}
		totalLen += len(item)
	}

	combined := make([]byte, 0, totalLen)
	for _, item := range buf {
		combined = append(combined, item...)
	}
	return combined
}

func parseManagedWSError(resp *http.Response) error {
	code, _ := strconv.Atoi(resp.Header.Get(larkws.HeaderHandshakeStatus))
	msg := resp.Header.Get(larkws.HeaderHandshakeMsg)
	switch code {
	case larkws.AuthFailed:
		authCode, _ := strconv.Atoi(resp.Header.Get(larkws.HeaderHandshakeAuthErrCode))
		if authCode == larkws.ExceedConnLimit {
			return larkws.NewClientError(code, msg)
		}
		return larkws.NewServerError(code, msg)
	case larkws.Forbidden:
		return larkws.NewClientError(code, msg)
	default:
		return larkws.NewServerError(code, msg)
	}
}

func nextReconnectBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return feishuReconnectInitialBackoff
	}
	current *= 2
	if current > feishuReconnectMaxBackoff {
		return feishuReconnectMaxBackoff
	}
	return current
}

func jitterReconnectDelay(base time.Duration) time.Duration {
	if base <= 0 {
		base = feishuReconnectInitialBackoff
	}
	jitter := time.Duration(rand.Int63n(int64(base / 5)))
	return base + jitter
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
