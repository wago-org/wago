package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestInstanceInvokePreparedHostCacheEviction(t *testing.T) {
	exports := make([][]byte, 5)
	for i := range exports {
		exports[i] = wasmtest.ExportEntry(fmt.Sprintf("g%d", i), 0, 1)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
	c := MustCompile(module)
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 { return v + 1 })
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := range exports {
		name := fmt.Sprintf("g%d", i)
		for range 2 {
			got, err := in.Invoke(name, I32(41))
			if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
				t.Fatalf("%s = %v, %v", name, got, err)
			}
		}
		ic := in.findInvokeCache(name)
		state := in.pluginState.Load()
		if ic == nil || state == nil || state.hostInvokeCache == nil || state.hostInvokeCache[ic.slotIndex] == nil || state.hostInvokeCache[ic.slotIndex].export != name {
			t.Fatalf("%s did not retain its own prepared host entry", name)
		}
	}
	if in.findInvokeCache("g0") != nil {
		t.Fatal("fifth export did not evict the first cache slot")
	}
	got, err := in.Invoke("g0", I32(41))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("refilled g0 = %v, %v", got, err)
	}
}

func TestInstanceInvokeConfiguredHostCacheSlots(t *testing.T) {
	exports := make([][]byte, 7)
	for i := range exports {
		exports[i] = wasmtest.ExportEntry(fmt.Sprintf("g%d", i), 0, 1)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
	c := MustCompile(module)
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 { return v + 1 })
	for _, slots := range []int{2, 6} {
		t.Run(fmt.Sprint(slots), func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{Imports: imports, InvokeCacheSlots: slots})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < slots; i++ {
				name := fmt.Sprintf("g%d", i)
				for range 2 {
					got, err := in.Invoke(name, I32(41))
					if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
						t.Fatalf("%s = %v, %v", name, got, err)
					}
				}
				ic := in.findInvokeCache(name)
				state := in.pluginState.Load()
				if ic == nil || state == nil || state.hostInvokeCache == nil || state.hostInvokeCache[ic.slotIndex] == nil {
					t.Fatalf("%s has no prepared host handle", name)
				}
			}
			if in.findInvokeCache("g0") == nil {
				t.Fatal("first export evicted before capacity was reached")
			}
			next := fmt.Sprintf("g%d", slots)
			if got, err := in.Invoke(next, I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
				t.Fatalf("%s = %v, %v", next, got, err)
			}
			if in.findInvokeCache("g0") != nil {
				t.Fatal("oldest export was not evicted at capacity")
			}
		})
	}
}

func TestInstanceInvokeCachedPreparedHostRevokedByCallbackSharing(t *testing.T) {
	c := MustCompile(sessionImportMemoryModule())
	defer c.Close()
	var in *Instance
	var identities []invocationID
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 {
		calls++
		ic := in.findInvokeCache("g")
		state := in.pluginState.Load()
		if ic == nil || state == nil || state.hostInvokeCache == nil || state.hostInvokeCache[ic.slotIndex] == nil {
			t.Error("name-based call did not cache its prepared host entry")
		} else if calls <= 2 {
			identities = append(identities, state.hostInvokeCache[ic.slotIndex].hostActivation.context(in).id)
		}
		if calls == 2 {
			if _, err := in.ExportedMemory("memory"); err != nil {
				panic(HostTrap{Err: err})
			}
		}
		return v + 1
	})
	var err error
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("g", I32(41))
		if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("call %d after sharing = %v, %v", i, got, err)
		}
	}
	if in.usesIndependentExecution() {
		t.Fatal("callback publication failed to revoke independent execution")
	}
	if len(identities) != 2 || identities[0] == 0 || identities[0] == identities[1] {
		t.Fatalf("cached callback identities = %v, want distinct nonzero IDs", identities)
	}
}

func TestInstanceInvokeCachedPreparedHostPanicReleasesGate(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	sentinel := errors.New("unexpected host panic")
	panicNext := false
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 {
		if panicNext {
			panicNext = false
			panic(sentinel)
		}
		return v + 1
	})
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("g", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("warm call = %v, %v", got, err)
	}
	panicNext = true
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = in.Invoke("g", I32(41))
	}()
	if recovered != sentinel {
		t.Fatalf("panic = %v, want %v", recovered, sentinel)
	}
	if got, err := in.Invoke("g", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("call after panic = %v, %v", got, err)
	}
}
