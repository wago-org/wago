//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"reflect"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcSingleFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01} // (struct (field (mut v128)))
	vec := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	body := append([]byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f, 0xfd, 0x0c}, vec...) // ref local 1, i32 local 2; v128.const
	body = append(body,
		0xfb, 0x00, 0x00, 0x21, 0x01, // struct.new 0; local.set 1
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x02, 0x20, 0x00, 0x4f, 0x0d, 0x01, // counter >= n => break block
		0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, // counter++
		0x0c, 0x00, // br loop
		0x0b, 0x0b, // end loop, block
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, // local.get 1; struct.get 0 0
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcHiddenOperandRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01}
	vec := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	body := append([]byte{0xfd, 0x0c}, vec...)
	body = append(body,
		0xfb, 0x00, 0x00, // struct.new 0; first ref remains on the operand stack
		0x02, 0x40, 0x0b, // preserve the hidden root across a control merge
		0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
		0xfb, 0x02, 0x00, 0x00, 0x0b, // struct.get 0 0
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func gcMutableGlobalFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b} // (mut (ref null 0)) = ref.null 0
	body := []byte{0x01, 0x01, 0x7f,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCMutableGlobalRootsInsideInvocation(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcMutableGlobalFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil {
		t.Fatal("mutable-global module lost native collection admission")
	}
	for _, cfg := range []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	} {
		in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 123)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{123}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [123]", got, callErr)
		}
		in.Close()
	}
}

func gcHostReentryFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	hostType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	outerType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	outerLocals := []byte{0x01, 0x01, 0x63, 0x00}
	outer := append([]byte(nil), outerLocals...)
	outer = append(outer,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x10, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	)
	innerLocals := []byte{0x01, 0x01, 0x7f}
	inner := append([]byte(nil), innerLocals...)
	inner = append(inner,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, 0x0b,
	)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, hostType, outerType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("outer", 0, 1), wasmtest.ExportEntry("inner", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(outer))), outer...),
			append(wasmtest.ULEB(uint32(len(inner))), inner...),
		)),
	)
}

func TestGCHostReentryNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcHostReentryFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.callsites[0].offsets) != 1 {
		t.Fatalf("host re-entry native root map = %+v", plan)
	}
	profiles := []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	}
	reloaded := roundTripCompiled(t, compiled)
	defer reloaded.Close()
	for _, candidate := range []*Compiled{compiled, reloaded} {
		for _, cfg := range profiles {
			var in *Instance
			calls := 0
			in, err = Instantiate(candidate, InstantiateOptions{GC: cfg, Imports: Imports{"env.reenter": HostFunc(func(_ HostModule, _, results []uint64) {
				calls++
				got, callErr := in.Invoke("inner")
				if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
					panic(fmt.Sprintf("inner = %v, %v", got, callErr))
				}
				results[0] = 0
			})}})
			if err != nil {
				t.Fatal(err)
			}
			got, callErr := in.Invoke("outer", 91)
			if callErr != nil || !reflect.DeepEqual(got, []uint64{91}) || calls != 1 {
				in.Close()
				t.Fatalf("outer = %v, %v, calls %d; want [91], nil, 1", got, callErr, calls)
			}
			in.Close()
		}
	}
}

func gcMultiFunctionFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	callerLocals := []byte{0x01, 0x01, 0x63, 0x00}
	caller := append([]byte(nil), callerLocals...)
	caller = append(caller,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01, // root = struct.new(n)
		0x20, 0x00, 0x10, 0x01, 0x1a, // call function 1; drop
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	)
	calleeLocals := []byte{0x01, 0x01, 0x7f}
	callee := append([]byte(nil), calleeLocals...)
	callee = append(callee,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, 0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
		)),
	)
}

func TestGCMultiFunctionNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcMultiFunctionFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) != 2 || len(plan.callsites) != 1 || len(plan.adapterReturnOffsets) < 2 || plan.callsites[0].frameBytes == 0 || plan.safepoints[1].frameBytes == 0 {
		t.Fatalf("multi-function native root map = %+v", plan)
	}
	cfg := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := in.Invoke("run", 37); err != nil || !reflect.DeepEqual(got, []uint64{37}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [37]", got, err)
		}
		in.Close()
	}
}

func gcTailRecursiveFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	locals := []byte{0x01, 0x01, 0x7f} // counter local 1
	body := append([]byte(nil), locals...)
	body = append(body,
		0x20, 0x00, 0x45, 0x04, 0x7f, // if n==0 (result i32)
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00,
		0x05,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x1a, // allocate/drop object for this tail step
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x12, 0x00, // return_call self(n-1)
		0x0b, 0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCTailRecursiveNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTailRecursiveFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 0 {
		t.Fatalf("tail-recursive native root map = %+v, want no retained caller frames", plan)
	}
	cfg := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run", 100); err != nil || !reflect.DeepEqual(got, []uint64{0}) {
		t.Fatalf("run = %v, %v; want [0]", got, err)
	}
}

func gcRecursiveFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	locals := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f}
	body := append([]byte(nil), locals...)
	body = append(body,
		0x20, 0x00, 0x45, // local.get n; i32.eqz
		0x04, 0x7f, // if (result i32)
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x02, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01, // counter >= 1000 => break
		0xfb, 0x01, 0x00, 0x1a, // struct.new_default; drop
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, // end loop/block; base result 0
		0x05,                                     // else
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01, // struct.new(n); local.set ref
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x10, 0x00, 0x1a, // recurse(n-1); drop
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, // ref; struct.get 0 0
		0x0b, 0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCRecursiveNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcRecursiveFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.adapterReturnOffsets) == 0 || len(plan.callsites[0].offsets) != 1 {
		t.Fatalf("recursive native root map = %+v", plan)
	}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run", 8); err != nil || !reflect.DeepEqual(got, []uint64{8}) {
				t.Fatalf("run = %v, %v; want [8]", got, err)
			}
			if stats := in.gc.Stats(); stats.FullCollections == 0 {
				t.Fatalf("collector stats = %+v, want recursive in-invocation collection", stats)
			}
		})
	}
	loaded := roundTripCompiled(t, compiled)
	defer loaded.Close()
	if roots := loaded.genericGCFrameRoots(); roots == nil || len(roots.callsites) != 1 {
		t.Fatalf("reloaded recursive root map = %+v", roots)
	}
	in, err := Instantiate(loaded, InstantiateOptions{GC: profiles[0].cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run", 8); err != nil || !reflect.DeepEqual(got, []uint64{8}) {
		t.Fatalf("reloaded run = %v, %v; want [8]", got, err)
	}
}

func gcFrameRootLimitModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01}
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	vec := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	body := append(locals, 0xfb, 0x01, 0x00, 0x1a)
	for i := uint32(0); i < count; i++ {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(i)...)
		body = append(body, 0x1a)
	}
	body = append(body, 0xfd, 0x0c)
	body = append(body, vec...)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCNativeFrameRootAdapterWritesParkedSlot(t *testing.T) {
	frame := make([]byte, 16)
	binary.LittleEndian.PutUint64(frame[8:], 7)
	roots := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{8}}
	seen := 0
	roots.RangeRoots(func(slot gc.RootSlot) bool {
		seen++
		if got := slot.GetRef(); got != gc.Ref(7) {
			t.Fatalf("root = %d, want 7", got)
		}
		slot.SetRef(gc.Ref(11))
		return true
	})
	goruntime.KeepAlive(frame)
	if seen != 1 || binary.LittleEndian.Uint64(frame[8:]) != 11 {
		t.Fatalf("root adapter seen=%d frame=%#x, want one rewritten qword", seen, frame[8:])
	}
}

func TestGCSingleNativeFrameRootLimit(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	compiled, err := Compile(cfg, gcFrameRootLimitModule(64))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) != 1 || len(plan.safepoints[0].offsets) != 64 || plan.safepoints[0].offsets[0] != 16 || plan.safepoints[0].offsets[63] != 520 {
		t.Fatalf("64-root plan = %+v", plan)
	}
	gcCfg := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	in, err := Instantiate(compiled, InstantiateOptions{GC: gcCfg})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("run"); err != nil {
		in.Close()
		t.Fatal(err)
	}
	in.Close()

	over, err := Compile(cfg, gcFrameRootLimitModule(65))
	if err != nil {
		t.Fatal(err)
	}
	defer over.Close()
	if plan := over.genericGCFrameRoots(); plan != nil {
		t.Fatalf("65-root module unexpectedly admitted: %+v", plan)
	}
}

func TestGCSingleNativeFrameRootsCollectInsideInvocation(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSingleFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) != 2 || len(plan.safepoints[0].offsets) != 0 || len(plan.safepoints[1].offsets) != 1 || plan.safepoints[1].offsets[0] != 24 || plan.safepoints[1].frameBytes == 0 {
		t.Fatalf("native GC frame-root plan = %+v, want dead local omitted at site 1 and live local offset 24 at site 2", plan)
	}
	want := []uint64{0x0706050403020100, 0x0f0e0d0c0b0a0908}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("run", 1000)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("run = %#x, %v; want %#x", got, err, want)
			}
			stats := in.gc.Stats()
			if stats.Allocations != 1001 || stats.FullCollections == 0 {
				t.Fatalf("collector stats = %+v, want 1001 allocations and in-invocation collection", stats)
			}
		})
	}
}

func TestGCSingleNativeFrameRootsHelperPathAllocations(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSingleFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", 0); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := in.Invoke("run", 0); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("exact frame-root helper path allocations = %v, want 0", allocs)
	}
}

func BenchmarkGCSingleNativeFrameRoots(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSingleFrameRootModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run", 1); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGCSingleNativeFrameRootsPublishHiddenOperandRoot(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcHiddenOperandRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) != 2 || len(plan.safepoints[0].offsets) != 0 || len(plan.safepoints[1].offsets) != 1 {
		t.Fatalf("hidden operand root plan = %+v, want one spill root at the second allocation", plan)
	}
	cfg := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	want := []uint64{0x0706050403020100, 0x0f0e0d0c0b0a0908}
	if got, err := in.Invoke("run"); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("run = %#x, %v; want %#x", got, err, want)
	}
}

func TestGCFrameRootCodecRejectsMalformedMetadata(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	for _, tc := range []struct {
		name   string
		module []byte
		mutate func(*compiledGCFrameRoots)
	}{
		{name: "zero safepoint", module: gcSingleFrameRootModule(), mutate: func(root *compiledGCFrameRoots) { root.safepoints[0].id = 0 }},
		{name: "duplicate safepoint", module: gcSingleFrameRootModule(), mutate: func(root *compiledGCFrameRoots) { root.safepoints[1].id = root.safepoints[0].id }},
		{name: "unaligned root", module: gcSingleFrameRootModule(), mutate: func(root *compiledGCFrameRoots) { root.safepoints[1].offsets[0]++ }},
		{name: "zero callsite", module: gcRecursiveFrameRootModule(), mutate: func(root *compiledGCFrameRoots) { root.callsites[0].returnOffset = 0 }},
		{name: "bad adapter", module: gcRecursiveFrameRootModule(), mutate: func(root *compiledGCFrameRoots) { root.adapterReturnOffsets[0] = uint32(len(root.callsites)) + 1<<30 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(cfg, tc.module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			root := compiled.genericGCFrameRoots()
			if root == nil {
				t.Fatal("missing root map")
			}
			tc.mutate(root)
			if _, err := compiled.MarshalBinary(); err == nil {
				t.Fatal("MarshalBinary accepted malformed GC frame-root metadata")
			}
		})
	}
}

func TestGCSingleNativeFrameRootsPersistThroughCodec(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSingleFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	loaded := roundTripCompiled(t, compiled)
	defer loaded.Close()
	if loaded.genericGCFrameRoots() == nil {
		t.Fatal("codec reload lost validated native frame-root admission")
	}
	cfg := gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	in, err := Instantiate(loaded, InstantiateOptions{GC: cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	want := []uint64{0x0706050403020100, 0x0f0e0d0c0b0a0908}
	if got, err := in.Invoke("run", 1000); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded run = %#x, %v; want %#x", got, err, want)
	}
	if stats := in.gc.Stats(); stats.FullCollections == 0 {
		t.Fatalf("reloaded collector stats = %+v, want in-invocation collection", stats)
	}
}
