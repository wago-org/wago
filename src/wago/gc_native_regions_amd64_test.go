//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func nativeRegionState(in *Instance, offset int) uint32 {
	state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().StructAllocState)), gc.NativeStructAllocStateSize)
	return binary.LittleEndian.Uint32(state[offset:])
}

// A persistent child and two live reference-bearing parents force initialized
// reference publication. The optional trap follows allocation, not a dead site.
func nativeRegionTrapModule() []byte {
	types := wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5f, 1, 0x64, 0, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	global := []byte{0x64, 0, 1, 0x41, 42, 0xfb, 0, 0, 0x0b}
	body := []byte{0, 0x23, 0, 0xfb, 0, 1, 0xd1, 0x1a, 0x23, 0, 0xfb, 0, 1, 0xfb, 2, 1, 0, 0xfb, 2, 0, 0,
		0x20, 0, 0x04, 0x40, 0x00, 0x0b, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, wasmtest.Vec([]byte{2})), wasmtest.Section(6, wasmtest.Vec(global)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCNativeRegionsStructTrapReuse(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", true).WithOptimization("dead-gc-new", false).WithOptimization("gc-lifetime-analyze", false), nativeRegionTrapModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 256, NurseryRegionShift: 6, SurvivorBytes: 256, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var nativePublications uint32
	for i := 0; i < 200; i++ {
		if got, err := in.Invoke("run", 0); err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("run %d: %v %v", i, got, err)
		}
		nativePublications += nativeRegionState(in, gc.NativeStructAllocCursorOffset)
		if start, end := nativeRegionState(in, gc.NativeStructAllocChunkStartOffset), nativeRegionState(in, gc.NativeStructAllocChunkEndOffset); start/64 != (end-1)/64 {
			t.Fatal("reservation crossed region")
		}
		epoch := nativeRegionState(in, gc.NativeStructAllocEpochOffset)
		if _, err := in.Invoke("run", 1); err == nil {
			t.Fatal("missing guest trap")
		}
		if nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 || nativeRegionState(in, gc.NativeStructAllocEpochOffset) == epoch {
			t.Fatal("trap retained reservation")
		}
	}
	if nativePublications == 0 {
		t.Fatal("no native struct publication observed")
	}
	if in.gc.Stats().FullCollections == 0 || in.gc.Stats().Allocations < 800 {
		t.Fatalf("missing reuse/counters: %+v", in.gc.Stats())
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

func TestGCNativeRegionsArraysAndMixedCancellation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		module      []byte
		arg         []uint64
		want        uint64
		allocations uint64
	}{
		{"arrays", gcNativeDynamicDefaultArrayModule(12), []uint64{4}, 4, 12},
		{"mixed", gcNativeMixedReservationModule(false), nil, 0, 11},
		{"checked-large-cancellation", gcNativeMixedReservationModule(true), nil, 0, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", true).WithOptimization("dead-gc-new", false).WithOptimization("gc-lifetime-analyze", false), tc.module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 2048, NurseryRegionShift: 10, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 100; i++ {
				before := in.gc.Stats().Allocations
				got, err := in.Invoke("run", tc.arg...)
				if err != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("run %d: %v %v", i, got, err)
				}
				if in.gc.Stats().Allocations-before != tc.allocations {
					t.Fatalf("wrong charge: %+v", in.gc.Stats())
				}
				if tc.name != "checked-large-cancellation" {
					if nativeRegionState(in, gc.NativeStructAllocCursorOffset) == 0 {
						t.Fatal("no native publication")
					}
					beforeCancel := in.gc.Stats().Allocations
					in.gc.CancelNativeAllocationBatch()
					if in.gc.Stats().Allocations != beforeCancel || nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 {
						t.Fatal("partial cancellation lost/doubled charge")
					}
				} else if nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 {
					t.Fatal("large fallback retained small reservation")
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGCNativeRegionsArrayThresholdFallback(t *testing.T) {
	// Prime a chunk with 24-byte arrays, then request a 32-byte large object.
	body := []byte{0}
	for range 9 {
		body = append(body, 0x41, 1, 0xfb, 7, 0, 0x24, 0)
	}
	body = append(body, 0x41, 4, 0xfb, 7, 0, 0x24, 0, 0x23, 0, 0xfb, 0x0f, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 2048, NurseryRegionShift: 10, LargeObjectBytes: 32, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 4 {
		t.Fatalf("run: %v %v", got, err)
	}
	if nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 {
		t.Fatal("large array consumed nursery chunk")
	}
	if in.gc.Stats().Allocations != 10 {
		t.Fatal("wrong allocation charges")
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

func TestGCNativeRegionsReentry(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("regionShift=%d", shift), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcHostReentryFrameRootModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var in *Instance
			active, calls := 0, 0
			var sawNative bool
			trap := false
			host := CallerHostCallFunc(func(caller Caller, call HostCall) {
				sawNative = sawNative || nativeRegionState(in, gc.NativeStructAllocCursorOffset) > 0
				active++
				calls++
				defer func() { active-- }()
				if active == 3 {
					if trap {
						panic(HostTrap{Err: errors.New("region integration trap")})
					}
					got, err := in.InvokeFromHost(context.Background(), caller, "inner")
					if err != nil || len(got) != 1 || got[0] != 0 {
						panic(fmt.Sprintf("inner=%v, %v", got, err))
					}
				} else {
					want := uint64(active)
					got, err := in.InvokeFromHost(context.Background(), caller, "outer", want)
					if err != nil {
						panic(HostTrap{Err: err})
					}
					if len(got) != 1 || got[0] != want {
						panic(fmt.Sprintf("nested outer=%v", got))
					}
				}
				call.ResultSlots()[0] = 0
			})
			in, err = Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, NurseryRegionShift: shift, VerifyAfterCollect: true}, Imports: testImports("env.reenter", host)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, fail := range []bool{false, true, false} {
				trap = fail
				calls = 0
				got, err := in.Invoke("outer", 91)
				if fail {
					if err == nil || !strings.Contains(err.Error(), "region integration trap") {
						t.Fatalf("trap=%v", err)
					}
				} else if err != nil || len(got) != 1 || got[0] != 91 {
					t.Fatalf("outer=%v, %v", got, err)
				}
				if active != 0 || calls != 3 {
					t.Fatalf("active=%d calls=%d", active, calls)
				}
				state := in.publicGCState()
				state.mu.Lock()
				pending := state.hostActivationCount
				state.mu.Unlock()
				if pending != 0 {
					t.Fatalf("leaked suspended activations: %d", pending)
				}
				assertHostReentryDepthsEmpty(t)
			}
			if !sawNative {
				t.Fatal("reentry never consumed native reservation")
			}
			if in.gc.Stats().Allocations < 2000 {
				t.Fatal("missing allocation pressure")
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
