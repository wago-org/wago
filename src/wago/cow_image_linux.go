//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func (c *Compiled) experimentalCOWImageMemory(initial, max int) (*runtime.JobMemory, bool, error) {
	fd, eligible, err := c.experimentalCOWImageFD(initial, max)
	if err != nil || !eligible {
		return nil, eligible, err
	}
	defer syscall.Close(fd)
	jm, err := runtime.NewJobMemoryGrowableFromImage(initial, max, fd)
	return jm, true, err
}

// experimentalCOWImageFD returns a duplicate descriptor for one instantiation.
// The compiled module owns the original; Close and artifact replacement close
// it. Existing MAP_PRIVATE mappings do not depend on the descriptor lifetime.
func (c *Compiled) experimentalCOWImageFD(initial, max int) (int, bool, error) {
	if c.activeDataCount() == 0 || c.memoryCount() != 1 ||
		c.memoryImport != "" || c.boundsMode != BoundsChecksExplicit ||
		os.Getenv("WAGO_EXPERIMENT_COW_IMAGE") != "1" {
		return -1, false, nil
	}
	def := c.memoryDef(0)
	if def.ImportKey != "" || def.Shared || def.Addr64 {
		return -1, false, nil
	}
	var imageEnd uint64
	for i := 0; i < c.activeDataCount(); i++ {
		d := c.activeDataAt(i)
		if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
			return -1, false, nil
		}
		end := uint64(d.Offset.Base) + uint64(len(d.Bytes))
		if end > uint64(initial) { // preserve the ordinary bounds error path
			return -1, false, nil
		}
		if end > imageEnd {
			imageEnd = end
		}
	}
	if imageEnd == 0 {
		return -1, false, nil
	}
	indexes := c.ensureCompileIndexes()
	if indexes == nil {
		return -1, false, fmt.Errorf("compiled module has no validation owner")
	}
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return -1, false, fmt.Errorf("compiled module is closed")
	}
	if indexes.memoryImage == nil {
		fd, err := unix.MemfdCreate("wago-cow-image", unix.MFD_CLOEXEC)
		if err != nil {
			return -1, false, fmt.Errorf("create CoW image: %w", err)
		}
		file := os.NewFile(uintptr(fd), "wago-cow-image")
		if err := file.Truncate(int64(abi.BasedataSize) + int64(max)); err != nil {
			_ = file.Close()
			return -1, false, fmt.Errorf("size CoW image: %w", err)
		}
		prefix, err := syscall.Mmap(fd, 0, abi.BasedataSize+int(imageEnd),
			syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			_ = file.Close()
			return -1, false, fmt.Errorf("build CoW image: %w", err)
		}
		for i := 0; i < c.activeDataCount(); i++ {
			d := c.activeDataAt(i)
			copy(prefix[abi.BasedataSize+int(d.Offset.Base):], d.Bytes)
		}
		if err := syscall.Munmap(prefix); err != nil {
			_ = file.Close()
			return -1, false, fmt.Errorf("unmap CoW image builder: %w", err)
		}
		indexes.memoryImage = file
	}
	dup, err := syscall.Dup(int(indexes.memoryImage.Fd()))
	if err != nil {
		return -1, false, fmt.Errorf("duplicate CoW image: %w", err)
	}
	return dup, true, nil
}
