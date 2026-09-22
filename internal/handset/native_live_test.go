package handset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in deployment smoke test. Creates two isolated test identities and revokes
// them on cleanup; it never contacts an existing person or inference provider.
func TestNativeDeploymentSmoke(t *testing.T) {
	endpoint, roots := os.Getenv("COMLINK_NATIVE_LIVE_ENDPOINT"), os.Getenv("COMLINK_NATIVE_LIVE_ROOTS")
	if endpoint == "" || roots == "" {
		t.Skip("explicit live endpoint and pinned roots required")
	}
	dir := t.TempDir()
	create := func(name string) (*Native, string) {
		path := filepath.Join(dir, name+".json")
		n := NewNative(path, roots, endpoint)
		t.Cleanup(func() {
			n.Close()
			cfg, err := PublicProfile(path, roots, endpoint)
			if err != nil || cfg.SubscriberID == "" {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err = EnrollmentOperation(ctx, cfg, endpoint, "", "revoke", cfg.SubscriberID, nil); err != nil {
				t.Errorf("revoke smoke identity %s: %v", name, err)
			}
		})
		number, err := n.Open()
		if err != nil {
			t.Fatal("open", name, err)
		}
		return n, number
	}
	a, an := create("a")
	b, bn := create("b")
	await := func(n *Native, kind string) map[string]string {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			raw, err := n.NextEvent()
			if err != nil {
				t.Fatal(err)
			}
			if raw == "" {
				continue
			}
			var e map[string]string
			if json.Unmarshal([]byte(raw), &e) != nil {
				t.Fatal("invalid event")
			}
			if e["type"] == kind {
				return e
			}
			if e["type"] == "disconnected" {
				t.Fatal("unexpected disconnect")
			}
		}
		t.Fatal("missing", kind)
		return nil
	}
	if err := a.SaveContact("Smoke B", bn, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dial(bn); err != nil {
		t.Fatal(err)
	}
	if await(b, "ring")["peer"] != an {
		t.Fatal("caller mismatch")
	}
	if err := b.Act("answer", ""); err == nil {
		t.Fatal("trust gate bypassed")
	}
	if err := b.SaveContact("Smoke A", an, true); err != nil {
		t.Fatal(err)
	}
	if err := b.Act("answer", ""); err != nil {
		t.Fatal(err)
	}
	await(a, "answer")
	if err := a.Act("say", "Comlink iOS deployment smoke A"); err != nil {
		t.Fatal(err)
	}
	if await(b, "say")["text"] != "Comlink iOS deployment smoke A" {
		t.Fatal("text mismatch")
	}
	if err := b.Act("say", "Comlink iOS deployment smoke B"); err != nil {
		t.Fatal(err)
	}
	if await(a, "say")["text"] != "Comlink iOS deployment smoke B" {
		t.Fatal("reply mismatch")
	}
	if err := a.Act("hangup", ""); err != nil {
		t.Fatal(err)
	}
}
