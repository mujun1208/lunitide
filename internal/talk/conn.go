package talk

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Conn is the websocket surface the engine talk loop needs.
type Conn interface {
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	Close() error
}

// Dialer opens a realtime websocket. Tests inject a fake; production uses DefaultDialer.
type Dialer func(ctx context.Context, rawURL string, header http.Header) (Conn, error)

func DefaultDialer(ctx context.Context, rawURL string, header http.Header) (Conn, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 6 * time.Second}
	conn, _, err := dialer.DialContext(ctx, rawURL, header)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func WriteText(conn Conn, payload []byte) error {
	return conn.WriteMessage(websocket.TextMessage, payload)
}
