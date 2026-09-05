package handset

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// EventCapability is negotiated at initialization by the supported local
// runtime adapter. Call content is never a diagnostic logging notification.
const EventCapability = "comlink.fyi/events"
const EventNotification = "notifications/comlink.fyi/event"
const EventVersion = 1

// LiveEvent contains only authenticated endpoint information. Seq increases
// across one local MCP session, independently of each Nex circuit's sequence.
// The receiver must process events in RAM and cancel call work on disconnect.
type LiveEvent struct {
	Version int    `json:"version"`
	Seq     uint64 `json:"seq"`
	Type    string `json:"type"`
	Call    string `json:"call,omitempty"`
	From    string `json:"from,omitempty"`
	Text    string `json:"text,omitempty"`
}

func supportsEvents(session *mcp.ServerSession) bool {
	p := session.InitializeParams()
	if p == nil || p.Capabilities == nil {
		return false
	}
	raw, err := json.Marshal(p.Capabilities.Experimental[EventCapability])
	if err != nil {
		return false
	}
	var capability struct {
		Version int `json:"version"`
	}
	return json.Unmarshal(raw, &capability) == nil && capability.Version == EventVersion
}

// eventTransport uses the SDK's concurrency-safe transport interface to send
// the negotiated extension. It is used only with a single local stdio session.
type eventTransport struct {
	mcp.Transport
	mu   sync.Mutex
	conn mcp.Connection
}

func (t *eventTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	return conn, nil
}

func (t *eventTransport) send(ctx context.Context, event LiveEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return errors.New("invalid live event")
	}
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return errors.New("local event connection unavailable")
	}
	// The SDK's stdio writer checks the context before Write, but a blocked
	// pipe write needs an explicit Close to release it after cancellation.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	err = conn.Write(ctx, &jsonrpc.Request{Method: EventNotification, Params: raw})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
