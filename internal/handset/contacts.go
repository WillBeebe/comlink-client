package handset

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

// Contact contains routing and owner-approved communication permission only.
// It never grants work, spending, tools, or authority over the local agent.
type Contact struct {
	Name                  string `json:"name"`
	Number                string `json:"number"`
	CommunicationApproved bool   `json:"communication_approved"`
}

type addressBook struct{ path string }

func contactNumber(number string) (string, error) {
	number = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(number), "-", ""))
	if len(number) != 128 {
		return "", errors.New("a 128-character Comlink number is required")
	}
	for _, c := range number {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return "", errors.New("invalid Comlink number")
		}
	}
	return number, nil
}

func (b addressBook) access(change func(*[]Contact) error) ([]Contact, error) {
	if b.path == "" {
		return nil, errors.New("persistent contacts require a local handset profile")
	}
	// A separate lock survives atomic file replacement and coordinates sessions.
	fd, err := syscall.Open(b.path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), b.path+".lock")
	defer lock.Close()
	if err = syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	contacts := []Contact{}
	if info, err := os.Lstat(b.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1024*1024 {
			return nil, errors.New("address book must be a private regular file under 1 MiB")
		}
		data, err := os.ReadFile(b.path)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &contacts); err != nil {
			return nil, errors.New("invalid address book; refusing to overwrite")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if change != nil {
		if err = change(&contacts); err != nil {
			return nil, err
		}
		sort.Slice(contacts, func(i, j int) bool { return strings.ToLower(contacts[i].Name) < strings.ToLower(contacts[j].Name) })
		data, err := json.MarshalIndent(contacts, "", "  ")
		if err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(filepath.Dir(b.path), ".comlink-contacts-")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(data); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err = os.Rename(f.Name(), b.path); err != nil {
			return nil, err
		}
	}
	return contacts, nil
}

func (b addressBook) save(c Contact) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 80 || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 {
		return errors.New("contact name must be 1–80 bytes without control characters")
	}
	number, err := contactNumber(c.Number)
	if err != nil {
		return err
	}
	c.Number = number
	_, err = b.access(func(all *[]Contact) error {
		for i, old := range *all {
			if strings.EqualFold(old.Name, c.Name) {
				if old.Number != c.Number {
					return errors.New("name already belongs to another number; remove it explicitly or choose another name")
				}
				(*all)[i] = c
				return nil
			}
			if old.Number == c.Number {
				return errors.New("number already saved under another name")
			}
		}
		if len(*all) >= 1000 {
			return errors.New("address book limit reached")
		}
		*all = append(*all, c)
		return nil
	})
	return err
}
