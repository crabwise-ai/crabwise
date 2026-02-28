package openclaw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

type EventCallback func(*EventFrame)

type GatewayClient struct {
	cfg Config

	mu        sync.RWMutex
	conn      *websocket.Conn
	connected bool
	closed    bool

	requestID uint64

	pendingMu sync.Mutex
	pending   map[string]chan responseResult

	callbacksMu sync.RWMutex
	callbacks   []EventCallback
}

type responseResult struct {
	payload json.RawMessage
	err     error
}

func NewGatewayClient(cfg Config) *GatewayClient {
	return &GatewayClient{
		cfg:     cfg,
		pending: make(map[string]chan responseResult),
	}
}

func (c *GatewayClient) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *GatewayClient) Connect(ctx context.Context) (*HelloOK, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("gateway client closed")
	}
	if c.connected && c.conn != nil {
		c.mu.Unlock()
		return &HelloOK{Type: "hello-ok", Protocol: 3}, nil
	}
	c.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.cfg.GatewayURL, nil)
	if err != nil {
		return nil, err
	}

	helloCh := make(chan *HelloOK, 1)
	errCh := make(chan error, 1)

	c.mu.Lock()
	c.conn = conn
	c.connected = false
	c.mu.Unlock()

	go c.readLoop(conn, helloCh, errCh)

	select {
	case hello := <-helloCh:
		return hello, nil
	case err := <-errCh:
		_ = conn.Close()
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
			c.connected = false
		}
		c.mu.Unlock()
		return nil, err
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	}
}

func (c *GatewayClient) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	raw, err := c.request(ctx, "sessions.list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload.Sessions, nil
}

func (c *GatewayClient) OnEvent(callback EventCallback) {
	c.callbacksMu.Lock()
	defer c.callbacksMu.Unlock()
	c.callbacks = append(c.callbacks, callback)
}

func (c *GatewayClient) Close() {
	c.mu.Lock()
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.connected = false
	c.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- responseResult{err: errors.New("gateway client closed")}
		close(ch)
		delete(c.pending, id)
	}
}

func (c *GatewayClient) request(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.RLock()
	conn := c.conn
	connected := c.connected
	c.mu.RUnlock()
	if conn == nil || !connected {
		return nil, errors.New("gateway client not connected")
	}

	id := fmt.Sprintf("req-%d", atomic.AddUint64(&c.requestID, 1))
	respCh := make(chan responseResult, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	req := RequestFrame{
		Type:   "req",
		ID:     id,
		Method: method,
		Params: params,
	}
	if err := conn.WriteJSON(req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case res := <-respCh:
		return res.payload, res.err
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *GatewayClient) readLoop(conn *websocket.Conn, helloCh chan<- *HelloOK, errCh chan<- error) {
	helloPending := true

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if helloPending {
				errCh <- err
			}
			c.failPending(err)
			c.mu.Lock()
			if c.conn == conn {
				c.conn = nil
				c.connected = false
			}
			c.mu.Unlock()
			return
		}

		var probe struct {
			Type  string `json:"type"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			if helloPending {
				errCh <- err
			}
			continue
		}

		switch probe.Type {
		case "hello-ok":
			var hello HelloOK
			if err := json.Unmarshal(data, &hello); err != nil {
				if helloPending {
					errCh <- err
				}
				continue
			}
			c.mu.Lock()
			c.connected = true
			c.mu.Unlock()
			if helloPending {
				helloCh <- &hello
				helloPending = false
			}
		case "event":
			if probe.Event == "connect.challenge" {
				if c.cfg.APIToken == "" {
					if helloPending {
						errCh <- errors.New("gateway auth challenge received but api token is not configured")
					}
					_ = conn.Close()
					return
				}
				if err := conn.WriteJSON(RequestFrame{
					Type:   "req",
					ID:     fmt.Sprintf("connect-%d", atomic.AddUint64(&c.requestID, 1)),
					Method: "connect",
					Params: connectParams(c.cfg.APIToken),
				}); err != nil && helloPending {
					errCh <- err
					helloPending = false
				}
				continue
			}

			frame, err := DecodeGatewayFrame(data)
			if err != nil {
				if helloPending {
					errCh <- err
					helloPending = false
				}
				continue
			}
			event, ok := frame.(*EventFrame)
			if ok {
				c.dispatchEvent(event)
			}
		case "res":
			var res struct {
				Type    string          `json:"type"`
				ID      string          `json:"id"`
				OK      bool            `json:"ok"`
				Payload json.RawMessage `json:"payload,omitempty"`
				Error   *ResponseError  `json:"error,omitempty"`
			}
			if err := json.Unmarshal(data, &res); err != nil {
				if helloPending {
					errCh <- err
					helloPending = false
				}
				continue
			}
			if helloPending && res.OK {
				var hello HelloOK
				if err := json.Unmarshal(res.Payload, &hello); err == nil && hello.Type == "hello-ok" {
					c.mu.Lock()
					c.connected = true
					c.mu.Unlock()
					helloCh <- &hello
					helloPending = false
					continue
				}
			}
			c.resolvePending(res.ID, responseResult{payload: res.Payload, err: responseError(res.OK, res.Error)})
		}
	}
}

func (c *GatewayClient) dispatchEvent(event *EventFrame) {
	c.callbacksMu.RLock()
	callbacks := append([]EventCallback(nil), c.callbacks...)
	c.callbacksMu.RUnlock()

	for _, callback := range callbacks {
		callback(event)
	}
}

func (c *GatewayClient) resolvePending(id string, result responseResult) {
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if ok {
		ch <- result
		close(ch)
	}
}

func (c *GatewayClient) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- responseResult{err: err}
		close(ch)
		delete(c.pending, id)
	}
}

func responseError(ok bool, err *ResponseError) error {
	if ok {
		return nil
	}
	if err == nil {
		return errors.New("gateway request failed")
	}
	return fmt.Errorf("%s", err.Message)
}

func connectParams(token string) map[string]interface{} {
	return map[string]interface{}{
		"minProtocol": 3,
		"maxProtocol": 3,
		"client": map[string]interface{}{
			"id":       "crabwise",
			"version":  "0.1.0",
			"platform": runtime.GOOS,
			"mode":     "cli",
		},
		"role":        "operator",
		"scopes":      []string{"operator.read"},
		"caps":        []string{},
		"commands":    []string{},
		"permissions": map[string]interface{}{},
		"locale":      "en-US",
		"userAgent":   "crabwise/0.1.0",
		"auth": map[string]interface{}{
			"token": token,
		},
	}
}
