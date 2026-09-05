package comlinkgo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connect starts the local handset and negotiates events. Register is separate.
// The owner must run comlink setup beforehand. Use one profile per live handset.
// cancel must cancel ctx; it is also called when the event stream fails.
func Connect(ctx context.Context, cancel context.CancelFunc, binary, state string, events chan Event, terminal func(Event)) (*mcp.ClientSession, error) {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(state) {
		return nil, errors.New("absolute binary and state paths required")
	}
	command := exec.CommandContext(ctx, binary, "mcp", "--enroll", "--file", filepath.Join(state, "handset.json"), "--roots", filepath.Join(state, "roots.json"), "--endpoint", "https://api.comlink.fyi/mcp")
	command.Stderr = io.Discard
	transport := &Transport{Base: &mcp.CommandTransport{Command: command, TerminateDuration: 5 * time.Second}, Events: events, OnTerminal: terminal, Stop: cancel}
	client := mcp.NewClient(&mcp.Implementation{Name: "comlink-go-starter", Version: "0.1.0"}, &mcp.ClientOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Capabilities: &mcp.ClientCapabilities{Experimental: map[string]any{"comlink.fyi/events": map[string]int{"version": 1}}}})
	session, e := client.Connect(ctx, transport, nil)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(session.InitializeResult().Capabilities.Experimental["comlink.fyi/events"])
	var capability struct {
		Version int `json:"version"`
	}
	if e != nil || json.Unmarshal(raw, &capability) != nil || capability.Version != 1 {
		cancel()
		_ = session.Close()
		return nil, errors.New("event capability missing")
	}
	return session, nil
}

// Register explicitly enrolls/reuses the local profile; only the public number
// is returned. Call with a deadline. Do not automatically retry uncertain calls.
func Register(ctx context.Context, session *mcp.ClientSession) (string, error) {
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "register", Arguments: map[string]any{}})
	if e != nil {
		return "", e
	}
	if result.IsError {
		return "", errors.New("registration refused")
	}
	raw, e := json.Marshal(result.StructuredContent)
	if e != nil {
		return "", e
	}
	var identity struct {
		Number string `json:"number"`
	}
	if json.Unmarshal(raw, &identity) != nil || !regexp.MustCompile(`^[0-9a-z]{128}$`).MatchString(identity.Number) {
		return "", errors.New("invalid public number")
	}
	return identity.Number, nil
}
