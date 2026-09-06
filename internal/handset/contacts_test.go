package handset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestContactsPersistAndRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.json")
	b := addressBook{path: path}
	number := strings.Repeat("a", 128)
	if err := b.save(Contact{"Ada lab", number, true}); err != nil {
		t.Fatal(err)
	}
	reopened := addressBook{path: path}
	all, err := reopened.access(nil)
	if err != nil || len(all) != 1 || !all[0].CommunicationApproved {
		t.Fatal(all, err)
	}
	if err = b.save(Contact{"Ada lab", strings.Repeat("b", 128), true}); err == nil {
		t.Fatal("silently rebound trusted name")
	}
	if err = b.save(Contact{"Ada lab", number, false}); err != nil {
		t.Fatal(err)
	}
	all, err = reopened.access(nil)
	if err != nil || all[0].CommunicationApproved {
		t.Fatal("revocation not persisted", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("nonprivate contacts")
	}
}
func TestContactsConcurrentAndCorrupt(t *testing.T) {
	b := addressBook{path: filepath.Join(t.TempDir(), "contacts.json")}
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := b.save(Contact{name, strings.Repeat(name, 128), false}); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()
	all, err := b.access(nil)
	if err != nil || len(all) != 4 {
		t.Fatal(all, err)
	}
	if err = os.WriteFile(b.path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = b.save(Contact{"e", strings.Repeat("e", 128), true}); err == nil {
		t.Fatal("overwrote corrupt data")
	}
}
func TestContactsMCPFreshSession(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "handset.json")
	cfg := Config{}
	cfg.Credential.Number = strings.Repeat("a", 128)
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(profile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	connect := func() *mcp.ClientSession {
		server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		addContactTools(server, profile)
		a, b := mcp.NewInMemoryTransports()
		ss, err := server.Connect(context.Background(), a, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ss.Close() })
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), b, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}
	call := func(c *mcp.ClientSession, name string, args any) string {
		r, err := c.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %v %+v", name, err, r)
		}
		raw, _ := json.Marshal(r)
		return string(raw)
	}
	first := connect()
	if !strings.Contains(call(first, "my_number", map[string]any{}), cfg.Credential.Number) {
		t.Fatal("own number missing")
	}
	call(first, "contacts_save", Contact{"Research peer", strings.Repeat("b", 128), true})
	first.Close()
	second := connect()
	result := call(second, "contacts_list", map[string]any{"query": "research", "approved_only": true})
	if !strings.Contains(result, "Research peer") || !strings.Contains(result, "communication_only") {
		t.Fatal(result)
	}
	call(second, "contacts_save", Contact{"Research peer", strings.Repeat("b", 128), false})
	if strings.Contains(call(second, "contacts_list", map[string]any{"approved_only": true}), "Research peer") {
		t.Fatal("revoked contact returned as approved")
	}
	call(second, "contacts_remove", map[string]any{"name": "Research peer"})
	if strings.Contains(call(second, "contacts_list", map[string]any{}), "Research peer") {
		t.Fatal("removed contact remains")
	}
}
