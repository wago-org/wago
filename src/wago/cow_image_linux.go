//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type cowImageUnavailableError struct{ cause error }

func (e *cowImageUnavailableError) Error() string { return e.cause.Error() }
func (e *cowImageUnavailableError) Unwrap() error { return e.cause }

func cowImageUnavailable(op string, err error) error {
	return &cowImageUnavailableError{fmt.Errorf("%s: %w", op, err)}
}

func (c *Compiled) experimentalCOWImageMemory(initial, max int) (*runtime.JobMemory, bool, error) {
	fd, eligible, err := c.experimentalCOWImageFD(initial, max)
	if err != nil {
		var unavailable *cowImageUnavailableError
		if errors.As(err, &unavailable) {
			return nil, false, nil // ordinary per-instance allocation
		}
		return nil, eligible, err
	}
	if !eligible {
		return nil, false, nil
	}
	defer syscall.Close(fd)
	jm, err := runtime.NewJobMemoryGrowableFromImage(initial, max, fd)
	if err != nil {
		if errors.Is(err, runtime.ErrImageMappingUnavailable) {
			return nil, false, nil // retry ordinary memory on mapping resource failure
		}
		return nil, false, err
	}
	return jm, true, nil
}

// experimentalCOWImageFD returns a duplicate descriptor for one instantiation.
// The compiled module owns the original; Close and artifact replacement close
// it. Existing MAP_PRIVATE mappings do not depend on the descriptor lifetime.
func (c *Compiled) experimentalCOWImageFD(initial, max int) (int, bool, error) {
	mode := os.Getenv("WAGO_EXPERIMENT_COW_IMAGE")
	if c.activeDataCount() == 0 || c.memoryCount() != 1 ||
		c.memoryImport != "" || c.boundsMode != BoundsChecksExplicit ||
		(mode != "1" && mode != "eager" && mode != "force") {
		return -1, false, nil
	}
	// Both public opt-ins require a substantial image. The default opt-in
	// defers image creation until reuse, avoiding a single-use cost; "eager"
	// favors memory at first use. "force" bypasses only the cost gates for
	// correctness and mechanism measurements.
	if mode != "force" && (c.activeDataCount() < 1024 || initial < 1<<20) {
		return -1, false, nil
	}
	def := c.memoryDef(0)
	if def.ImportKey != "" || def.Shared || def.Addr64 {
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
	if !indexes.cowImageConfigured || indexes.cowImageInitial != initial || indexes.cowImageMax != max {
		// Existing instance MAP_PRIVATE mappings remain valid after closing the
		// compiled owner's descriptor. Do not reuse a file with the wrong
		// reservation size or an admission proof for another initial size.
		if indexes.memoryImage != nil {
			if err := indexes.memoryImage.Close(); err != nil {
				return -1, false, fmt.Errorf("close replaced CoW image: %w", err)
			}
			indexes.memoryImage = nil
		}
		indexes.cowImageInitial, indexes.cowImageMax = initial, max
		indexes.cowImageEnd = 0
		indexes.cowImageConfigured = true
		indexes.cowImageAttempted = false
		indexes.cowImageChecked = false
		indexes.cowImageEligible = false
	}
	if !indexes.cowImageAttempted {
		indexes.cowImageAttempted = true
		if mode == "1" {
			return -1, false, nil
		}
	}
	if !indexes.cowImageChecked {
		var imageEnd uint64
		eligible := true
		for i := 0; i < c.activeDataCount(); i++ {
			d := c.activeDataAt(i)
			if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
				eligible = false
				break
			}
			end := uint64(d.Offset.Base) + uint64(len(d.Bytes))
			if end > uint64(initial) { // preserve the ordinary bounds error path
				eligible = false
				break
			}
			if end > imageEnd {
				imageEnd = end
			}
		}
		indexes.cowImageEnd = imageEnd
		indexes.cowImageEligible = eligible && imageEnd != 0
		indexes.cowImageChecked = true
	}
	if !indexes.cowImageEligible {
		return -1, false, nil
	}
	if indexes.memoryImage == nil {
		fd, err := unix.MemfdCreate("wago-cow-image", unix.MFD_CLOEXEC)
		if err != nil {
			return -1, false, cowImageUnavailable("create CoW image", err)
		}
		file := os.NewFile(uintptr(fd), "wago-cow-image")
		if err := file.Truncate(int64(abi.BasedataSize) + int64(max)); err != nil {
			_ = file.Close()
			return -1, false, cowImageUnavailable("size CoW image", err)
		}
		prefix, err := syscall.Mmap(fd, 0, abi.BasedataSize+int(indexes.cowImageEnd),
			syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			_ = file.Close()
			return -1, false, cowImageUnavailable("build CoW image", err)
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
	dup, err := unix.FcntlInt(indexes.memoryImage.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, false, cowImageUnavailable("duplicate CoW image", err)
	}
	return dup, true, nil
}
