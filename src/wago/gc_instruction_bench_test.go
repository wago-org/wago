//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcInstructionBenchmarkModule(body []byte, globals ...[]byte) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	return gcInstructionBenchmarkModuleWithTypes([][]byte{structType}, body, globals...)
}

func gcInstructionBenchmarkModuleWithTypes(types [][]byte, body []byte, globals ...[]byte) []byte {
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	typeEntries := append(append([][]byte(nil), types...), funcType)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(typeEntries...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(uint32(len(types))))),
	}
	if len(globals) != 0 {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	return wasmtest.Module(sections...)
}

func gcRefCastInstructionBenchmarkModule() []byte {
	// Keep the source statically broad and mutable so the cast cannot be folded
	// from the struct.new initializer's exact type.
	global := []byte{
		0x6d, 0x01, // (mut eqref)
		0x41, 0x07, // i32.const 7
		0xfb, 0x00, 0x00, // struct.new 0
		0x0b,
	}
	body := []byte{
		0x01, 0x01, 0x7f, // local 1: counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x01, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x23, 0x00, // global.get 0: dynamic eqref
		0xfb, 0x16, 0x00, // ref.cast (ref 0)
		0x1a,                                     // drop: the cast remains observable because it can trap
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x01, // return counter
		0x0b,
	}
	return gcInstructionBenchmarkModule(body, global)
}

func gcNonFinalRefCastInstructionBenchmarkModule() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},       // open type 0: (struct (field (mut i32)))
		{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}, // final type 1 <: 0
	}
	global := []byte{
		0x6d, 0x01, // (mut eqref)
		0x41, 0x07, // i32.const 7
		0xfb, 0x00, 0x01, // struct.new 1
		0x0b,
	}
	body := []byte{
		0x01, 0x01, 0x7f, // local 1: counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x01, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x23, 0x00, // global.get 0: dynamic eqref
		0xfb, 0x16, 0x00, // ref.cast (ref 0), accepting subtype 1
		0x1a,                                     // drop
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x01, // return counter
		0x0b,
	}
	return gcInstructionBenchmarkModuleWithTypes(types, body, global)
}

func gcStructGetInstructionBenchmarkModule() []byte {
	body := []byte{
		0x02,             // two local declaration groups
		0x01, 0x63, 0x00, // local 1: (ref null 0)
		0x01, 0x7f, // local 2: counter
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x21, 0x01, // object = struct.new 0 (7)
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x02, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x20, 0x01,
		0xfb, 0x02, 0x00, 0x00, // struct.get 0 0
		0x1a,                                     // drop: the access still performs its checked object read
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x02, // return counter
		0x0b,
	}
	return gcInstructionBenchmarkModule(body)
}

func gcNonFinalStructGetInstructionBenchmarkModule() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},       // open type 0: (struct (field (mut i32)))
		{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}, // final type 1 <: 0
	}
	body := []byte{
		0x02,             // two local declaration groups
		0x01, 0x63, 0x01, // local 1: (ref null 1)
		0x01, 0x7f, // local 2: counter
		0x41, 0x07, 0xfb, 0x00, 0x01, 0x21, 0x01, // object = struct.new 1 (7)
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x02, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x20, 0x01,
		0xfb, 0x02, 0x00, 0x00, // struct.get 0 0 through open supertype 0
		0x1a,                                     // drop
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x02, // return counter
		0x0b,
	}
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func gcInstructionLoopControlModule() []byte {
	body := []byte{
		0x01, 0x01, 0x7f, // local 1: counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x01, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x01, // return counter
		0x0b,
	}
	return gcInstructionBenchmarkModule(body)
}

func benchmarkGCInstructionLoop(b *testing.B, module []byte, valuePerIteration uint32) {
	b.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{DisableCollection: true, ThroughputHeapBytes: 64 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()

	if got, err := instance.Invoke("run", 1); err != nil || len(got) != 1 || got[0] != uint64(valuePerIteration) {
		b.Fatalf("warm run = %v, %v; want [%d]", got, err, valuePerIteration)
	}
	if uint64(b.N) > uint64(^uint32(0)) {
		b.Fatalf("iteration count %d exceeds the i32 guest-loop limit", b.N)
	}
	b.ReportAllocs()
	b.ResetTimer()
	got, err := instance.Invoke("run", uint64(uint32(b.N)))
	b.StopTimer()
	want := uint64(uint32(uint64(uint32(b.N)) * uint64(valuePerIteration)))
	if err != nil || len(got) != 1 || got[0] != want {
		b.Fatalf("run(%d) = %v, %v; want [%d]", b.N, got, err, want)
	}
}

// BenchmarkGCRefCastInstruction measures a successful final-type ref.cast in a
// guest loop. The source comes from a mutable eqref global, so exact-type facts
// cannot remove the cast. One host invocation executes b.N casts.
func BenchmarkGCRefCastInstruction(b *testing.B) {
	benchmarkGCInstructionLoop(b, gcRefCastInstructionBenchmarkModule(), 1)
}

// BenchmarkGCStructGetInstruction measures struct.get on one retained object
// with a mutable i32 field. The object is allocated before the guest loop, so
// allocation and host-boundary costs are amortized over b.N field reads.
func BenchmarkGCStructGetInstruction(b *testing.B) {
	benchmarkGCInstructionLoop(b, gcStructGetInstructionBenchmarkModule(), 1)
}

// BenchmarkGCRefCastNonFinalInstruction measures a successful cast from a
// dynamic eqref holding a proper subtype to an open struct supertype.
func BenchmarkGCRefCastNonFinalInstruction(b *testing.B) {
	benchmarkGCInstructionLoop(b, gcNonFinalRefCastInstructionBenchmarkModule(), 1)
}

// BenchmarkGCStructGetNonFinalInstruction measures field access declared
// through an open struct supertype while the retained object has a final subtype.
func BenchmarkGCStructGetNonFinalInstruction(b *testing.B) {
	benchmarkGCInstructionLoop(b, gcNonFinalStructGetInstructionBenchmarkModule(), 1)
}

// BenchmarkGCInstructionLoopControl measures the shared guest loop and counter
// update without a GC instruction. It is a lower-bound control, not an
// exact value to subtract from the instruction benchmarks.
func BenchmarkGCInstructionLoopControl(b *testing.B) {
	benchmarkGCInstructionLoop(b, gcInstructionLoopControlModule(), 1)
}
