//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The public boundary is one i32 slot. Twenty-one internal carriers force
// sizing from actual function signatures, even through a small foreign caller.
func rv64WideClosedReferenceSource(vector, tails bool) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	wide := []wasm.ValType{wasm.I32}
	for range 12 {
		wide = append(wide, wasm.FuncRef)
	}
	for range 4 {
		wide = append(wide, typ)
	}
	root := []byte{0x20, 0}
	for i := 0; i < 12; i++ {
		if i%2 == 0 {
			root = append(root, 0xd0, 0x70)
		} else {
			root = append(root, 0xd2, 1)
		}
	}
	payload := func(i int) []byte {
		bits := uint64(0x7ff0000000000001) + uint64(i)
		if vector {
			p := []byte{0xfd, 12}
			p = binary.LittleEndian.AppendUint64(p, bits)
			p = binary.LittleEndian.AppendUint64(p, 0x8000000000000000+uint64(i))
			return p
		}
		return binary.LittleEndian.AppendUint64([]byte{0x44}, bits)
	}
	for i := 0; i < 4; i++ {
		root = append(root, payload(i)...)
	}
	root = append(root, 0x10, 2)
	for i := 3; i >= 0; i-- {
		if vector {
			root = append(root, payload(i)...)
			root = append(root, 0xfd, 0x51, 0xfd, 0x53)
		} else {
			root = append(root, 0xbd, 0x42)
			root = append(root, wasmtest.SLEB64(int64(uint64(0x7ff0000000000001)+uint64(i)))...)
			root = append(root, 0x52)
		}
		root = append(root, 4, 0x40, 0, 0x0b)
	}
	for i := 11; i >= 0; i-- {
		root = append(root, 0xd1)
		if i%2 == 0 {
			root = append(root, 0x45)
		}
		root = append(root, 4, 0x40, 0, 0x0b)
	}
	root = append(root, 0x0b)
	var forward, leaf []byte
	for i := range wide {
		forward = append(forward, 0x20, byte(i))
		leaf = append(leaf, 0x20, byte(i))
		if i == 0 {
			leaf = append(leaf, 0x41, 5, 0x6a)
		}
	}
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	forward = append(forward, call, 3, 0x0b)
	leaf = append(leaf, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil), wasmtest.FuncType(wide, wide))), wasmtest.Section(3, []byte{4, 0, 1, 2, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(9, []byte{1, 3, 0, 1, 1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(root), wasmtest.Code([]byte{0x0b}), wasmtest.Code(forward), wasmtest.Code(leaf))))
}

func TestRV64PublicReferenceFunctionsForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, route := range []string{"ordinary", "static-tail", "reference-call", "reference-tail", "table-tail"} {
					key := fmt.Sprintf("reference-functions-wide-v%t-t%t", vector, tails)
					t.Run(fmt.Sprintf("artifact%t-%s-%s", artifact, key, route), func(t *testing.T) {
						pc := rv64NativeTailCompile(t, key, rv64WideClosedReferenceSource(vector, tails), artifact)
						slots := 17
						if vector {
							slots = 21
						}
						if p, r, err := pc.maxCallSlots(); err != nil || p != slots || r != slots {
							t.Fatal("private actual signature sizing", p, r, err)
						}
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rt.Close()
						producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						target, err := producer.ExportedFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						var source []byte
						imports := NewImports()
						args := []uint64{0xdeadbeeffffffffe}
						switch route {
						case "ordinary":
							source = wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))))
							imports = testImports("env.target", target)
						case "static-tail":
							source = rv64PublicTailModule([]wasm.ValType{wasm.I32}, false)
							imports = testImports("env.tail", target)
						case "reference-call", "reference-tail":
							source = rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true, route == "reference-call")
							imports = testImports("env.target", target)
							args = append(args, 0)
						case "table-tail":
							source = rv64PublicDynamicTailModule([]wasm.ValType{wasm.I32}, false)
							args = append(args, 0)
						}
						cc := rv64NativeTailCompile(t, "reference-functions-consumer-"+route, source, artifact)
						caller := rv64NativeTokenInstantiate(t, rt, cc, imports)
						if route == "table-tail" {
							rv64InstallForeignTableTargetAt(t, caller, producer, 0)
						}
						if len(producer.serArgs) < slots*8 || len(producer.results) < slots*8 {
							t.Fatal("undersized producer arenas")
						}
						if len(caller.serArgs) >= len(producer.serArgs) || len(caller.results) >= len(producer.results) {
							t.Fatal("fixture did not separate private and exported capacities")
						}
						beforeCapabilities := map[*Instance]uint64{caller: rv64EntryCapability(caller), producer: rv64EntryCapability(producer)}
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
						if err := pc.Close(); err != nil {
							t.Fatal(err)
						}
						if producer.jm == nil {
							t.Fatal("attached closed producer retired early")
						}
						want := []uint64{3}
						for range 3 {
							if got, err := caller.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
								t.Fatal("closed wide foreign owner", got, err)
							}
						}
						fn, err := caller.WasmFunc("call")
						if err != nil {
							t.Fatal(err)
						}
						session, err := fn.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						for range 3 {
							if got, err := session.Invoke(args...); err != nil || !slices.Equal(got, want) {
								session.Close()
								t.Fatal("prepared wide foreign owner", got, err)
							}
						}
						session.Close()
						if route == "ordinary" || route == "static-tail" || route == "reference-call" {
							if a := testing.AllocsPerRun(10, func() {
								got, err := caller.Invoke("call", args...)
								if err != nil || !slices.Equal(got, want) {
									panic("closed foreign warm invoke")
								}
							}); a != 0 {
								t.Fatal("warm allocations", a)
							}
						}
						for _, in := range []*Instance{caller, producer} {
							state := &in.pluginState.Load().railshotInvocation
							if state.definedTailOwner != nil || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != beforeCapabilities[in] {
								t.Fatal("completed entry retained generation")
							}
						}
						if err := caller.Close(); err != nil {
							t.Fatal(err)
						}
						if producer.jm != nil {
							t.Fatal("last foreign owner did not retire producer")
						}
					})
				}
			}
		}
	}
}
