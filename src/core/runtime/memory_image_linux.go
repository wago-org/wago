//go:build linux && (amd64 || arm64)

package runtime

import (
	"errors"
	"fmt"
	"syscall"
)

// ErrImageMappingUnavailable identifies a resource failure while mapping an
// otherwise valid image. Callers may retry with ordinary linear memory.
var ErrImageMappingUnavailable = errors.New("runtime: image mapping unavailable")

// NewJobMemoryGrowableFromImage maps a prebuilt basedata-plus-linear-memory
// image privately. The caller retains ownership of fd; it can close fd after
// this returns because the instance owns the mapping. The image must include
// the complete sparse max reservation, not just the initialized prefix, so a
// later memory.grow reads zeros instead of faulting beyond EOF.
func NewJobMemoryGrowableFromImage(initialBytes, maxBytes, fd int) (*JobMemory, error) {
	if err := validateJobMemorySizes(initialBytes, maxBytes); err != nil {
		return nil, err
	}
	initialBytes, maxBytes, reserveBytes := normalizeMemorySizes(initialBytes, maxBytes)
	size := basedataSize + reserveBytes
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Size < int64(size) {
		return nil, fmt.Errorf("runtime: CoW image size %d is below reservation %d", stat.Size, size)
	}
	mem, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_NORESERVE)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageMappingUnavailable, err)
	}
	j := &JobMemory{mem: mem, linOff: basedataSize, linLen: reserveBytes, imageBacked: true}
	j.reset(initialBytes, maxBytes, reserveBytes, false)
	if err := j.registerInterruptLinearMemory(); err != nil {
		_ = j.Close()
		return nil, err
	}
	return j, nil
}
