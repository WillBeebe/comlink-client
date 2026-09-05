package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupPreservesIdentityAndPins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if e := run([]string{"setup", "--state", dir}); e != nil {
		t.Fatal(e)
	}
	profile := filepath.Join(dir, "handset.json")
	if _, e := os.Stat(profile); !os.IsNotExist(e) {
		t.Fatal("setup created an identity")
	}
	if e := os.WriteFile(profile, []byte("private fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := run([]string{"setup", "--state", dir}); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(profile)
	if string(got) != "private fixture" {
		t.Fatal("profile changed")
	}
	if e := os.WriteFile(filepath.Join(dir, "roots.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := run([]string{"setup", "--state", dir}); e == nil {
		t.Fatal("silently changed roots")
	}
}
func TestSetupRefusesUnsafeStateAndServerCommands(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if e := os.Mkdir(dir, 0755); e != nil {
		t.Fatal(e)
	}
	if e := run([]string{"setup", "--state", dir}); e == nil {
		t.Fatal("unsafe permissions")
	}
	for _, command := range []string{"serve", "serve-public", "public-init", "mother", "codec", "suspend"} {
		if e := run([]string{command}); e == nil {
			t.Fatal(command)
		}
	}
}
