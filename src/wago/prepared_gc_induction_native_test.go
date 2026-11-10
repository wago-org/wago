//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func TestGCDeadInductionEntry(t *testing.T) {
	if codeProfileEnabled || !gc.NativeDeadBatchSupported {
		t.Skip("private induction capability unavailable")
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"struct", gcStructReductionBytes(false, false)},
		{"array", gcArrayReductionBytes(false)},
		{"fill", gcArrayFillReductionBytes(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), tc.data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if fn.gcDeadInductionLayout == 0 {
				t.Fatal("compiler induction proof missing")
			}
			if _, err := fn.Invoke(1); err != nil {
				t.Fatal(err)
			}
			state := fn.tryBeginPrivateGCState()
			if state == nil {
				t.Fatal("private gate unavailable")
			}
			defer func() {
				if state != nil {
					in.endPrivateGCInvocation(state)
				}
			}()

			before := in.gc.Stats()
			if in.gc.TryReserveDeadAllocationBatch(0xffffffff, uint32(fn.gcDeadInductionLayout), uint32(fn.gcDeadInductionLayout>>32)) {
				t.Fatal("large reservation unexpectedly succeeded")
			}
			if after := in.gc.Stats(); after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
				t.Fatalf("failed reservation mutated state: %+v -> %+v", before, after)
			}
			in.endPrivateGCInvocation(state)
			state = nil
			for _, n := range []uint64{0, 1, 7, 0xffffffff00000003} {
				count := uint32(n)
				if count == 0 {
					count = 1
				}
				before := in.gc.Stats()
				value, err := fn.Invoke(n)
				if in.pluginState.Load().invocationID != 0 {
					t.Fatal("Go charge published invocation identity")
				}
				x := uint64(uint32(n))
				want := uint64(uint32(x * (x - 1) / 2))
				if err != nil || len(value) != 1 || value[0] != want {
					t.Fatalf("n=%x result=%v error=%v", n, value, err)
				}
				if got := in.gc.Stats().Allocations - before.Allocations; got != uint64(count) {
					t.Fatalf("allocation delta=%d want=%d", got, count)
				}
			}
			state = fn.tryBeginPrivateGCState()
			if state == nil {
				t.Fatal("private gate unavailable after calls")
			}
			in.gc.CancelNativeAllocationBatch()
			before = in.gc.Stats()
			if in.gc.TryReserveDeadAllocationBatch(1, uint32(fn.gcDeadInductionLayout), uint32(fn.gcDeadInductionLayout>>32)) {
				t.Fatal("cancelled reservation unexpectedly succeeded")
			}
			if after := in.gc.Stats(); after.Allocations != before.Allocations {
				t.Fatal("retry charged allocation")
			}
			in.endPrivateGCInvocation(state)
			state = nil
			if _, err := fn.Invoke(1); err != nil {
				t.Fatal(err)
			}
			if in.pluginState.Load().invocationID != 0 {
				t.Fatal("fallback retained invocation identity")
			}
			if got := in.gc.Stats().Allocations - before.Allocations; got != 1 {
				t.Fatalf("fallback allocations=%d", got)
			}
		})
	}
}

func TestGCDeadInductionWrapperParity(t *testing.T) {
	if codeProfileEnabled || !gc.NativeDeadBatchSupported {
		t.Skip("private induction capability unavailable")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayReductionBytes(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{{}, {StressNurseryBytes: 256, VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 256}, {Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}} {
		a, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		b, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		af, _ := a.WasmFunc("run")
		bf, _ := b.WasmFunc("run")
		if af.gcDeadInductionLayout == 0 {
			t.Fatal("missing induction proof")
		}
		bf.gcDeadInductionLayout = 0
		for i := 0; i < 30; i++ {
			for _, n := range []uint64{0, 1, 7, 64, 4096, 3} {
				got, ge := af.Invoke(n)
				want, we := bf.Invoke(n)
				if (ge == nil) != (we == nil) {
					t.Fatalf("error mismatch %v / %v", ge, we)
				}
				if ge == nil && got[0] != want[0] {
					t.Fatalf("result mismatch %v / %v", got, want)
				}
				x, y := a.gc.Stats(), b.gc.Stats()
				if x.Allocations != y.Allocations || x.LiveObjects != y.LiveObjects || x.MinorCollections != y.MinorCollections || x.FullCollections != y.FullCollections {
					t.Fatalf("collector mismatch %+v / %+v", x, y)
				}
			}
		}
	}

}

func TestGCDeadInductionParallelDirectory(t *testing.T) {
	if codeProfileEnabled || !gc.NativeDeadBatchSupported {
		t.Skip("private induction capability unavailable")
	}
	m, err := wasm.DecodeModule(gcArrayReductionBytes(false))
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte{1, 2, 0x7f}, m.Code[0].BodyBytes...)
	var funcs, codes, exports [][]byte
	for i := 0; i < 8; i++ {
		funcs = append(funcs, []byte{1})
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("run%d", i), 0, uint32(i)))
	}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(codes...)))
	for _, workers := range []int{1, 4} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithFunctionWorkers(workers), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		var entry uint64
		for i := 0; i < 8; i++ {
			fn, err := in.WasmFunc(fmt.Sprintf("run%d", i))
			if err != nil {
				t.Fatal(err)
			}
			if fn.gcDeadInductionLayout == 0 {
				t.Fatalf("workers=%d function=%d missing induction proof", workers, i)
			}
			if i > 0 && fn.gcDeadInductionLayout != entry {
				t.Fatal("identical functions produced different induction layouts")
			}
			entry = fn.gcDeadInductionLayout
			for j := 0; j < 3; j++ {
				out, err := fn.Invoke(7)
				if err != nil || out[0] != 21 {
					t.Fatalf("result=%v error=%v", out, err)
				}
			}
		}
	}
}

func TestGCDeadInductionRollback(t *testing.T) {
	for _, knob := range []string{"reg-abi", "prepared-direct-entry", "prepared-bounded-entry"} {
		t.Run(knob, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization(knob, false), gcArrayReductionBytes(false))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if fn.gcDeadInductionLayout != 0 {
				t.Fatal("disabled optimization retained induction proof")
			}
			for range 3 {
				out, err := fn.Invoke(7)
				if err != nil || len(out) != 1 || out[0] != 21 {
					t.Fatalf("fallback: %v, %v", out, err)
				}
			}
		})
	}
}

func TestGCDeadInductionInterruptedBeforeCharge(t *testing.T) {
	if codeProfileEnabled || !gc.NativeDeadBatchSupported {
		t.Skip("private induction capability unavailable")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if fn.gcDeadInductionLayout == 0 {
		t.Fatal("missing induction proof")
	}
	if _, err := fn.Invoke(1); err != nil {
		t.Fatal(err)
	}
	before := in.gc.Stats()
	rt.RequestInterrupt(in.trap)
	_, err = fn.Invoke(7)
	requireInterruptedTrap(t, err)
	if after := in.gc.Stats(); after != before {
		t.Fatalf("interrupted call charged allocation: %+v -> %+v", before, after)
	}
	if in.invocationState.word.Load() != 0 || in.ensurePluginState().invocationID != 0 {
		t.Fatal("interrupted call retained admission")
	}
	if got, err := fn.Invoke(7); err != nil || got[0] != 21 {
		t.Fatalf("recovery: %v %v", got, err)
	}
}

func TestGCDeadInductionWithoutCachedNativeContext(t *testing.T) {
	if codeProfileEnabled || !gc.NativeDeadBatchSupported {
		t.Skip("private induction capability unavailable")
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayReductionBytes(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	warm, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := warm.Invoke(1); err != nil {
		t.Fatal(err)
	}
	cold, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if cold.hostPrepared != nil {
		t.Fatal("new handle unexpectedly has native context")
	}
	if got, err := cold.Invoke(7); err != nil || got[0] != 21 {
		t.Fatalf("Go-only charge: %v %v", got, err)
	}
	if cold.hostPrepared != nil {
		t.Fatal("Go-only charge prepared unused native context")
	}
	state := cold.tryBeginPrivateGCState()
	if state == nil {
		t.Fatal("private gate unavailable")
	}
	in.gc.CancelNativeAllocationBatch()
	in.endPrivateGCInvocation(state)
	if got, err := cold.Invoke(7); err != nil || got[0] != 21 {
		t.Fatalf("cold native fallback: %v %v", got, err)
	}
	if cold.hostPrepared == nil {
		t.Fatal("fallback did not prepare native context")
	}
}

func TestGCDeadInductionPreparesOnlyOneIdentity(t *testing.T) {
	if codeProfileEnabled || !gc.NativeDeadBatchSupported || !nativeStructAllocEnabled {
		t.Skip("native dead batches unavailable")
	}
	for _, data := range [][]byte{gcStructReductionBytes(false, false), gcArrayReductionBytes(false), gcArrayFillReductionBytes(false)} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fn.Invoke(1); err != nil {
			t.Fatal(err)
		}
		state := in.gc.NativeView().StructAllocState
		got := binary.LittleEndian.Uint32(unsafe.Slice((*byte)(offHeapPtr(state+gc.NativeStructAllocCountOffset)), 4))
		if got != 1 {
			t.Fatalf("dead batch reserved %d identities, want one cancellation/policy witness", got)
		}
		in.Close()
		c.Close()
	}
}
