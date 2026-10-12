//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNativeTypedStructAllocModule() []byte {
	types := wasmtest.Vec(
		[]byte{0x5f, 0x01, 0x7f, 0x00},       // type 0: final (struct i32)
		[]byte{0x5f, 0x01, 0x64, 0x00, 0x00}, // type 1: final (struct (ref 0))
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	)
	global := []byte{0x64, 0x00, 0x01, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}
	body := []byte{0x00,
		0x23, 0x00, 0xfb, 0x00, 0x01, 0xd1, 0x1a, // first typed parent is used
		0x23, 0x00, 0xfb, 0x00, 0x01, // second typed parent
		0xfb, 0x02, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCNativeTypedStructAllocSurvivesCollection(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeTypedStructAllocModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		cfg  GCConfig
	}{
		{name: "native-batch"},
		{name: "collection", cfg: GCConfig{StressNurseryBytes: 96, VerifyAfterCollect: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 100; i++ {
				if got, err := in.Invoke("run"); err != nil || !reflect.DeepEqual(got, []uint64{42}) {
					t.Fatalf("iteration %d: run = %v, %v; want [42]", i, got, err)
				}
			}
			if tc.name == "collection" && in.gc.Stats().MinorCollections == 0 {
				t.Fatal("stress collection did not run")
			}
		})
	}
}

func gcNativeOpenTypedStructAllocModule() []byte {
	types := wasmtest.Vec(
		[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},       // type 0: open (struct i32)
		[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x00}, // type 1: final <: type 0
		[]byte{0x5f, 0x01, 0x63, 0x00, 0x00},             // type 2: final (struct (ref null 0))
		[]byte{0x5f, 0x01, 0x7f, 0x00},                   // type 3: unrelated final struct
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	)
	childGlobal := []byte{0x64, 0x01, 0x01, 0x41, 0x2a, 0xfb, 0x00, 0x01, 0x0b}
	unrelatedGlobal := []byte{0x64, 0x03, 0x01, 0x41, 0x63, 0xfb, 0x00, 0x03, 0x0b}
	run := []byte{0x00,
		0x23, 0x00, 0xfb, 0x00, 0x02, 0xd1, 0x1a,
		0x23, 0x00, 0xfb, 0x00, 0x02,
		0xfb, 0x02, 0x02, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	}
	nullRun := []byte{0x00,
		0xd0, 0x00, 0xfb, 0x00, 0x02, 0xd1, 0x1a,
		0xd0, 0x00, 0xfb, 0x00, 0x02,
		0xfb, 0x02, 0x02, 0x00, 0xd1, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4), wasmtest.ULEB(4))),
		wasmtest.Section(6, wasmtest.Vec(childGlobal, unrelatedGlobal)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("null_run", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(run))), run...),
			append(wasmtest.ULEB(uint32(len(nullRun))), nullRun...),
		)),
	)
}

func TestGCNativeOpenTypedStructAllocChecksSubtypeAndNullAcrossCollection(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOpenTypedStructAllocModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		cfg  GCConfig
	}{
		{name: "native-batch"},
		{name: "collection", cfg: GCConfig{StressNurseryBytes: 96, VerifyAfterCollect: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 100; i++ {
				if got, err := in.Invoke("run"); err != nil || !reflect.DeepEqual(got, []uint64{42}) {
					t.Fatalf("iteration %d: subtype run = %v, %v; want [42]", i, got, err)
				}
				if got, err := in.Invoke("null_run"); err != nil || !reflect.DeepEqual(got, []uint64{1}) {
					t.Fatalf("iteration %d: null run = %v, %v; want [1]", i, got, err)
				}
			}
			if tc.name == "collection" && in.gc.Stats().MinorCollections == 0 {
				t.Fatal("stress collection did not run")
			}
			if tc.name == "native-batch" {
				original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
				unrelated := readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)
				writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, unrelated)
				_, err := in.Invoke("run")
				writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
				if err == nil {
					t.Fatal("checked native constructor accepted an unrelated collector type")
				}
				stale := uint64(in.gc.NativeView().HandleCount) << 1
				writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, stale)
				_, err = in.Invoke("run")
				writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
				if err == nil {
					t.Fatal("checked native constructor accepted an out-of-range handle")
				}
			}
		})
	}
}

func gcNativeOpenStructGetModule() []byte {
	types := wasmtest.Vec(
		[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},             // 0: open (struct i32)
		[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x00},       // 1: final <: 0
		[]byte{0x5f, 0x01, 0x7f, 0x00},                         // 2: unrelated final struct
		[]byte{0x50, 0x00, 0x5f, 0x01, 0x78, 0x00},             // 3: open (struct i8)
		[]byte{0x4f, 0x01, 0x03, 0x5f, 0x01, 0x78, 0x00},       // 4: final <: 3
		[]byte{0x50, 0x00, 0x5f, 0x01, 0x63, 0x00, 0x00},       // 5: open (struct (ref null 0))
		[]byte{0x4f, 0x01, 0x05, 0x5f, 0x01, 0x63, 0x00, 0x00}, // 6: final <: 5
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, nil),
	)
	getI32 := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	getI8 := []byte{0x00, 0x23, 0x01, 0xfb, 0x03, 0x03, 0x00, 0x0b}
	getRef := []byte{0x00, 0x23, 0x02, 0xfb, 0x02, 0x05, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	churn := []byte{0x00, 0x41, 0x03, 0xfb, 0x00, 0x01, 0x1a, 0x41, 0x08, 0xfb, 0x00, 0x02, 0x1a, 0x0b}
	getRefAfterChurn := []byte{0x00, 0x23, 0x02, 0xfb, 0x02, 0x05, 0x00, 0x10, 0x03, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(7), wasmtest.ULEB(7), wasmtest.ULEB(7), wasmtest.ULEB(8), wasmtest.ULEB(7))),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{0x63, 0x00, 0x01, 0x41, 0x2a, 0xfb, 0x00, 0x01, 0x0b},
			[]byte{0x63, 0x03, 0x01, 0x41, 0x7f, 0xfb, 0x00, 0x04, 0x0b},
			[]byte{0x63, 0x05, 0x01, 0x41, 0x2a, 0xfb, 0x00, 0x01, 0xfb, 0x00, 0x06, 0x0b},
			[]byte{0x63, 0x02, 0x01, 0x41, 0x07, 0xfb, 0x00, 0x02, 0x0b},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get_i32", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("get_i8", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("get_ref", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("churn", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("get_ref_after_churn", byte(wasm.ExternFunc), 4),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(getI32))), getI32...),
			append(wasmtest.ULEB(uint32(len(getI8))), getI8...),
			append(wasmtest.ULEB(uint32(len(getRef))), getRef...),
			append(wasmtest.ULEB(uint32(len(churn))), churn...),
			append(wasmtest.ULEB(uint32(len(getRefAfterChurn))), getRefAfterChurn...),
		)),
	)
}

func TestGCNativeOpenStructGetChecksSubtypeAndSurvivesCollection(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOpenStructGetModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		cfg  GCConfig
	}{
		{name: "normal"},
		{name: "collection", cfg: GCConfig{StressNurseryBytes: 96, VerifyAfterCollect: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 100; i++ {
				for _, check := range []struct {
					name string
					want uint64
				}{{"get_i32", 42}, {"get_i8", uint64(^uint32(0))}, {"get_ref", 42}, {"get_ref_after_churn", 42}} {
					if got, err := in.Invoke(check.name); err != nil || !reflect.DeepEqual(got, []uint64{check.want}) {
						t.Fatalf("iteration %d %s = %v, %v; want %d", i, check.name, got, err, check.want)
					}
				}
				if _, err := in.Invoke("churn"); err != nil {
					t.Fatalf("iteration %d churn: %v", i, err)
				}
			}
			if tc.name == "collection" && in.gc.Stats().MinorCollections == 0 {
				t.Fatal("stress collection did not run")
			}
			if tc.name == "normal" {
				original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
				unrelated := readGlobalObject(in.globalCells[3], in.c.Globals[3].Type)
				for _, bad := range []struct {
					name  string
					value uint64
				}{{"wrong type", unrelated}, {"null", 0}, {"out-of-range handle", uint64(in.gc.NativeView().HandleCount) << 1}} {
					writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, bad.value)
					_, err := in.Invoke("get_i32")
					writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original)
					if err == nil {
						t.Fatalf("open struct.get accepted %s", bad.name)
					}
				}
			}
		})
	}
}

func TestGCNativeStructAllocPreparedAcrossCollections(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	var ordinary corergc.Stats
	for _, prepared := range []bool{false, true} {
		in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 128, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		call := func(args ...uint64) ([]uint64, error) { return in.Invoke("new_get", args...) }
		if prepared {
			fn, err := in.WasmFunc("new_get")
			if err != nil {
				t.Fatal(err)
			}
			call = fn.Invoke
		}
		lo, hi := uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
		for i := 0; i < 2000; i++ {
			got, err := call(lo+uint64(i), hi^uint64(i))
			want := []uint64{lo + uint64(i), hi ^ uint64(i)}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("prepared=%v iteration %d = %#x, %v; want %#x", prepared, i, got, err, want)
			}
		}
		stats := in.gc.Stats()
		if stats.Allocations != 2001 || stats.MinorCollections == 0 || stats.FullCollections != 0 {
			t.Fatalf("prepared=%v collector stats = %+v, want one global plus 2000 call allocations across policy-driven minor collections", prepared, stats)
		}
		if prepared && stats != ordinary {
			t.Fatalf("prepared stats = %+v, ordinary = %+v", stats, ordinary)
		}
		ordinary = stats
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
		if live := in.gc.Stats().LiveObjects; live != 1 {
			t.Fatalf("prepared=%v retained %d objects after collection, want one global", prepared, live)
		}
	}
}

func TestGCNativeStructAllocMalformedMetadataFallsBack(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("new_get")
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := uint64(0x1111222233334444), uint64(0xaaaabbbbccccdddd)
	if got, err := fn.Invoke(lo, hi); err != nil || !reflect.DeepEqual(got, []uint64{lo, hi}) {
		t.Fatalf("initial constructor = %#x, %v", got, err)
	}
	view := in.gc.NativeView()
	state := view.StructAllocState
	view.StructAllocState = 0
	if got, err := fn.Invoke(lo+1, hi+1); err != nil || !reflect.DeepEqual(got, []uint64{lo + 1, hi + 1}) {
		t.Fatalf("nil allocation-state fallback = %#x, %v", got, err)
	}
	view.StructAllocState = state
	stride := view.HandleStride
	view.HandleStride++
	got, invokeErr := fn.Invoke(lo+2, hi+2)
	if nativeGCEntryValidationEnabled {
		if invokeErr == nil {
			t.Fatalf("wagodebug accepted mutated immutable handle stride: %#x", got)
		}
	} else if invokeErr != nil || !reflect.DeepEqual(got, []uint64{lo + 2, hi + 2}) {
		t.Fatalf("trusted immutable handle stride hot path = %#x, %v", got, invokeErr)
	}
	view.HandleStride = stride
}

func TestGCNativeStructAllocReferenceFieldsRemainRooted(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldStructReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 256, StressBarriers: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	parent := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if err := in.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	fn, err := in.WasmFunc("set_child")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		if _, err := fn.Invoke(); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	value, err := in.gc.StructGet(parent, 0)
	if err != nil || !value.Ref.IsObj() {
		t.Fatalf("stored child after repeated native allocations = %+v, %v", value, err)
	}
}
