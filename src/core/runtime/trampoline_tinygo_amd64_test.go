//go:build linux && amd64 && tinygo

package runtime

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestTinyGoEntryMappingFailureReturnsError(t *testing.T) {
	oldMapper := tinygoMmapExec
	tinygoMmapExec = func([]byte) ([]byte, error) { return nil, errors.New("mapping denied") }
	defer func() { tinygoMmapExec = oldMapper }()

	engine, err := NewEngine()
	if engine != nil {
		engine.Close()
		t.Fatal("failed entry mapping returned an Engine")
	}
	if err == nil || !strings.Contains(err.Error(), "tinygo entry trampoline") {
		t.Fatalf("entry mapping error = %v", err)
	}
}

func TestTinyGoEntryMappingRetriesWithNewEngine(t *testing.T) {
	oldMapper := tinygoMmapExec
	calls := 0
	tinygoMmapExec = func(code []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("mapping denied")
		}
		return oldMapper(code)
	}
	defer func() { tinygoMmapExec = oldMapper }()

	if engine, err := NewEngine(); err == nil || engine != nil {
		if engine != nil {
			engine.Close()
		}
		t.Fatalf("first NewEngine = %v, %v; want mapping failure", engine, err)
	}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	if engine.stackTop == 0 || len(engine.preparedInt.mem) == 0 {
		t.Fatal("successful Engine did not own its entry mapping")
	}
	if calls != 2 {
		t.Fatalf("mapping calls = %d, want 2", calls)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	if engine.preparedInt.stackTop != 0 || engine.preparedInt.mem != nil {
		t.Fatal("Engine.Close retained its entry mapping")
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
