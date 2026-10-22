//go:build linux && !tinygo

package riscv64

import (
	"errors"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"sync"
)

// A successful identity lasts for the process. Failed unpublished maps remain
// owned here until cleanup succeeds; a later construction retries cleanup first.
type nativeTailExitCache struct {
	mu               sync.Mutex
	code             *coreruntime.CodeBuffer
	pointer          uintptr
	publicationError error
}

func (s *nativeTailExitCache) get(build func() ([]byte, error), allocate func(int) (*coreruntime.CodeBuffer, error), seal, closeCode func(*coreruntime.CodeBuffer) error) (uintptr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pointer != 0 {
		return s.pointer, nil
	}
	if s.code != nil {
		if err := closeCode(s.code); err != nil {
			return 0, errors.Join(s.publicationError, err)
		}
		s.code = nil
		s.publicationError = nil
	}
	code, err := build()
	if err != nil {
		return 0, err
	}
	s.code, err = allocate(len(code))
	if err == nil {
		err = s.code.Append(code)
	}
	if err == nil {
		err = seal(s.code)
	}
	if err != nil {
		s.publicationError = err
		if s.code != nil {
			if cleanup := closeCode(s.code); cleanup != nil {
				return 0, errors.Join(err, cleanup)
			}
			s.code = nil
		}
		return 0, err
	}
	s.pointer = s.code.Base()
	return s.pointer, nil
}
