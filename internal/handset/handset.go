// Package handset keeps all speech keys and plaintext at the agent endpoint.
package handset

import (
	"context"
	"crypto/ecdh"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"nexum/circuit"
)

type Config struct {
	SubscriberID string             `json:"subscriber_id,omitempty"`
	Audience     string             `json:"audience,omitempty"`
	Identity     circuit.Identity   `json:"identity"`
	Credential   circuit.Credential `json:"credential"`
	Roots        map[string]string  `json:"roots"`
}
type liveCall struct {
	State   circuit.State
	Private *ecdh.PrivateKey
	Key     []byte
}
type Event struct {
	Type   string         `json:"type"`
	Call   string         `json:"call,omitempty"`
	Packet circuit.Packet `json:"packet,omitempty"`
	State  *circuit.State `json:"state,omitempty"`
	Text   string         `json:"text,omitempty"`
}
type Phone struct {
	Config     Config
	remote     *mcp.ClientSession
	mu         sync.Mutex
	operations sync.Mutex
	calls      map[string]*liveCall
	notify     func(Event)
	done       chan struct{}
	disconnect sync.Once
}
type authTransport struct {
	base  http.RoundTripper
	token string
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("Authorization", "Bearer "+t.token)
	if r.Method == http.MethodDelete {
		// The SDK's graceful session Close waits for DELETE before canceling
		// its detached transport context. Bound cleanup without timing out SSE.
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		response, err := t.base.RoundTrip(r.WithContext(ctx))
		cancel()
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
			response.Body = http.NoBody
		}
		return response, err
	}
	return t.base.RoundTrip(r)
}
func Connect(ctx context.Context, cfg Config, endpoint, token, ca string, notify func(Event)) (*Phone, error) {
	public, e := cfg.Identity.Public()
	if e != nil || public.ID() != cfg.Credential.Owner.ID() || cfg.Credential.Admitted(cfg.Roots) != nil {
		return nil, errors.New("invalid endpoint identity or credential")
	}
	u, e := url.Parse(endpoint)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/mcp" || (u.Scheme != "https" && (u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, errors.New("HTTPS /mcp endpoint required (HTTP only on loopback)")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	if ca != "" {
		raw, e := os.ReadFile(ca)
		if e != nil {
			return nil, e
		}
		roots, e := x509.SystemCertPool()
		if e != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("invalid CA")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	httpClient := &http.Client{Transport: authTransport{base: transport, token: strings.TrimSpace(token)}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	p := &Phone{Config: cfg, calls: map[string]*liveCall{}, notify: notify, done: make(chan struct{})}
	client := mcp.NewClient(&mcp.Implementation{Name: "comlink-handset", Version: "0.1.0"}, &mcp.ClientOptions{KeepAlive: 10 * time.Second, LoggingMessageHandler: func(ctx context.Context, r *mcp.LoggingMessageRequest) {
		if r.Params.Logger != "comlink.events" {
			return
		}
		raw, _ := json.Marshal(r.Params.Data)
		var event Event
		if json.Unmarshal(raw, &event) == nil {
			p.receive(event)
		}
	}})
	p.remote, e = client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient}, nil)
	if e != nil {
		return nil, e
	}
	if e = p.remote.SetLoggingLevel(ctx, &mcp.SetLoggingLevelParams{Level: "info"}); e != nil {
		p.Close()
		return nil, e
	}
	if _, e = p.rpc(ctx, "register", map[string]any{"credential": cfg.Credential, "proof": circuit.RegistrationProof(cfg.Identity, p.remote.ID())}); e != nil {
		p.Close()
		return nil, e
	}
	go func() { _ = p.remote.Wait(); p.disconnected() }()
	return p, nil
}

// Connected reports whether the authenticated remote session remains usable.
// Registration must establish a new session after remote loss.
func (p *Phone) Connected() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *Phone) disconnected() {
	ids, first := p.markDisconnected()
	p.notifyDisconnected(ids, first)
}

func (p *Phone) markDisconnected() (ids []string, first bool) {
	p.disconnect.Do(func() {
		close(p.done)
		ids = p.dropCalls()
		first = true
	})
	return ids, first
}

func (p *Phone) notifyDisconnected(ids []string, first bool) {
	// The once guard protects state changes, never user callbacks. A runtime
	// reacting to closure may itself request another Close.
	if first && p.notify != nil {
		for _, id := range ids {
			p.notify(Event{Type: "closed", Call: id})
		}
		p.notify(Event{Type: "disconnected"})
	}
}

// A failed packet submission may already have changed the remote circuit.
// Retire the session instead of forgetting the only state that could close it.
// State changes precede unlocking; callbacks run after all operation locks.
func (p *Phone) finishOperation(failedSubmission bool) {
	if !failedSubmission {
		p.operations.Unlock()
		return
	}
	ids, first := p.markDisconnected()
	p.operations.Unlock()
	if p.remote != nil {
		// Close waits for notification handlers. Keep it off a handler's stack;
		// authTransport bounds the remote DELETE even if the gateway stalls.
		go func() { _ = p.remote.Close() }()
	}
	p.notifyDisconnected(ids, first)
}

func (p *Phone) Close() {
	p.disconnected()
	if p.remote != nil {
		_ = p.remote.Close()
	}
}
func (p *Phone) dropCalls() []string {
	p.mu.Lock()
	ids := make([]string, 0, len(p.calls))
	for id, c := range p.calls {
		clear(c.Key)
		c.Private = nil
		ids = append(ids, id)
		delete(p.calls, id)
	}
	p.mu.Unlock()
	return ids
}

func (p *Phone) rpc(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if !p.Connected() {
		return nil, errors.New("exchange disconnected; register a fresh session")
	}
	r, e := p.remote.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if e != nil {
		return nil, e
	}
	if r.IsError {
		for _, content := range r.Content {
			if t, ok := content.(*mcp.TextContent); ok {
				return nil, errors.New(t.Text)
			}
		}
		return nil, errors.New("exchange rejected operation")
	}
	if r.StructuredContent != nil {
		return json.Marshal(r.StructuredContent)
	}
	for _, content := range r.Content {
		if t, ok := content.(*mcp.TextContent); ok {
			return []byte(t.Text), nil
		}
	}
	return nil, errors.New("missing result")
}
func (p *Phone) Whois(ctx context.Context, number string) (map[string]any, error) {
	raw, e := p.rpc(ctx, "whois", map[string]string{"number": number})
	if e != nil {
		return nil, e
	}
	var result struct {
		Online     bool               `json:"online"`
		Credential circuit.Credential `json:"credential"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil, circuit.ErrInvalid
	}
	if result.Online && (result.Credential.Number != number || result.Credential.Admitted(p.Config.Roots) != nil) {
		return nil, circuit.ErrInvalid
	}
	return map[string]any{"online": result.Online, "credential": result.Credential}, nil
}
func (p *Phone) Dial(ctx context.Context, to string) (string, error) {
	p.operations.Lock()
	failedSubmission := false
	defer func() { p.finishOperation(failedSubmission) }()
	found, e := p.Whois(ctx, to)
	if e != nil {
		return "", e
	}
	if found["online"] != true {
		return "", errors.New("offline: no voicemail")
	}
	peer := found["credential"].(circuit.Credential)
	private, e := circuit.Ephemeral()
	if e != nil {
		return "", e
	}
	packet := circuit.Packet{Kind: "comlink.circuit", Call: circuit.RandomID(), Epoch: circuit.SessionEpoch(p.remote.ID()), From: p.Config.Credential.Number, To: to, Type: "dial", Seq: 1, Key: private.PublicKey().Bytes()}
	packet.Sign(p.Config.Identity)
	state, e := circuit.Open(p.Config.Credential, peer, packet)
	if e != nil {
		return "", e
	}
	p.mu.Lock()
	if len(p.calls) > 0 {
		p.mu.Unlock()
		return "", errors.New("busy")
	}
	p.calls[packet.Call] = &liveCall{State: state, Private: private}
	p.mu.Unlock()
	if _, e = p.rpc(ctx, "dial", map[string]any{"packet": packet}); e != nil {
		failedSubmission = true
		return "", e
	}
	return packet.Call, nil
}
func (p *Phone) Act(ctx context.Context, id, action, text string) error {
	p.operations.Lock()
	failedSubmission := false
	defer func() { p.finishOperation(failedSubmission) }()
	p.mu.Lock()
	c := p.calls[id]
	if c == nil {
		p.mu.Unlock()
		return errors.New("closed or unknown circuit")
	}
	other := c.State.Callee
	if other.Number == p.Config.Credential.Number {
		other = c.State.Caller
	}
	packet := circuit.Packet{Kind: "comlink.circuit", Call: id, Epoch: c.State.Dial.Epoch, From: p.Config.Credential.Number, To: other.Number, Type: action, Seq: c.State.Sequences[p.Config.Credential.Number] + 1, Previous: c.State.Head}
	var e error
	switch action {
	case "answer":
		c.Private, e = circuit.Ephemeral()
		if e == nil {
			packet.Key = c.Private.PublicKey().Bytes()
			packet.Sign(p.Config.Identity)
		}
	case "say":
		if c.State.Phase != "accepted" {
			p.mu.Unlock()
			return errors.New("recipient has not answered")
		}
		e = circuit.Seal(c.Key, []byte(text), &packet, p.Config.Identity)
	case "reject", "hangup":
		packet.Sign(p.Config.Identity)
	default:
		e = circuit.ErrInvalid
	}
	if e == nil {
		var state circuit.State
		state, e = circuit.Apply(c.State, packet)
		if e == nil {
			c.State = state
			if action == "answer" {
				c.Key, e = circuit.Key(c.Private, c.State.Dial.Key, c.State)
				c.Private = nil
			}
		}
	}
	p.mu.Unlock()
	if e != nil {
		return e
	}
	_, e = p.rpc(ctx, action, map[string]any{"packet": packet})
	if e != nil {
		failedSubmission = true
	} else if action == "hangup" || action == "reject" {
		p.forget(id)
	}
	return e
}
func (p *Phone) forget(id string) {
	p.mu.Lock()
	if c := p.calls[id]; c != nil {
		clear(c.Key)
		c.Private = nil
		delete(p.calls, id)
	}
	p.mu.Unlock()
}
func (p *Phone) receive(event Event) {
	// The relay supplies event metadata, never trusted prose. Only a successful
	// authenticated speech decryption below may populate local event text.
	event.Text = ""
	p.mu.Lock()
	if !p.Connected() {
		p.mu.Unlock()
		return
	}
	var e error
	if event.Type == "closed" {
		if c := p.calls[event.Call]; c != nil {
			clear(c.Key)
			c.Private = nil
			delete(p.calls, event.Call)
		}
		p.mu.Unlock()
		if p.notify != nil {
			p.notify(Event{Type: "closed", Call: event.Call})
		}
		return
	}
	packet := event.Packet
	event.Call = packet.Call
	validType := event.Type == "ring" && packet.Type == "dial"
	switch event.Type {
	case "answer", "reject", "say", "hangup":
		validType = event.Type == packet.Type
	}
	if !validType {
		e = circuit.ErrInvalid
	} else if event.Type == "ring" {
		if event.State == nil || event.State.Caller.Admitted(p.Config.Roots) != nil || event.State.Callee.Owner.ID() != p.Config.Credential.Owner.ID() || event.State.Callee.Number != p.Config.Credential.Number || len(p.calls) > 0 {
			e = circuit.ErrInvalid
		} else {
			var state circuit.State
			state, e = circuit.Open(event.State.Caller, event.State.Callee, packet)
			if e == nil {
				p.calls[packet.Call] = &liveCall{State: state}
			}
		}
	} else {
		c := p.calls[packet.Call]
		if c == nil {
			e = circuit.ErrInvalid
		} else {
			var next circuit.State
			next, e = circuit.Apply(c.State, packet)
			if e == nil {
				c.State = next
				switch packet.Type {
				case "answer":
					if c.Private == nil {
						e = circuit.ErrInvalid
					} else {
						c.Key, e = circuit.Key(c.Private, packet.Key, c.State)
						c.Private = nil
					}
				case "say":
					owner := c.State.Caller.Owner
					if packet.From == c.State.Callee.Number {
						owner = c.State.Callee.Owner
					}
					var text []byte
					text, e = circuit.Unseal(c.Key, packet, owner)
					event.Text = string(text)
				case "reject", "hangup":
					clear(c.Key)
					c.Private = nil
					delete(p.calls, packet.Call)
				}
			}
		}
	}
	closed := false
	if e != nil {
		if c := p.calls[packet.Call]; c != nil {
			clear(c.Key)
			c.Private = nil
			delete(p.calls, packet.Call)
			closed = true
		}
	}
	p.mu.Unlock()
	if closed && p.notify != nil {
		p.notify(Event{Type: "closed", Call: packet.Call})
	}
	if closed && p.remote != nil {
		// Close waits for notification handlers, so it cannot run on this
		// handler's stack. Dropping the session also ends the peer's circuit.
		go p.Close()
	}
	if e == nil && p.notify != nil {
		event.State = nil
		event.Packet.Body = nil
		p.notify(event)
	}
}
