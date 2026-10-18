//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorForwardBytes() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},                   // open base: (struct (field i32))
		{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}, // open child <: base
	}
	body := []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get 0
		0x41, 0x07, // i32.const 7
		0xfb, 0x00, 0x01, // struct.new child
		0xfb, 0x16, 0x00, // ref.cast base
		0xfb, 0x02, 0x00, 0x00, // struct.get base field 0
		0x0b,
	}
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCConstructorGetForwardExecution(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorForwardBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	profiles := []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			instance, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for i := 0; i < 1000; i++ {
				want := uint64(uint32(i*17 - 33))
				got, err := instance.Invoke("run", want)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("run iteration %d = %v, %v; want [%d]", i, got, err, want)
				}
			}
			if got := instance.gc.Stats().Allocations; got != 1000 {
				t.Fatalf("allocations = %d, want 1000", got)
			}
		})
	}
}

func gcConstructorScalarReplacementTrapBytes() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},                   // open base: (struct (field i32))
		{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}, // open child <: base
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	body := []byte{
		0x20, 0x00, // selected field: local.get 0
		0x41, 0x01, 0x20, 0x01, 0x6e, // unselected field: 1 / local.get 1
		0xfb, 0x00, 0x01, // struct.new child
		0xfb, 0x16, 0x00, // ref.cast base
		0xfb, 0x02, 0x00, 0x00, // struct.get base field 0
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCConstructorScalarReplacementPreservesInitializerTrap(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", enabled), gcConstructorScalarReplacementTrapBytes())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()

			if got, err := instance.Invoke("run", 37, 1); err != nil || len(got) != 1 || got[0] != 37 {
				t.Fatalf("successful run = %v, %v; want [37]", got, err)
			}
			if got := instance.gc.Stats().Allocations; got != 1 {
				t.Fatalf("allocations = %d, want 1", got)
			}

			_, err = instance.Invoke("run", 41, 0)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapDivZero {
				t.Fatalf("division by zero = %v; want %v", err, TrapDivZero)
			}
			if got := instance.gc.Stats().Allocations; got != 1 {
				t.Fatalf("allocations after trap = %d, want 1", got)
			}
		})
	}
}

func TestGCConstructorNativeScalarReplacementPreservesInitializerTrap(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorScalarReplacementTrapBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 37, 1); err != nil || len(got) != 1 || got[0] != 37 {
		t.Fatalf("successful run = %v, %v; want [37]", got, err)
	}
	beforeStats := instance.gc.Stats()
	beforeBump := binary.LittleEndian.Uint32(unsafe.Slice((*byte)(offHeapPtr(instance.gc.NativeView().NurseryBump)), 4))
	_, err = instance.Invoke("run", 41, 0)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapDivZero {
		t.Fatalf("division by zero = %v; want %v", err, TrapDivZero)
	}
	afterStats := instance.gc.Stats()
	afterBump := binary.LittleEndian.Uint32(unsafe.Slice((*byte)(offHeapPtr(instance.gc.NativeView().NurseryBump)), 4))
	if afterStats.Allocations != beforeStats.Allocations || afterStats.LiveObjects != beforeStats.LiveObjects || afterBump != beforeBump {
		t.Fatalf("trap changed native allocation state: stats %+v -> %+v, bump %d -> %d", beforeStats, afterStats, beforeBump, afterBump)
	}
}

func TestGCConstructorNativeChargeComparesFullCollectionCycle(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorForwardBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 37); err != nil || len(got) != 1 || got[0] != 37 {
		t.Fatalf("warm run = %v, %v; want [37]", got, err)
	}

	view := instance.gc.NativeView()
	stats := unsafe.Slice((*byte)(offHeapPtr(view.AllocationCount)), corergc.NativeAllocationDeadObjectsOffset+4)
	binary.LittleEndian.PutUint64(stats[corergc.NativeAllocationMinorCollectionsOffset:], 1<<32)
	binary.LittleEndian.PutUint32(stats[corergc.NativeAllocationDeadObjectsOffset:], 7)
	view.DeadNurseryCycle = 0
	ordinaryLive := instance.gc.Stats().LiveObjects

	if got, err := instance.Invoke("run", 41); err != nil || len(got) != 1 || got[0] != 41 {
		t.Fatalf("run = %v, %v; want [41]", got, err)
	}
	if pending := binary.LittleEndian.Uint32(stats[corergc.NativeAllocationDeadObjectsOffset:]); pending != 1 {
		t.Fatalf("pending dead objects = %d, want 1", pending)
	}
	if view.DeadNurseryCycle != 1<<32 {
		t.Fatalf("dead nursery cycle = %#x, want 0x100000000", view.DeadNurseryCycle)
	}
	if got := instance.gc.Stats().LiveObjects; got != ordinaryLive+1 {
		t.Fatalf("live objects = %d, want %d", got, ordinaryLive+1)
	}
}

func TestGCConstructorScalarReplacementPreservesBoundedAllocation(t *testing.T) {
	var want string
	for _, enabled := range []bool{false, true} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", enabled), gcConstructorForwardBytes())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
			Profile:        GCProfileTiny,
			TinyHeapBytes:  16,
			TinyBlockBytes: 16,
		}})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		_, invokeErr := instance.Invoke("run", 37)
		instance.Close()
		compiled.Close()
		if invokeErr == nil || !strings.Contains(invokeErr.Error(), "tiny heap exhausted") {
			t.Fatalf("enabled=%v error = %v; want tiny heap exhaustion", enabled, invokeErr)
		}
		if enabled {
			if invokeErr.Error() != want {
				t.Fatalf("enabled error = %q; want disabled error %q", invokeErr, want)
			}
		} else {
			want = invokeErr.Error()
		}
	}
}

func gcConstructorScalarMatrixBytes(valueType wasm.ValType, fieldIndex byte, cast []byte, accessType byte, livePrefix bool) []byte {
	encoded := wasm.MustEncodeValType(valueType)
	fields := []byte{0x5f, 0x03, encoded, 0x00, encoded, 0x00, encoded, 0x00}
	base := append([]byte{0x50, 0x00}, fields...)
	child := append([]byte{0x50, 0x01, 0x00}, fields...)
	body := make([]byte, 0, 24)
	if livePrefix {
		body = append(body, 0x41, 0x07)
	}
	body = append(body,
		0x20, 0x00,
		0x20, 0x01,
		0x20, 0x02,
		0xfb, 0x00, 0x01, // struct.new child
	)
	body = append(body, cast...)
	body = append(body,
		0xfb, 0x02, accessType, fieldIndex, // struct.get
	)
	if livePrefix {
		body = append(body, 0x6a) // i32.add
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			base,
			child,
			wasmtest.FuncType([]wasm.ValType{valueType, valueType, valueType}, []wasm.ValType{valueType}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCConstructorScalarReplacementValues(t *testing.T) {
	for _, tc := range []struct {
		name       string
		valueType  wasm.ValType
		fieldIndex byte
		cast       []byte
		accessType byte
		args       []uint64
		want       uint64
		livePrefix bool
	}{
		{name: "i32 first live prefix", valueType: wasm.I32, fieldIndex: 0, cast: []byte{0xfb, 0x16, 0x00}, args: []uint64{0xfffffff1, 2, 3}, want: uint64(uint32(0xfffffff8)), livePrefix: true},
		{name: "i32 middle nullable cast", valueType: wasm.I32, fieldIndex: 1, cast: []byte{0xfb, 0x17, 0x00}, args: []uint64{1, 0x87654321, 3}, want: 0x87654321},
		{name: "i32 last exact cast", valueType: wasm.I32, fieldIndex: 2, cast: []byte{0xfb, 0x16, 0x62, 0x01}, accessType: 1, args: []uint64{1, 2, 0x76543210}, want: 0x76543210},
		{name: "i64 full bits", valueType: wasm.I64, fieldIndex: 1, cast: []byte{0xfb, 0x16, 0x00}, args: []uint64{1, 0xfedcba9876543210, 3}, want: 0xfedcba9876543210},
		{name: "f32 negative zero", valueType: wasm.F32, fieldIndex: 2, cast: []byte{0xfb, 0x16, 0x00}, args: []uint64{1, 2, 0x80000000}, want: 0x80000000},
		{name: "f64 nan payload", valueType: wasm.F64, fieldIndex: 0, cast: []byte{0xfb, 0x16, 0x00}, args: []uint64{0x7ff8123456789abc, 2, 3}, want: 0x7ff8123456789abc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorScalarMatrixBytes(tc.valueType, tc.fieldIndex, tc.cast, tc.accessType, tc.livePrefix))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run", tc.args...)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("run = %x, %v; want [%x]", got, err, tc.want)
			}
			if got := instance.gc.Stats().Allocations; got != 1 {
				t.Fatalf("allocations = %d, want 1", got)
			}
		})
	}
}

func gcConstructorNativeScalarReplacementBytes(valueType wasm.ValType, fieldIndex byte, cast []byte, accessType byte) []byte {
	encoded := wasm.MustEncodeValType(valueType)
	fields := []byte{0x5f, 0x03, encoded, 0x00, encoded, 0x00, encoded, 0x00}
	base := append([]byte{0x50, 0x00}, fields...)
	child := append([]byte{0x50, 0x01, 0x00}, fields...)
	body := []byte{
		0x02, 0x01, 0x7f, 0x01, encoded, // one i32 counter and one result local
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x03, 0x41, 0x28, 0x4f, 0x0d, 0x01, // break when counter >= 40
		0x20, 0x00, 0x20, 0x01, 0x20, 0x02,
		0xfb, 0x00, 0x01, // struct.new child
	}
	body = append(body, cast...)
	body = append(body,
		0xfb, 0x02, accessType, fieldIndex, // struct.get
		0x21, 0x04, // save the forwarded scalar
		0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03,
		0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x04,
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			base,
			child,
			wasmtest.FuncType([]wasm.ValType{valueType, valueType, valueType}, []wasm.ValType{valueType}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCConstructorScalarReplacementUsesNativeReservation(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	for _, tc := range []struct {
		name       string
		valueType  wasm.ValType
		fieldIndex byte
		cast       []byte
		accessType byte
		args       []uint64
		want       uint64
	}{
		{name: "i32 first", valueType: wasm.I32, cast: []byte{0xfb, 0x16, 0x00}, args: []uint64{0x76543210, 2, 3}, want: 0x76543210},
		{name: "i64 middle nullable", valueType: wasm.I64, fieldIndex: 1, cast: []byte{0xfb, 0x17, 0x00}, args: []uint64{1, 0xfedcba9876543210, 3}, want: 0xfedcba9876543210},
		{name: "f32 last exact", valueType: wasm.F32, fieldIndex: 2, cast: []byte{0xfb, 0x16, 0x62, 0x01}, accessType: 1, args: []uint64{1, 2, 0x80000000}, want: 0x80000000},
		{name: "f64 first", valueType: wasm.F64, cast: []byte{0xfb, 0x16, 0x00}, args: []uint64{0x7ff8123456789abc, 2, 3}, want: 0x7ff8123456789abc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorNativeScalarReplacementBytes(tc.valueType, tc.fieldIndex, tc.cast, tc.accessType))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if status := compiled.GCNativeRootAdmission(); !status.Required || !status.Exact {
				t.Fatalf("root admission = %+v, want required exact maps", status)
			}
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if got, err := instance.Invoke("run", tc.args...); err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("run = %x, %v; want [%x]", got, err, tc.want)
			}
			if got := instance.gc.Stats().Allocations; got != 40 {
				t.Fatalf("allocations = %d, want 40", got)
			}
			view := instance.gc.NativeView()
			state := unsafe.Slice((*byte)(offHeapPtr(view.StructAllocState)), corergc.NativeStructAllocStateSize)
			if count := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCountOffset:]); count == 0 {
				t.Fatal("native reservation count is zero")
			}
			if cursor := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCursorOffset:]); cursor != 0 {
				t.Fatalf("dead native reservation consumed %d handles", cursor)
			}
			bump := unsafe.Slice((*byte)(offHeapPtr(view.NurseryBump)), 4)
			if charged := binary.LittleEndian.Uint32(bump); charged == 0 {
				t.Fatal("dead native allocation did not charge nursery bytes")
			}
			if got := instance.gc.Stats().LiveObjects; got != 40 {
				t.Fatalf("charged live objects = %d, want 40", got)
			}
			instance.gc.CancelNativeAllocationBatch()
			if got := instance.gc.Stats().LiveObjects; got != 40 {
				t.Fatalf("charged live objects after batch cancel = %d, want 40", got)
			}
			if err := instance.gc.CollectMinor(corergc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if got := instance.gc.Stats().LiveObjects; got != 0 {
				t.Fatalf("dead native allocations retained %d handles after collection", got)
			}
		})
	}
}

func gcConstructorAfterNativeArraysBytes() []byte {
	types := [][]byte{
		{0x5e, 0x7f, 0x01},                                     // array (mut i32)
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},                   // open base: (struct (field i32))
		{0x50, 0x01, 0x01, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}, // open child <: base
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	body := []byte{0x01, 0x01, 0x63, 0x00} // one (ref null array) local
	for range 10 {
		body = append(body,
			0x41, 0x01, // i32.const 1
			0xfb, 0x07, 0x00, // array.new_default 0
			0x21, 0x01, // local.set 1
		)
	}
	body = append(body,
		0x20, 0x00, 0x41, 0x07,
		0xfb, 0x00, 0x02, // struct.new child
		0xfb, 0x16, 0x01, // ref.cast base
		0xfb, 0x02, 0x01, 0x00, // struct.get base field 0
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCConstructorNativeChargeKeepsActiveArrayChunk(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorAfterNativeArraysBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 37); err != nil || len(got) != 1 || got[0] != 37 {
		t.Fatalf("run = %v, %v; want [37]", got, err)
	}
	if got := instance.gc.Stats(); got.Allocations != 11 || got.LiveObjects != 11 {
		t.Fatalf("collector stats = %+v, want 11 allocations/live objects", got)
	}
	state := unsafe.Slice((*byte)(offHeapPtr(instance.gc.NativeView().StructAllocState)), corergc.NativeStructAllocStateSize)
	if chunkEnd := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocChunkEndOffset:]); chunkEnd == 0 {
		t.Fatal("dead struct charge discarded the active array chunk")
	}
	if cursor := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCursorOffset:]); cursor == 0 {
		t.Fatal("native arrays did not consume a reserved handle")
	}
}

func gcConstructorMixedNativeStubBytes() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},
		{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00},
	}
	dead := []byte{
		0x20, 0x00, 0x41, 0x07,
		0xfb, 0x00, 0x01,
		0xfb, 0x16, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x1a,
	}
	body := []byte{0x01, 0x01, 0x63, 0x01} // one (ref null child) local
	body = append(body, dead...)
	body = append(body,
		0x20, 0x00, 0x41, 0xe3, 0x00, // field 1 = 99
		0xfb, 0x00, 0x01,
		0x21, 0x01, // save the live child
	)
	body = append(body, dead...) // clear fresh-constructor facts across allocation
	body = append(body,
		0x20, 0x01,
		0xfb, 0x02, 0x01, 0x01, // read child field 1
		0x0b,
	)
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCConstructorMixedNativeStubPreservesLivePayload(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", true), gcConstructorMixedNativeStubBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if status := compiled.GCNativeRootAdmission(); !status.Required || !status.Exact {
		t.Fatalf("root admission = %+v, want required exact maps", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 37); err != nil || len(got) != 1 || got[0] != 99 {
		t.Fatalf("run = %v, %v; want [99]", got, err)
	}
	if got := instance.gc.Stats().Allocations; got != 3 {
		t.Fatalf("allocations = %d, want 3", got)
	}
}

func gcFreshStructLocalForwardBytes(overwriteSource, mutate bool, rootLength byte) []byte {
	fieldMutability := byte(0x00)
	if mutate {
		fieldMutability = 0x01
	}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, fieldMutability}, // (struct (field i32))
		{0x5e, 0x63, 0x00, 0x01},            // (array (mut (ref null 0)))
	}
	body := []byte{
		0x02, 0x01, 0x63, 0x01, 0x01, 0x63, 0x00, // locals: roots, item
		0x41, rootLength, 0xfb, 0x07, 0x01, 0x21, 0x01, // roots = array.new_default 1 (rootLength)
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x02, // item = struct.new 0 (param)
	}
	if overwriteSource {
		body = append(body, 0x41, 0x07, 0x21, 0x00) // direct source-local overwrite
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x02, 0xfb, 0x0e, 0x01, // roots[0] = item
	)
	if mutate {
		body = append(body, 0x20, 0x02, 0x41, 0x07, 0xfb, 0x05, 0x00, 0x00) // item.field 0 = 7
	}
	body = append(body,
		0x20, 0x02, 0xfb, 0x02, 0x00, 0x00, // item.field 0
		0x0b,
	)
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCFreshStructLocalGetForwardExecution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		overwriteSource bool
		mutate          bool
		want            func(int) uint64
	}{
		{name: "forward", want: func(i int) uint64 { return uint64(uint32(i*17 - 33)) }},
		{name: "source overwritten", overwriteSource: true, want: func(i int) uint64 { return uint64(uint32(i*17 - 33)) }},
		{name: "field mutated", mutate: true, want: func(int) uint64 { return 7 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshStructLocalForwardBytes(tc.overwriteSource, tc.mutate, 1))
			if err != nil {
				t.Fatal(err)
			}
			for _, gc := range []GCConfig{
				{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 100; i++ {
					input := uint64(uint32(i*17 - 33))
					want := tc.want(i)
					got, err := instance.Invoke("run", input)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("iteration %d = %v, %v; want [%d]", i, got, err, want)
					}
				}
				if err := instance.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGCFreshStructRootStoreTrap(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshStructLocalForwardBytes(false, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 1); err == nil {
		t.Fatalf("run = %v, want out-of-bounds trap", got)
	}
}

func gcFreshArrayLocalForwardBytes(rootLength, length, mask byte, overwriteSource, mutate, verifyPayload bool) []byte {
	types := [][]byte{
		{0x5e, 0x7f, 0x01},       // (array (mut i32))
		{0x5e, 0x63, 0x00, 0x01}, // (array (mut (ref null 0)))
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	body := []byte{
		0x02, 0x01, 0x63, 0x01, 0x01, 0x63, 0x00, // locals: roots, item
		0x41, rootLength, 0xfb, 0x07, 0x01, 0x21, 0x02,
		0x20, 0x00, 0x41, length, 0xfb, 0x06, 0x00, 0x21, 0x03,
	}
	if overwriteSource {
		body = append(body, 0x41, 0x07, 0x21, 0x00) // direct source-local overwrite
	}
	body = append(body, 0x20, 0x02, 0x41, 0x00, 0x20, 0x03, 0xfb, 0x0e, 0x01)
	if mutate {
		body = append(body, 0x20, 0x03, 0x41, 0x00, 0x41, 0x07, 0xfb, 0x0e, 0x00)
	}
	body = append(body, 0x20, 0x03, 0x20, 0x01, 0x41, mask, 0x71, 0xfb, 0x0b, 0x00, 0x0b)
	if verifyPayload {
		body = body[:len(body)-1]
		body = append(body, 0x20, 0x03, 0x20, 0x01, 0x41, mask, 0x71, 0xfb, 0x0b, 0x00, 0x6a, 0x0b)
	}
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
}

func TestGCFreshArrayLocalGetForwardExecution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		overwriteSource bool
		mutate          bool
	}{
		{name: "forward"},
		{name: "source overwritten", overwriteSource: true},
		{name: "array mutated", mutate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshArrayLocalForwardBytes(1, 16, 15, tc.overwriteSource, tc.mutate, true))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			for _, gc := range []GCConfig{
				{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
				if err != nil {
					t.Fatal(err)
				}
				for index := uint64(0); index < 16; index++ {
					value := uint64(uint32(0x91230000 + index))
					want := uint64(uint32(value * 2))
					if tc.mutate && index == 0 {
						want = 14
					}
					got, err := instance.Invoke("run", value, index)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("profile %d index %d = %v, %v; want [%#x]", gc.Profile, index, got, err, want)
					}
				}
				if got := instance.gc.Stats().Allocations; got != 32 {
					t.Fatalf("profile %d allocations = %d, want 32", gc.Profile, got)
				}
				if err := instance.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGCFreshArrayUnsafeMaskTraps(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshArrayLocalForwardBytes(1, 16, 31, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", 9, 16)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
		t.Fatalf("run = %v, %v; want bounds trap", got, err)
	}
}

func TestGCFreshArrayRootStoreTrap(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshArrayLocalForwardBytes(0, 16, 15, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", 9, 0)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
		t.Fatalf("run = %v, %v; want root-store bounds trap", got, err)
	}
}
