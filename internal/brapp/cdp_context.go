package brapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// cdpContext owns only the newly-created private browser context. It never
// navigates or closes tabs from the user's default browser context.
type cdpContext struct {
	mu       sync.Mutex
	endpoint string
	conn     *websocket.Conn
	id       string
	sequence int64
}

func createCDPContext(ctx context.Context, endpoint, proxyURL string) (*cdpContext, error) {
	c := &cdpContext{endpoint: endpoint}
	var result struct {
		ID string `json:"browserContextId"`
	}
	if err := c.call(ctx, "Target.createBrowserContext", map[string]any{"disposeOnDetach": true, "proxyServer": proxyURL, "proxyBypassList": "<-loopback>"}, &result); err != nil {
		c.closeConnection()
		return nil, err
	}
	if result.ID == "" || len(result.ID) > 256 {
		c.closeConnection()
		return nil, errors.New("CDP did not create an owned browser context")
	}
	c.id = result.ID
	return c, nil
}

func (c *cdpContext) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.conn == nil {
		if _, _, err := wsHostPort(c.endpoint); err != nil {
			return err
		}
		dialer := websocket.Dialer{HandshakeTimeout: BrConnectTimeout, Proxy: nil}
		conn, response, err := dialer.DialContext(ctx, c.endpoint, nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			return err
		}
		c.conn = conn
		conn.SetReadLimit(1 << 20)
	}
	deadline := time.Now().Add(BrConnectTimeout)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}
	_ = c.conn.SetReadDeadline(deadline)
	_ = c.conn.SetWriteDeadline(deadline)
	c.sequence++
	if err := c.conn.WriteJSON(map[string]any{"id": c.sequence, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var message struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := c.conn.ReadJSON(&message); err != nil {
			return err
		}
		if message.ID != c.sequence {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("CDP %s: %s", method, clampDetail(message.Error.Message))
		}
		if result != nil {
			return json.Unmarshal(message.Result, result)
		}
		return nil
	}
}

func (c *cdpContext) closeConnection() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *cdpContext) Navigate(ctx context.Context, rawURL string) error {
	var result struct {
		ID string `json:"targetId"`
	}
	err := c.call(ctx, "Target.createTarget", map[string]any{"url": rawURL, "browserContextId": c.id, "background": true}, &result)
	if err == nil && result.ID == "" {
		return errors.New("CDP did not create a target in the owned context")
	}
	return err
}

func (c *cdpContext) Close(ctx context.Context) error {
	err := c.call(ctx, "Target.disposeBrowserContext", map[string]string{"browserContextId": c.id}, nil)
	c.closeConnection()
	if err == nil {
		return nil
	}
	// An ACK can be lost after disposal. Confirm absence using a fresh socket;
	// never close another context or claim a stop while verification is offline.
	var result struct {
		IDs []string `json:"browserContextIds"`
	}
	if verifyErr := c.call(ctx, "Target.getBrowserContexts", map[string]any{}, &result); verifyErr != nil {
		c.closeConnection()
		return errors.Join(err, verifyErr)
	}
	c.closeConnection()
	for _, id := range result.IDs {
		if id == c.id {
			return err
		}
	}
	return nil
}

func verifyExternalBrowserNetworkFlags(ctx context.Context, endpoint string) error {
	c := &cdpContext{endpoint: endpoint}
	defer c.closeConnection()
	var result struct {
		Arguments []string `json:"arguments"`
	}
	if err := c.call(ctx, "Browser.getBrowserCommandLine", map[string]any{}, &result); err != nil {
		return fmt.Errorf("%w: 无法验证外部浏览器的网络隔离启动参数，请使用应用管理的 Chrome/Edge 模式", ErrBrMode)
	}
	for _, required := range []string{"--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"} {
		if !slices.Contains(result.Arguments, required) {
			return fmt.Errorf("%w: 外部浏览器缺少受限网络启动参数 %s，请使用应用管理的 Chrome/Edge 模式", ErrBrMode, required)
		}
	}
	return nil
}
