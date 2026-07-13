//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import (
	"fmt"
	"syscall"
)

// CodeArena is a bounded RW staging mapping for JIT output. Seal changes the
// whole mapping to RX, preserving W^X while avoiding the normal heap-to-image
// copy. It owns its mapping until Take or Close.
type CodeArena struct {
	mem    []byte
	sealed bool
}

func NewCodeArena(n int) (*CodeArena, error) {
	if n <= 0 {
		return nil, fmt.Errorf("jit: invalid code arena size %d", n)
	}
	mem, err := mmapCodeRW(n)
	if err != nil {
		return nil, err
	}
	return &CodeArena{mem: mem}, nil
}

// Bytes returns an empty slice whose capacity is the bounded output capacity.
func (a *CodeArena) Bytes() []byte {
	if a == nil || a.sealed || a.mem == nil {
		return nil
	}
	return a.mem[:0:len(a.mem)]
}

// Seal changes the mapping to RX and records used bytes. Take transfers the
// sealed mapping to its long-lived owner.
func (a *CodeArena) Seal(used int) error {
	if a == nil || a.mem == nil {
		return fmt.Errorf("jit: code arena is closed")
	}
	if a.sealed {
		return fmt.Errorf("jit: code arena already sealed")
	}
	if used < 0 || used > len(a.mem) {
		return fmt.Errorf("jit: code arena used length %d exceeds %d", used, len(a.mem))
	}
	if err := syscall.Mprotect(a.mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		return err
	}
	a.sealed = true
	return nil
}

// Take transfers a sealed mapping. It returns the full mapping so Close can
// unmap the page-rounded allocation; used slices it to actual machine code.
func (a *CodeArena) Take(used int) ([]byte, uintptr, error) {
	if a == nil || !a.sealed || a.mem == nil {
		return nil, 0, fmt.Errorf("jit: code arena is not sealed")
	}
	if used < 0 || used > len(a.mem) {
		return nil, 0, fmt.Errorf("jit: code arena used length %d exceeds %d", used, len(a.mem))
	}
	mem := a.mem
	a.mem = nil
	return mem, slicePtr(mem), nil
}

func (a *CodeArena) Close() error {
	if a == nil || a.mem == nil {
		return nil
	}
	mem := a.mem
	a.mem = nil
	return munmap(mem)
}
