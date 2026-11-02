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
