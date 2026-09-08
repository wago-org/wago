//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
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
		0xfb, 0x01, 0x00, 0xd1, 0x1a, // struct.new_default 0; ref.is_null; drop
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
		0xfb, 0x01, 0x00, 0xd1, 0x1a, // struct.new_default 0; ref.is_null; drop
		0xfb, 0x02, 0x00, 0x00, 0x0b, // struct.get 0 0
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func gcDeepHiddenOperandRootModule(depth int) []byte {
	body := append([]byte{0x41}, wasmtest.SLEB32(73)...)
	body = append(body, 0xfb, 0x00, 0x00) // struct.new 0; keep the reference hidden on the operand stack.
	for range depth {
		body = append(body, 0x02, 0x40) // block
	}
	body = append(body, 0xfb, 0x01, 0x00, 0x1a) // collecting struct.new_default 0; drop
	for range depth {
		body = append(body, 0x0b)
	}
	body = append(body, 0xfb, 0x02, 0x00, 0x00, 0x0b) // hidden struct.get 0 0; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func BenchmarkGCDeepControlFrameRootCompilation(b *testing.B) {
	data := gcDeepHiddenOperandRootModule(128)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		if err := compiled.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func gcTryTableHiddenOperandRootModule() []byte {
	body := append([]byte{0x41}, wasmtest.SLEB32(73)...)
	body = append(body,
		0xfb, 0x00, 0x00, // struct.new 0; first ref remains on the operand stack
		0x02, 0x40, // catch target block
		0x1f, 0x40, 0x01, byte(wasm.CatchAll), 0x00, // try_table (catch_all 0)
		0x01,       // nop: normal fallthrough
		0x0b, 0x0b, // end try_table and block
		0xfb, 0x01, 0x00, 0x1a, // allocating struct.new_default; drop
		0xfb, 0x02, 0x00, 0x00, // hidden struct.get 0 0
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCTryTablePreservesHiddenStackRoot(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTryTableHiddenOperandRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 2 {
		t.Fatalf("try_table root admission = %+v", status)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, callErr := in.Invoke("run")
	if callErr != nil || !reflect.DeepEqual(got, []uint64{73}) {
		t.Fatalf("run = %v, %v; want [73]", got, callErr)
	}
}

func gcEHFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	tagType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tag := []byte{0x00, 0x01}
	body := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x02, 0x7f,
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00,
		0x41, 0x07, 0x08, 0x00,
		0x0b, 0x41, 0x00, 0x0b, 0x1a,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x02, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, tagType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(13, wasmtest.Vec(tag)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCEHNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcEHFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil {
		t.Fatal("EH module lost native collection admission")
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 44)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{44}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [44]", got, callErr)
		}
		in.Close()
	}
}

func gcVersionedLoopRootPlanModule() []byte {
	callee := []byte{0x0b}
	run := []byte{
		0x01, 0x01, 0x63, 0x00, // local 1: rooted struct
		0xfb, 0x01, 0x00, 0x21, 0x01, // initial allocation remains live
		0x03, 0x40, // versioning-eligible void loop
	}
	for range 4 {
		run = append(run, 0x20, 0x00, 0x28, 0x02, 0x00, 0x1a) // load invariant base; drop
	}
	run = append(run,
		0xfb, 0x01, 0x00, 0x1a, // collecting allocation in the loop
		0x10, 0x00, // ordinary call in the loop
		0x0b,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, // return rooted field
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(callee),
			append(wasmtest.ULEB(uint32(len(run))), run...),
		)),
	)
}

func compileGCVersionedLoopRootPlan(t *testing.T) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(cfg, gcVersionedLoopRootPlanModule())
	if err != nil {
		t.Fatal(err)
	}
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact {
		compiled.Close()
		t.Fatalf("versionable-loop root admission = %+v", status)
	}
	return compiled
}

func TestGCVersionedLoopPreservesRootPlanSafepointCount(t *testing.T) {
	compiled := compileGCVersionedLoopRootPlan(t)
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if status.Safepoints != 2 {
		t.Fatalf("safepoints = %d, want original Wasm count 2", status.Safepoints)
	}
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) != 2 {
		t.Fatalf("safepoint plan = %+v", plan)
	}
}

func TestGCVersionedLoopPreservesRootPlanCallsiteCount(t *testing.T) {
	compiled := compileGCVersionedLoopRootPlan(t)
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 {
		t.Fatalf("callsite plan = %+v, want one original Wasm call", plan)
	}
}

func gcCallRefFrameRootModule(tail bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	target := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x20, 0x00, 0x0b}
	caller := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x00, 0xd2, 0x00}
	if tail {
		caller = append(caller, 0x15, 0x01, 0x0b)
	} else {
		caller = append(caller, 0x14, 0x01, 0x1a, 0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b)
	}
	declared := []byte{0x03, 0x00, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestGCCallRefNativeFrameRoots(t *testing.T) {
	for _, tail := range []bool{false, true} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcCallRefFrameRootModule(tail))
		if err != nil {
			t.Fatal(err)
		}
		plan := compiled.genericGCFrameRoots()
		wantCallsites := 3 // internal, same-context wrapper, cross-context wrapper
		if tail {
			wantCallsites = 0
		}
		if plan == nil || len(plan.callsites) != wantCallsites {
			compiled.Close()
			t.Fatalf("call_ref tail=%v root map = %+v", tail, plan)
		}
		in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 55)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{55}) {
			in.Close()
			compiled.Close()
			t.Fatalf("call_ref tail=%v = %v, %v; want [55]", tail, got, callErr)
		}
		in.Close()
		compiled.Close()
	}
}

func gcTableFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	table := []byte{0x63, 0x00, 0x00, 0x01} // one (ref null 0) slot
	body := []byte{0x01, 0x01, 0x7f,
		0x41, 0x00, 0x20, 0x00, 0xfb, 0x00, 0x00, 0x26, 0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCTableRootsInsideInvocation(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTableFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil {
		t.Fatal("collector table module lost native collection admission")
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 88)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{88}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [88]", got, callErr)
		}
		in.Close()
	}
}

func gcIndirectFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	target := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, 0x0b}
	caller := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x00, 0x11, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	table := []byte{0x70, 0x00, 0x01}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestGCIndirectNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcIndirectFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.callsites[0].offsets) != 1 {
		t.Fatalf("indirect native root map = %+v", plan)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 77)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [77]", got, callErr)
		}
		in.Close()
	}
}

func gcTailIndirectFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	target := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x20, 0x00, 0x0b}
	caller := []byte{0x00, 0x20, 0x00, 0x41, 0x00, 0x13, 0x01, 0x00, 0x0b}
	table := []byte{0x70, 0x00, 0x01}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestGCTailIndirectNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTailIndirectFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 0 {
		t.Fatalf("tail-indirect root map = %+v, want no retained caller callsite", plan)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("run", 66); err != nil || !reflect.DeepEqual(got, []uint64{66}) {
		t.Fatalf("run = %v, %v; want [66]", got, err)
	}
}

func gcMutableGlobalFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b} // (mut (ref null 0)) = ref.null 0
	body := []byte{0x01, 0x01, 0x7f,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
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
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
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
	reloaded := publicArtifactRoundTrip(t, compiled)
	defer reloaded.Close()
	for _, candidate := range []*Compiled{compiled, reloaded} {
		for _, cfg := range profiles {
			var in *Instance
			calls := 0
			in, err = Instantiate(candidate, InstantiateOptions{GC: cfg, Imports: Imports{"env.reenter": HostFunc(func(mod HostModule, _, results []uint64) {
				calls++
				got, callErr := in.InvokeFromHost(context.Background(), mod, "inner")
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
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
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
	// Only the exported caller is host-addressable. The direct-only callee has
	// safepoint/callsite metadata but intentionally no adapter return offset.
	if plan == nil || len(plan.safepoints) != 2 || len(plan.callsites) != 1 || len(plan.adapterReturnOffsets) != 1 || plan.callsites[0].frameBytes == 0 || plan.safepoints[1].frameBytes == 0 {
		t.Fatalf("multi-function native root map = %+v", plan)
	}
	cfg := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
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
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
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
		0xfb, 0x01, 0x00, 0xd1, 0x1a, // struct.new_default; ref.is_null; drop
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
	loaded := publicArtifactRoundTrip(t, compiled)
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
	body := append(locals, 0xfb, 0x01, 0x00, 0xd1, 0x1a)
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

func gcManyLiveObjectsFrameRootModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	body := append([]byte{}, locals...)
	for i := uint32(0); i < count; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i))...)
		body = append(body, 0xfb, 0x00, 0x00, 0x21)
		body = append(body, wasmtest.ULEB(i)...)
	}
	body = append(body, 0xfb, 0x01, 0x00, 0x1a) // collecting site with every local live
	for i := uint32(0); i < count; i++ {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(i)...)
		body = append(body, 0xfb, 0x02, 0x00, 0x00, 0x1a)
	}
	body = append(body, 0x41, 0x07, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcSparseLiveFrameRootModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	body := append(locals,
		0xfb, 0x01, 0x00, 0x1a, // struct.new_default; drop
		0x20, 0x00, 0x1a, // local.get 0; drop: only this local crosses collection
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcDisjointLiveFrameRootModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	body := locals
	for i := uint32(0); i < count; i++ {
		body = append(body, 0xfb, 0x01, 0x00, 0x21) // new default; local.set i
		body = append(body, wasmtest.ULEB(i)...)
		body = append(body, 0xfb, 0x01, 0x00, 0x1a) // collect while only i is live
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(i)...)
		body = append(body, 0x1a) // local.get i; drop
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcRepeatedFrameRootModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01}
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	vec := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	body := append(locals,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
	)
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

func gcPerFunctionFrameRootModule(wideRoots uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01}
	funcType := wasmtest.FuncType(nil, []wasm.ValType{wasm.V128})
	vec := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	collecting := []byte{0x01, 0x01, 0x63, 0x00, 0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0xfd, 0x0c}
	collecting = append(collecting, vec...)
	collecting = append(collecting, 0x0b)
	wide := append([]byte{0x01}, wasmtest.ULEB(wideRoots)...)
	wide = append(wide, 0x63, 0x00)
	for i := uint32(0); i < wideRoots; i++ {
		wide = append(wide, 0x20)
		wide = append(wide, wasmtest.ULEB(i)...)
		wide = append(wide, 0x1a)
	}
	wide = append(wide, 0xfd, 0x0c)
	wide = append(wide, vec...)
	wide = append(wide, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("collecting", 0, 0), wasmtest.ExportEntry("wide", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(collecting))), collecting...),
			append(wasmtest.ULEB(uint32(len(wide))), wide...),
		)),
	)
}

func gcWideCallerFrameRootModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	callerType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	calleeType := wasmtest.FuncType(nil, nil)
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	caller := append(locals,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01, // root local 1 = struct.new(input)
		0x10, 0x01, // call allocating callee
	)
	for i := uint32(0); i < count; i++ {
		caller = append(caller, 0x20)
		caller = append(caller, wasmtest.ULEB(i+1)...)
		caller = append(caller, 0x1a)
	}
	caller = append(caller, 0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b)
	callee := []byte{0x00, 0xfb, 0x01, 0x00, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, callerType, calleeType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
		)),
	)
}

func gcLocalStartFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	startType := wasmtest.FuncType(nil, nil)
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	start := []byte{0x00, 0xfb, 0x01, 0x00, 0x1a, 0x0b}
	run := []byte{0x00, 0x41, 0x07, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, startType, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(start))), start...),
			append(wasmtest.ULEB(uint32(len(run))), run...),
		)),
	)
}

type gcClassifiedRootCounter struct {
	classes map[gc.RootClass]int
}

func (s *gcClassifiedRootCounter) VisitClassifiedRootRef(class gc.RootClass, _ gc.Ref) bool {
	if s.classes == nil {
		s.classes = make(map[gc.RootClass]int)
	}
	s.classes[class]++
	return true
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

func TestGCNativeFrameRootAdapterClassifiesFrames(t *testing.T) {
	frame := make([]byte, 16)
	binary.LittleEndian.PutUint64(frame[8:], 7)
	roots := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{8}}
	counter := new(gcClassifiedRootCounter)
	roots.RangeClassifiedRootRefs(counter)
	goruntime.KeepAlive(frame)
	if counter.classes[gc.RootNativeFrame] != 1 || len(counter.classes) != 1 {
		t.Fatalf("classified native roots = %v", counter.classes)
	}
}

func BenchmarkGCNativeFrameRootMetadataWidths(b *testing.B) {
	for _, roots := range []uint32{64, 65, 128, 256, 1024} {
		b.Run(fmt.Sprintf("roots=%d", roots), func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithGCCodeTelemetry(true), gcFrameRootLimitModule(roots))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			telemetry, ok := compiled.GCNativeCodeTelemetry()
			if !ok || telemetry.RootMapBytes == 0 {
				b.Fatalf("native code telemetry = %+v, enabled=%v", telemetry, ok)
			}
			plan := compiled.genericGCFrameRoots()
			if plan == nil || len(plan.safepoints) != 1 {
				b.Fatalf("root plan = %+v", plan)
			}
			b.ReportAllocs()
			var sink *compiledGCFrameSafepoint
			b.ResetTimer()
			b.ReportMetric(float64(telemetry.RootMapBytes), "root-map-bytes/site")
			for i := 0; i < b.N; i++ {
				sink = plan.safepointByID(1)
			}
			if sink == nil {
				b.Fatal("safepoint lookup failed")
			}
		})
	}
}

func TestGCSingleNativeFrameRootWidths(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	for _, roots := range []uint32{64, 65, 128, 256, 1024} {
		t.Run(fmt.Sprintf("roots=%d", roots), func(t *testing.T) {
			compiled, err := Compile(cfg, gcFrameRootLimitModule(roots))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			plan := compiled.genericGCFrameRoots()
			status := compiled.GCNativeRootAdmission()
			if !status.Required || !status.Exact || status.MaximumRoots != roots || status.Safepoints != 1 || status.Reason != "" {
				t.Fatalf("%d-root admission = %+v", roots, status)
			}
			wantLast := uint32(16 + (roots-1)*8)
			if plan == nil || len(plan.safepoints) != 1 || len(plan.safepoints[0].offsets) != int(roots) || plan.safepoints[0].offsets[0] != 16 || plan.safepoints[0].offsets[roots-1] != wantLast {
				t.Fatalf("%d-root plan = %+v", roots, plan)
			}
			loaded := publicArtifactRoundTrip(t, compiled)
			defer loaded.Close()
			gcCfg := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
			for _, candidate := range []*Compiled{compiled, loaded} {
				in, err := Instantiate(candidate, InstantiateOptions{GC: gcCfg})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("run"); err != nil {
					in.Close()
					t.Fatal(err)
				}
				in.Close()
			}
		})
	}
}

func TestGCRepeatedFrameRootMapsShareStorageAfterCodec(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcRepeatedFrameRootModule(65))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		plan := candidate.genericGCFrameRoots()
		if plan == nil || len(plan.safepoints) != 2 || len(plan.safepoints[0].offsets) != 65 || len(plan.safepoints[1].offsets) != 65 {
			t.Fatalf("repeated root plan = %+v", plan)
		}
		if unsafe.SliceData(plan.safepoints[0].offsets) != unsafe.SliceData(plan.safepoints[1].offsets) {
			t.Fatal("identical repeated root maps do not share immutable storage")
		}
	}
}

func TestGCNativeRootAdmissionIsPerFunction(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcPerFunctionFrameRootModule(1025))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 1 {
		t.Fatalf("per-function admission = %+v", status)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("collecting"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("wide"); err != nil {
		t.Fatal(err)
	}
}

func TestGCNativeRootAdmissionAcceptsMoreThan1024LiveRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFrameRootLimitModule(1025))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.MaximumRoots != 1025 {
		t.Fatalf("wide root admission = %+v", status)
	}
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) == 0 || len(plan.safepoints[0].offsets) != 1025 {
		t.Fatalf("wide root map = %+v", plan)
	}
}

func TestGCNativeFrameKeepsMoreThan1024LiveObjects(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	const roots = 1025
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcManyLiveObjectsFrameRootModule(roots))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Exact || status.MaximumRoots < roots {
		t.Fatalf("wide live-object root admission = %+v", status)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128 << 10, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "run"); got != 7 {
		t.Fatalf("wide live-object collection result = %d, want 7", got)
	}
}

func TestGCNativeRootAdmissionCompactsDeadDeclaredLocals(t *testing.T) {
	const declaredRoots = 1138
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSparseLiveFrameRootModule(declaredRoots))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 1 {
		t.Fatalf("%d-declared-root admission = %+v", declaredRoots, status)
	}
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.safepoints) != 1 || len(plan.safepoints[0].offsets) != 1 {
		t.Fatalf("compacted native root plan = %+v", plan)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run"); err != nil {
		t.Fatal(err)
	}
}

func TestGCNativeRootAdmissionAllowsWideDisjointLiveUnion(t *testing.T) {
	const declaredRoots = 1025
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDisjointLiveFrameRootModule(declaredRoots))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Exact || status.Safepoints != 2*declaredRoots || status.MaximumRoots != 1 {
		t.Fatalf("%d-root disjoint-union admission = %+v", declaredRoots, status)
	}
}

func TestGCLocalStartUsesExactNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLocalStartFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if plan := compiled.genericGCFrameRoots(); plan == nil || len(plan.safepoints) != 1 {
		t.Fatalf("local-start root map = %+v", plan)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, invokeErr := in.Invoke("run")
		in.Close()
		if invokeErr != nil || !reflect.DeepEqual(got, []uint64{7}) {
			t.Fatalf("run = %v, %v; want [7]", got, invokeErr)
		}
	}
}

func TestGCWideCallerNativeFrameRootsRewrite(t *testing.T) {
	const roots = 65
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcWideCallerFrameRootModule(roots))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.callsites[0].offsets) != roots {
		t.Fatalf("wide caller root map = %+v", plan)
	}
	cfg := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		got, invokeErr := in.Invoke("run", 73)
		in.Close()
		if invokeErr != nil || !reflect.DeepEqual(got, []uint64{73}) {
			t.Fatalf("run = %v, %v; want [73]", got, invokeErr)
		}
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
	if plan == nil || len(plan.safepoints) != 2 || len(plan.safepoints[0].offsets) != 1 || plan.safepoints[0].offsets[0] != 24 || len(plan.safepoints[1].offsets) != 1 || plan.safepoints[1].offsets[0] != 24 || plan.safepoints[1].frameBytes == 0 {
		t.Fatalf("native GC frame-root plan = %+v, want bounded conservative local offset 24 at both sites", plan)
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
	b.ReportMetric(float64(compiled.CodeSize()), "code-B")
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
		{name: "root vector exceeds frame", module: gcFrameRootLimitModule(64), mutate: func(root *compiledGCFrameRoots) {
			root.safepoints[0].offsets = make([]uint32, 1025)
			for i := range root.safepoints[0].offsets {
				root.safepoints[0].offsets[i] = shared.AMD64FrameHeaderBytes + uint32(i*8)
			}
		}},
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
	loaded := publicArtifactRoundTrip(t, compiled)
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
