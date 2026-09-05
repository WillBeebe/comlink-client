// Package comlinkgo shows the Comlink event hook for a Go MCP host.
// It is a transport example, not a complete model runtime.
package comlinkgo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Event struct {
	Version int    `json:"version"`
	Seq     uint64 `json:"seq"`
	Type    string `json:"type"`
	Call    string `json:"call,omitempty"`
	From    string `json:"from,omitempty"`
	Text    string `json:"text,omitempty"`
}

var callPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Transport intercepts custom events before normal SDK dispatch.
// Events must be a bounded buffered channel consumed independently of model work.
// OnTerminal must immediately cancel call work and discard queued context; it
// must not wait on provider I/O or log the event. Stop cancels the whole host
// connection on malformed input, EOF or queue overflow. All hooks are required.
type Transport struct {
	Base       mcp.Transport
	Events     chan Event
	OnTerminal func(Event)
	Stop       context.CancelFunc
}

func (t *Transport) Connect(ctx context.Context) (mcp.Connection, error) {
	if t.Base == nil || t.Events == nil || cap(t.Events) < 1 || cap(t.Events) > 64 || t.Stop == nil || t.OnTerminal == nil {
		return nil, errors.New("bounded event channel and lifecycle hooks required")
	}
	c, e := t.Base.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &connection{Connection: c, owner: t}, nil
}

type connection struct {
	mcp.Connection
	owner *Transport
	seq   uint64
}

func (c *connection) fail(err error) (jsonrpc.Message, error) {
	c.owner.OnTerminal(Event{Type: "disconnected"})
	c.owner.Stop()
	return nil, err
}
func (c *connection) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		message, e := c.Connection.Read(ctx)
		if e != nil {
			return c.fail(e)
		}
		r, ok := message.(*jsonrpc.Request)
		if !ok || r.Method != "notifications/comlink.fyi/event" {
			return message, nil
		}
		var event Event
		if r.ID.IsValid() || len(r.Params) > 262144 || json.Unmarshal(r.Params, &event) != nil || event.Version != 1 || event.Seq != c.seq+1 {
			return c.fail(errors.New("invalid Comlink event"))
		}
		terminal := false
		switch event.Type {
		case "reject", "hangup", "closed", "disconnected":
			terminal = true
		case "ring", "answer", "say":
		default:
			return c.fail(errors.New("unknown Comlink event"))
		}
		if (event.Type != "disconnected" && !callPattern.MatchString(event.Call)) || len(event.Text) > 65536 || (event.Type != "say" && event.Text != "") {
			return c.fail(errors.New("invalid event fields"))
		}
		c.seq = event.Seq
		if event.Type == "disconnected" {
			return c.fail(io.EOF)
		}
		if terminal {
			c.owner.OnTerminal(event)
		}
		select {
		case c.owner.Events <- event:
		default:
			return c.fail(errors.New("Comlink event queue full"))
		}
	}
}
