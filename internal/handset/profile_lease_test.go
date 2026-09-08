package handset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileLeaseExcludesOtherHandsetAndReleases(t *testing.T) {
	p := filepath.Join(t.TempDir(), "handset.json")
	release, err := leaseProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if extra, err := leaseProfile(p); err == nil {
		extra()
		t.Fatal("second live owner allowed")
	}
	release()
	release, err = leaseProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestProfileLeaseRejectsSymlink(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "other"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "handset.json")
	if err := os.Symlink(filepath.Join(d, "other"), p+".session.lock"); err != nil {
		t.Fatal(err)
	}
	if release, err := leaseProfile(p); err == nil {
		release()
		t.Fatal("followed symlink")
	}
}
