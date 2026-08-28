//go:build linux && amd64 && tinygo

package runtime

import (
	"errors"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestTinyGoThunkFailureReturnsTrap(t *testing.T) {
	thunkMu.Lock()
	oldCache, oldMapper := thunkCache, tinygoMmapExec
	thunkCache = map[uintptr]uintptr{}
	tinygoMmapExec = func([]byte) ([]byte, error) { return nil, errors.New("mapping denied") }
	lastThunk.Store(nil)
	thunkMu.Unlock()
	defer func() {
		thunkMu.Lock()
		thunkCache, tinygoMmapExec = oldCache, oldMapper
		lastThunk.Store(nil)
		thunkMu.Unlock()
	}()

	var trap uint32
	enterNative(1, 0, 0, uintptr(unsafe.Pointer(&trap)), 0, 1)
	if TrapCode(trap) != TrapBuiltin {
		t.Fatalf("mapping failure trap = %v, want builtin trap", TrapCode(trap))
	}

	thunkMu.Lock()
	for i := 0; i < maxTinyGoEntryThunks; i++ {
		thunkCache[uintptr(i+1)] = uintptr(i + 1)
	}
	thunkMu.Unlock()
	if entry := thunkForSlow(maxTinyGoEntryThunks + 1); entry != 0 {
		t.Fatalf("entry beyond bounded cache = %#x, want zero", entry)
	}
}

func TestTinyGoResumeThunkRetriesMappingFailure(t *testing.T) {
	resumeThunkMu.Lock()
	oldEntry, oldMapper := atomic.LoadUintptr(&resumeThunkEntry), tinygoMmapExec
	atomic.StoreUintptr(&resumeThunkEntry, 0)
	calls := 0
	backing := make([]byte, 1)
	tinygoMmapExec = func([]byte) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("mapping denied")
		}
		return backing, nil
	}
	resumeThunkMu.Unlock()
	defer func() {
		resumeThunkMu.Lock()
		atomic.StoreUintptr(&resumeThunkEntry, oldEntry)
		tinygoMmapExec = oldMapper
		resumeThunkMu.Unlock()
	}()

	if entry := resumeThunkPtr(); entry != 0 {
		t.Fatalf("first mapping attempt = %#x, want zero", entry)
	}
	if entry := resumeThunkPtr(); entry != uintptr(unsafe.Pointer(&backing[0])) {
		t.Fatalf("retry mapping = %#x, want %#x", entry, uintptr(unsafe.Pointer(&backing[0])))
	}
	if calls != 2 {
		t.Fatalf("mapping calls = %d, want 2", calls)
	}
}
