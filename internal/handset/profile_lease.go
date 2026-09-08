package handset

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// leaseProfile prevents two live MCP handsets from registering one identity.
// The separate inode survives atomic profile updates; process exit releases it.
func leaseProfile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path+".session.lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("cannot open private handset session lock")
	}
	var st syscall.Stat_t
	if syscall.Fstat(fd, &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Mode&0077 != 0 || st.Uid != uint32(os.Getuid()) {
		syscall.Close(fd)
		return nil, errors.New("unsafe handset session lock")
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		syscall.Close(fd)
		return nil, errors.New("this identity already has a live handset; use its managed MCP connection")
	}
	return func() { _ = syscall.Close(fd) }, nil
}
