package handset

import (
	"encoding/json"
	"nexum/circuit"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeTrustBoundaries(t *testing.T) {
	n := NewNative(filepath.Join(t.TempDir(), "profile.json"), "", "https://example.invalid/mcp")
	if err := n.SaveContact("bad", "+15555555555", true); err == nil {
		t.Fatal("accepted SMS number")
	}
	peer := strings.Repeat("a", 128)
	if err := n.SaveContact("Alice", peer, true); err != nil {
		t.Fatal(err)
	}
	if err := n.SaveContact("Alice", strings.Repeat("b", 128), true); err == nil {
		t.Fatal("silently replaced trusted number")
	}
	if err := n.Act("answer", ""); err == nil {
		t.Fatal("answered absent call")
	}
	n.peer = peer
	n.call = "call"
	n.accepted = false
	n.events <- Event{Type: "say", Call: "call", Packet: circuit.Packet{From: peer}, Text: "unaccepted"}
	if raw, err := n.NextEvent(); err != nil || raw != "" {
		t.Fatal("exposed unaccepted text", err)
	}
	n.accepted = true
	n.events <- Event{Type: "say", Call: "other", Packet: circuit.Packet{From: peer}, Text: "stale"}
	if raw, err := n.NextEvent(); err != nil || raw != "" {
		t.Fatal("exposed other call text", err)
	}
	n.events <- Event{Type: "say", Call: "call", Packet: circuit.Packet{From: peer, Key: []byte("secret")}, Text: "hello"}
	raw, err := n.NextEvent()
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if json.Unmarshal([]byte(raw), &event) != nil || event["text"] != "hello" || event["packet"] != nil || strings.Contains(raw, "secret") {
		t.Fatal("unsafe event projection")
	}
	if err = n.SaveContact("Alice", peer, false); err != nil {
		t.Fatal(err)
	}
	n.events <- Event{Type: "say", Call: "call", Packet: circuit.Packet{From: peer}, Text: "revoked"}
	if raw, err = n.NextEvent(); err != nil || raw != "" {
		t.Fatal("exposed revoked text", err)
	}
}
