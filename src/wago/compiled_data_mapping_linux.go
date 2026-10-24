//go:build linux

package wago

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
)

const minMappedCompactData = 1 << 20

// mapCompactActiveData writes large immutable payloads through the kernel page
// cache, then maps an exact read-only view. Unlike copying into an anonymous Go
// allocation, publication does not fault a second resident copy into the
// process while the caller's module bytes are still live.
func mapCompactActiveData(c *Compiled, payloadBytes int) ([]byte, error) {
	if payloadBytes < minMappedCompactData {
		return nil, nil
	}
	f, err := os.CreateTemp("", "wago-active-data-*")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(name)
	}()
	if err := os.Remove(name); err != nil {
		return nil, err
	}
	w := bufio.NewWriterSize(f, 256<<10)
	written := 0
	for i := 0; i < c.activeDataCount(); i++ {
		n, err := w.Write(c.activeDataAt(i).Bytes)
		written += n
		if err != nil {
			return nil, err
		}
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	if written != payloadBytes {
		return nil, fmt.Errorf("compiled active data: wrote %d bytes, want %d", written, payloadBytes)
	}
	return syscall.Mmap(int(f.Fd()), 0, payloadBytes, syscall.PROT_READ, syscall.MAP_SHARED)
}

func unmapCompactData(bytes []byte) error {
	if len(bytes) == 0 {
		return nil
	}
	return syscall.Munmap(bytes)
}
