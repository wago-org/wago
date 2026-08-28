//go:build linux && amd64 && tinygo

package runtime

import (
	"errors"
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
