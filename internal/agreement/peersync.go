package agreement

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"nexum/adapter"
)

const peerSyncHeader = "COMLINKPEERSYNC1"

// peerSyncStore persists AES-256-GCM authenticated per-destination agreement
// convergence records in a private 0700 endpoint directory. Zero speech is stored.
type peerSyncStore struct {
	dir  string
	aead cipher.AEAD
}

func openPeerSyncStore(dir string, key []byte) (*peerSyncStore, error) {
	if len(key) != 32 {
		return nil, adapter.ErrStorageKey
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(abs, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, adapter.ErrStorageDir
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &peerSyncStore{dir: abs, aead: aead}, nil
}

func (s *peerSyncStore) fileForKey(name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".sync")
}

func (s *peerSyncStore) Read(name string) ([]byte, error) {
	raw, err := os.ReadFile(s.fileForKey(name))
	if err != nil {
		return nil, err
	}
	hdrLen := len(peerSyncHeader)
	ns := s.aead.NonceSize()
	if len(raw) < hdrLen+ns+s.aead.Overhead() || string(raw[:hdrLen]) != peerSyncHeader {
		return nil, errors.New("agreement: corrupt peer sync record")
	}
	nonce := raw[hdrLen : hdrLen+ns]
	ct := raw[hdrLen+ns:]
	return s.aead.Open(nil, nonce, ct, []byte(peerSyncHeader+"/"+name))
}

func (s *peerSyncStore) Write(name string, plain []byte) error {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := s.aead.Seal(nil, nonce, plain, []byte(peerSyncHeader+"/"+name))
	buf := make([]byte, 0, len(peerSyncHeader)+len(nonce)+len(ct))
	buf = append(buf, peerSyncHeader...)
	buf = append(buf, nonce...)
	buf = append(buf, ct...)

	dst := s.fileForKey(name)
	var suffix [8]byte
	_, _ = rand.Read(suffix[:])
	tmp := dst + ".tmp." + hex.EncodeToString(suffix[:])
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if df, derr := os.Open(s.dir); derr == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}
