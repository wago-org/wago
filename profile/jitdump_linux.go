//go:build linux && !tinygo

package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wago-org/wago/internal/jitprofile"
	"golang.org/x/sys/unix"
)

// JITFile keeps perf's executable mmap discovery marker alive for the capture.
type JITFile struct {
	*JITDump
	f      *os.File
	marker []byte
}

func OpenJITDump(dir string) (*JITFile, error) {
	path := filepath.Join(dir, fmt.Sprintf("jit-%d.dump", os.Getpid()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	j, err := NewJITDump(f, uint32(os.Getpid()), runtime.GOARCH, jitprofile.Now())
	if err != nil {
		f.Close()
		return nil, err
	}
	marker, err := unix.Mmap(int(f.Fd()), 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_EXEC, unix.MAP_PRIVATE)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("jitdump discovery mmap: %w", err)
	}
	return &JITFile{JITDump: j, f: f, marker: marker}, nil
}
func (f *JITFile) Close() error {
	if f.f == nil {
		return nil
	}
	err := errors.Join(f.JITDump.Close(jitprofile.Now()), f.f.Sync(), unix.Munmap(f.marker), f.f.Close())
	f.f = nil
	f.marker = nil
	return err
}

// RefreshMarker republishes perf's discovery mmap after an acknowledged enable.
// This also works with collectors that omit mapping events while disabled.
func (f *JITFile) RefreshMarker() error {
	if f.f == nil {
		return fmt.Errorf("jitdump is closed")
	}
	marker, err := unix.Mmap(int(f.f.Fd()), 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_EXEC, unix.MAP_PRIVATE)
	if err != nil {
		return err
	}
	old := f.marker
	f.marker = marker
	return unix.Munmap(old)
}
