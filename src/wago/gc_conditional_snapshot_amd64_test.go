//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"unsafe"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func conditionalSnapshotParams(n int) []wasm.ValType {
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.F64
		if i < 8 {
			params[i] = wasm.I32
		}
	}
	return params
}

// Keep a void control frame active across each conditional operation. Change
// alternate locals after frame entry, so reloading stale slots cannot pass.
func conditionalSnapshotPrefix(n int) []byte {
	body := []byte{0x02, 0x40}
	for i := 1; i < n; i += 2 {
		body = append(body, 0x20, byte(i))
		if i < 8 {
			body = append(body, 0x41, 0x2d, 0x73)
		} else {
			body = append(body, 0x9a)
		} // xor 45 / f64.neg
		body = append(body, 0x21, byte(i))
	}
	return body
}

func conditionalSnapshotResults(n int) []byte {
	body := []byte{0x0b} // end the void block before returning the locals
	for i := 0; i < n; i++ {
		body = append(body, 0x20, byte(i))
	}
	return append(body, 0x0b)
}

func conditionalSnapshotValues(n int) (args, want []uint64) {
	args, want = make([]uint64, n), make([]uint64, n)
	for i := range args {
		if i < 8 {
			args[i] = uint64(100 + i*17)
			want[i] = args[i]
			if i%2 == 1 {
				want[i] ^= 45
			}
		} else {
			// Include a signed zero and a NaN payload; exact bits must survive.
			args[i] = math.Float64bits(float64(i) + 0.125)
			if i == 8 {
				args[i] = 1 << 63
			}
			if i == 9 {
				args[i] = 0x7ff8000000001234
			}
			want[i] = args[i]
			if i%2 == 1 {
				want[i] ^= 1 << 63
			}
		}
	}
	return
}

func checkConditionalSnapshotValues(t *testing.T, got, want []uint64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("results = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("local/result %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

func checkConditionalSnapshotPins(t *testing.T, data []byte, function, want int) {
	t.Helper()
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats backend.ModuleStats
	_, err = backend.CompileModuleWith(m, backend.CompileOptions{
		Stats: &stats, GCStructHelpers: true, GCArrayHelpers: true, GCTypeSubtypingRefTest: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := stats.Funcs[function]
	if s.PinnedLocals != want || s.PinRelinquishments != 0 {
		t.Fatalf("pins = %d, relinquishments = %d; want %d, 0", s.PinnedLocals, s.PinRelinquishments, want)
	}
	t.Logf("actual pinned locals: %d", s.PinnedLocals)
}

func conditionalSnapshotStoreModule(array bool, n, repeats int) []byte {
	parentType := []byte{0x5f, 0x01, 0x6d, 0x01}
	if array {
		parentType = []byte{0x5e, 0x6d, 0x01}
	}
	childType := []byte{0x5f, 0x01, 0x7f, 0x00} // child has an immutable i32 payload
	init := []byte{}
	if array {
		init = append(init, 0x41, 0x01, 0xfb, 0x07, 0x00)
	} else {
		init = append(init, 0xfb, 0x01, 0x00)
	}
	init = append(init, 0x24, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x01, 0x24, 0x01, 0x0b)
	body := conditionalSnapshotPrefix(n)
	for j := 0; j < repeats; j++ {
		body = append(body, 0x23, 0x00)
		if array {
			body = append(body, 0x41, 0x00)
		}
		body = append(body, 0x23, 0x01)
		if array {
			body = append(body, 0xfb, 0x0e, 0x00)
		} else {
			body = append(body, 0xfb, 0x05, 0x00, 0x00)
		}
	}
	body = append(body, conditionalSnapshotResults(n)...)
	params := conditionalSnapshotParams(n)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(parentType, childType, wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, params))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("clear_child", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(body), wasmtest.Code([]byte{0xd0, 1, 0x24, 1, 0x0b}))),
	)
}

func runConditionalSnapshotStore(t *testing.T, array bool, n, repeats int, track func(*testing.T, *Instance)) {
	t.Helper()
	data := conditionalSnapshotStoreModule(array, n, repeats)
	checkConditionalSnapshotPins(t, data, 1, n)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressBarriers: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	parent := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	child := gc.Ref(uint32(readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)))
	if err := in.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	if got := in.gc.RememberedCount(); got != 0 {
		t.Fatalf("remembered parents before store = %d", got)
	}
	// Initialization leaves root cards; the first array store must add an object card.
	cards := in.gc.CardCount()
	in.SetGCHelperStatsTracking(true)
	defer in.SetGCHelperStatsTracking(false)
	args, want := conditionalSnapshotValues(n)
	got, err := in.Invoke("run", args...)
	if err != nil {
		t.Fatal(err)
	}
	checkConditionalSnapshotValues(t, got, want)
	if track != nil {
		track(t, in)
	}
	if got := in.gc.RememberedCount(); got != 1 {
		t.Fatalf("remembered parents after store = %d, want 1", got)
	}
	if array && in.gc.CardCount() != cards+1 {
		t.Fatalf("cards after store = %d, want %d", in.gc.CardCount(), cards+1)
	}
	checkChild := func() {
		t.Helper()
		var value gc.Value
		var err error
		if array {
			value, err = in.gc.ArrayGet(parent, 0)
		} else {
			value, err = in.gc.StructGet(parent, 0)
		}
		if err != nil || value.Ref != child {
			t.Fatalf("stored reference = %+v, %v; want %v", value, err, child)
		}
		payload, err := in.gc.StructGet(value.Ref, 0)
		if err != nil || payload.Bits != 42 {
			t.Fatalf("child payload = %+v, %v; want 42", payload, err)
		}
	}
	checkChild()
	if _, err := in.Invoke("clear_child"); err != nil {
		t.Fatal(err)
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	checkChild()
}

func TestGCConditionalSnapshotBoundariesAndFallback(t *testing.T) {
	for _, array := range []bool{false, true} {
		for _, n := range []int{16, 17, 19} {
			t.Run(fmt.Sprintf("array=%t/pins=%d", array, n), func(t *testing.T) { runConditionalSnapshotStore(t, array, n, 1, nil) })
		}
	}
}

func TestGCConditionalSnapshotRepeatedWideStores(t *testing.T) {
	// Bounded stress: many conditional snapshots with 19 actual live pins, not
	// merely a large declaration count. The first store takes the helper; later
	// stores reuse the remembered parent/card and take the native branch.
	for _, array := range []bool{false, true} {
		t.Run(fmt.Sprintf("array=%t", array), func(t *testing.T) { runConditionalSnapshotStore(t, array, 19, 64, nil) })
	}
}

func conditionalSnapshotFunctionModule(n int) []byte {
	params := conditionalSnapshotParams(n)
	body := conditionalSnapshotPrefix(n)
	body = append(body, 0x20, 0, 0x25, 0, 0xfb, 0x14, 0, 0x24, 0) // table.get; ref.test 0; save result
	body = append(body, conditionalSnapshotResults(n)...)
	// Return the test result after the independently observable numeric locals.
	body = append(body[:len(body)-1], 0x23, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, append(append([]wasm.ValType{}, params...), wasm.I32)))),
		wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "f", 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 4})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 0, 0x0b})),
		// Exporting the table prevents immutable-table classification.
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3), wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x0b}), wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))),
	)
}

func TestDynamicFunctionConditionalSnapshot(t *testing.T) {
	for _, n := range []int{16, 17, 19} {
		t.Run(fmt.Sprintf("pins=%d", n), func(t *testing.T) {
			data := conditionalSnapshotFunctionModule(n)
			checkConditionalSnapshotPins(t, data, 2, n)
			providerCode, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructImportedFuncrefProviderModule())
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			provider, err := Instantiate(providerCode, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			if len(provider.funcRefDescs) != 0 {
				t.Fatal("provider must use the bare-function attachment fallback")
			}
			export, err := provider.ExportedFunc("f")
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if !compiled.usesDynamicFuncRefTest() {
				t.Fatal("ref.test was classified statically")
			}
			in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.f": export}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			// A bare provider's proxy has the unknown compact type ID. This is an
			// explicit native-branch condition for the full-metadata Go helper.
			ptr := binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.TableEntryCodePtrOffset:])
			ids := unsafe.Slice((*byte)(offHeapPtr(uintptr(ptr))), 4*len(compiled.FuncTypeID))
			if got := binary.LittleEndian.Uint32(ids); got != ^uint32(0) {
				t.Fatalf("import type ID = %d, want fallback sentinel", got)
			}
			for index, result := range []uint64{1, 1, 0, 0} {
				t.Run(fmt.Sprintf("table=%d", index), func(t *testing.T) {
					args, want := conditionalSnapshotValues(n)
					args[0], want[0] = uint64(index), uint64(index)
					got, err := in.Invoke("run", args...)
					if err != nil {
						t.Fatal(err)
					}
					checkConditionalSnapshotValues(t, got, append(want, result))
				})
			}
		})
	}
}
