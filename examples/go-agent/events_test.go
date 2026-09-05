package comlinkgo

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"io"
	"strings"
	"testing"
)

type fake struct{ messages []jsonrpc.Message }

func (f *fake) Read(context.Context) (jsonrpc.Message, error) {
	if len(f.messages) == 0 {
		return nil, io.EOF
	}
	m := f.messages[0]
	f.messages = f.messages[1:]
	return m, nil
}
func (f *fake) Write(context.Context, jsonrpc.Message) error { return nil }
func (f *fake) Close() error                                 { return nil }
func (f *fake) SessionID() string                            { return "" }
func notification(seq int, kind string) *jsonrpc.Request {
	b, _ := json.Marshal(map[string]any{"version": 1, "seq": seq, "type": kind, "call": strings.Repeat("a", 64)})
	return &jsonrpc.Request{Method: "notifications/comlink.fyi/event", Params: b}
}
func TestEventInterceptionAndTerminalBeforeResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var terminal []string
	queue := make(chan Event, 4)
	normal := &jsonrpc.Response{}
	c := &connection{Connection: &fake{[]jsonrpc.Message{notification(1, "ring"), notification(2, "hangup"), normal}}, owner: &Transport{Events: queue, OnTerminal: func(e Event) { terminal = append(terminal, e.Type) }, Stop: cancel}}
	got, e := c.Read(ctx)
	if e != nil || got != normal || len(terminal) != 1 || terminal[0] != "hangup" || len(queue) != 2 {
		t.Fatal("event dispatch or terminal cancellation lost")
	}
}
func TestReplayAndOverflowDisconnect(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		queue := make(chan Event, 1)
		seq := 1
		if overflow {
			seq = 2
		}
		c := &connection{Connection: &fake{[]jsonrpc.Message{notification(1, "ring"), notification(seq, "ring")}}, owner: &Transport{Events: queue, OnTerminal: func(Event) {}, Stop: cancel}}
		_, e := c.Read(ctx)
		if e == nil || ctx.Err() == nil {
			t.Fatal("bad stream did not fail closed")
		}
		cancel()
	}
}
