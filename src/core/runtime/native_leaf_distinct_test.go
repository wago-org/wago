//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/internal/runtimebridge"
	"testing"
)

//go:noinline
//go:norace
func distinctLeafOne(v int32) int32 { return v + 19 }

//go:noinline
//go:norace
func distinctLeafTwo(v int32) int32 { return v - 11 }

//go:noinline
//go:norace
func distinctLeafPair(a, b int32) int32 { return a ^ b }

func TestNativeLeafDistinctAcquisitionRollback(t *testing.T) {
	callbacks := []any{distinctLeafOne, distinctLeafTwo, distinctLeafPair}
	params := []uint32{1, 1, 2}
	for _, mode := range []string{"reject", "error", "no-direct"} {
		for fail := 0; fail < len(callbacks); fail++ {
			t.Run(fmt.Sprintf("%s/%d", mode, fail), func(t *testing.T) {
				ctrl := make([]byte, ctrlFrameSize)
				defer unregisterNativeScalarLeaf(ctrl)
				nativeLeafCodeCache.Lock()
				before := len(nativeLeafCodeCache.entries)
				nativeLeafCodeCache.Unlock()
				index := 0
				injected := errors.New("injected acquisition error")
				build := func(fn any, p uint32) (nativeScalarLeaf, bool, error) {
					i := index
					index++
					if i == fail && mode == "reject" {
						return nativeScalarLeaf{}, false, nil
					}
					if i == fail && mode == "error" {
						return nativeScalarLeaf{}, false, injected
					}
					leaf, ok, err := buildNativeScalarLeaf(fn, p)
					if i == fail && mode == "no-direct" {
						leaf.direct = 0
					}
					return leaf, ok, err
				}
				ok, err := registerNativeScalarLeafImports(ctrl, callbacks, params, build)
				if ok || (mode == "error" && !errors.Is(err, injected)) || (mode != "error" && err != nil) {
					t.Fatalf("register=%t,%v", ok, err)
				}
				if _, exists := nativeLeafHosts.Load(slicePtr(ctrl)); exists {
					t.Fatal("provisional registration published")
				}
				nativeLeafCodeCache.Lock()
				after := len(nativeLeafCodeCache.entries)
				nativeLeafCodeCache.Unlock()
				if after != before {
					t.Fatalf("cache leaked after acquisition %d: %d -> %d", fail, before, after)
				}
			})
		}
	}
}

func TestNativeLeafDistinctIndexedOwnership(t *testing.T) {
	access := runtimebridge.GrantHostScalarCall()
	callbacks := []any{distinctLeafOne, distinctLeafPair, distinctLeafOne, distinctLeafTwo}
	params := []uint32{1, 2, 1, 1}
	controls := [][]byte{make([]byte, ctrlFrameSize), make([]byte, ctrlFrameSize)}
	var retained nativeScalarLeafRegistration
	for _, ctrl := range controls {
		ok, err := RegisterNativeScalarLeafImports(access, ctrl, callbacks, params)
		if err != nil || !ok {
			t.Fatalf("register=%t,%v", ok, err)
		}
		defer unregisterNativeScalarLeaf(ctrl)
		if nativeScalarLeafPtr(ctrl) != 0 {
			t.Fatal("distinct registration selected a legacy trampoline")
		}
		if err := InitHostCtrlFrame(ctrl); err != nil {
			t.Fatal(err)
		}
		generic, err := HostCallTrampolinePointer()
		if err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint64(ctrl[hcTrampoline:]) != uint64(generic) {
			t.Fatal("distinct trampoline is not generic")
		}
		value, _ := nativeLeafHosts.Load(slicePtr(ctrl))
		registration := value.(nativeScalarLeafRegistration)
		if retained.imports != nil {
			for i, leaf := range registration.imports {
				if leaf.owner != retained.imports[i].owner {
					t.Fatal("code not shared per binding")
				}
			}
		}
		retained = registration
		for i, leaf := range registration.imports {
			cell := make([]byte, ImportDispatchEntryBytes)
			if !BindNativeScalarLeafImportAt(access, ctrl, cell, i) || binary.LittleEndian.Uint64(cell) != uint64(leaf.direct) {
				t.Fatalf("wrong indexed target %d", i)
			}
		}
		if BindNativeScalarLeafImportAt(access, ctrl, make([]byte, ImportDispatchEntryBytes), len(callbacks)) {
			t.Fatal("out of range index admitted")
		}
	}
	if retained.imports[0].owner != retained.imports[2].owner {
		t.Fatal("aliases did not share code")
	}
	unregisterNativeScalarLeaf(controls[0])
	nativeLeafCodeCache.Lock()
	for i, leaf := range retained.imports {
		want := uint64(1)
		if i == 0 || i == 2 {
			want = 2
		}
		if leaf.owner.refs != want {
			t.Errorf("binding %d refs=%d want=%d", i, leaf.owner.refs, want)
		}
	}
	nativeLeafCodeCache.Unlock()
	unregisterNativeScalarLeaf(controls[1])
	nativeLeafCodeCache.Lock()
	defer nativeLeafCodeCache.Unlock()
	for _, leaf := range retained.imports {
		if leaf.owner.refs != 0 || nativeLeafCodeCache.entries[leaf.owner.key] != nil {
			t.Error("final close retained code")
		}
	}
}

// Enter each acquired wrapper with raw u64 argument/result homes. Unlike public
// Invoke, this supplies poisoned i32 high bits without guest normalization.
func TestNativeLeafDistinctRawHomes(t *testing.T) {
	eng, jm, arena := fixture(t)
	args, results, trap, ctrl := hostCallFixture(t, jm, arena)
	callbacks := []any{distinctLeafOne, distinctLeafPair, distinctLeafTwo}
	params := []uint32{1, 2, 1}
	access := runtimebridge.GrantHostScalarCall()
	ok, err := RegisterNativeScalarLeafImports(access, ctrl, callbacks, params)
	if err != nil || !ok {
		t.Fatalf("register=%t,%v", ok, err)
	}
	defer unregisterNativeScalarLeaf(ctrl)
	for index := range callbacks {
		cell := make([]byte, ImportDispatchEntryBytes)
		if !BindNativeScalarLeafImportAt(access, ctrl, cell, index) {
			t.Fatalf("binding %d", index)
		}
		target := uintptr(binary.LittleEndian.Uint64(cell))
		for _, overlap := range []bool{false, true} {
			for _, a := range []uint64{0xfeedface00000000, 0x12345678ffffffff, 0xdeadbeef80000000, 0xffffffff7fffffff} {
				b := uint64(0xaabbccddfffffffd)
				binary.LittleEndian.PutUint64(args, a)
				binary.LittleEndian.PutUint64(args[8:], b)
				output := results
				if overlap {
					output = args
				} else {
					binary.LittleEndian.PutUint64(output, ^uint64(0))
					binary.LittleEndian.PutUint64(output[8:], b)
				}
				var want int32
				switch index {
				case 0:
					want = distinctLeafOne(int32(a))
				case 1:
					want = distinctLeafPair(int32(a), int32(uint32(b)))
				case 2:
					want = distinctLeafTwo(int32(a))
				}
				if err = eng.Call(target, args, jm.LinearMemory(), trap, output); err != nil {
					t.Fatal(err)
				}
				if got := binary.LittleEndian.Uint64(output); got != uint64(uint32(want)) {
					t.Fatalf("binding=%d overlap=%t raw=%x want=%x", index, overlap, got, uint32(want))
				}
				if binary.LittleEndian.Uint64(output[8:]) != b {
					t.Fatal("wrapper overwrote adjacent home")
				}
			}
		}
	}
}
