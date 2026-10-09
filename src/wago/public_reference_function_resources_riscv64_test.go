//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Three independent borrowers keep function, memory and global roots distinct.
// Memory0 aliases producer memory0, but the invocation context belongs here.
func rv64ReferenceResourceConsumer(vector, wideFirst bool, kind string) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	var types, imports, functions, exports, codes [][]byte
	imported := uint32(0)
	if kind == "combined" {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.I32}))
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))
		imported = 1
	}
	memories, globals := kind != "globals", kind != "memory"
	widths := []bool{wideFirst, wideFirst, !wideFirst}
	if memories {
		for i, name := range []string{"memory0", "alias0", "memory1"} {
			flag := byte(1)
			if widths[i] {
				flag = 5
			}
			imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 2, flag, 1, 3))
		}
	}
	if globals {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "counter", wasm.I32, true), wasmtest.GlobalImportEntry("env", "counter-alias", wasm.I32, true), wasmtest.GlobalImportEntry("env", "payload", typ, true))
	}
	add := func(name string, params, results []wasm.ValType, body []byte) {
		functions = append(functions, wasmtest.ULEB(uint32(len(types))))
		types = append(types, wasmtest.FuncType(params, results))
		exports = append(exports, wasmtest.ExportEntry(name, 0, imported+uint32(len(codes))))
		codes = append(codes, wasmtest.Code(append(body, 0x0b)))
	}
	constant := func(index int, n int32) []byte {
		op := byte(0x41)
		if widths[index] {
			op = 0x42
		}
		return append([]byte{op}, wasmtest.SLEB32(n)...)
	}
	if kind == "combined" {
		body := []byte{0x20, 0, 0x20, 1, 0x10, 0, 0x23, 3, 0x41}
		body = append(body, wasmtest.SLEB32(113)...)
		body = append(body, 0x47, 4, 0x40, 0, 0x0b)
		// This passive segment belongs to the caller, after returning from a
		// producer that has its own data slots and the same primary JobMemory.
		body = append(body, constant(2, 80)...)
		body = append(body, 0x41, 0, 0x41, 1, 0xfc, 8, 0, 2)
		add("call", []wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.I32}, body)
	}
	if memories {
		var probe []byte
		for i := range widths {
			probe = append(probe, 0x3f, byte(i))
			if !widths[i] {
				probe = append(probe, 0xad)
			}
		}
		probe = append(probe, constant(2, 80)...)
		probe = append(probe, rv64M64Arg([]byte{0x2d}, 2, 0)...)
		add("memories", nil, []wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I32}, probe)
		for i := range widths {
			addr := wasm.I32
			if widths[i] {
				addr = wasm.I64
			}
			add(fmt.Sprintf("grow%d", i), []wasm.ValType{addr}, []wasm.ValType{addr}, []byte{0x20, 0, 0x40, byte(i)})
		}
	}
	if globals {
		add("globals", nil, []wasm.ValType{wasm.I32, wasm.I32, typ}, []byte{0x23, 0, 0x23, 1, 0x23, 2})
		add("set", []wasm.ValType{wasm.I32, typ}, nil, []byte{0x20, 0, 0x24, 1, 0x20, 1, 0x24, 2})
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, wasmtest.Vec(functions...))}
	if kind == "combined" {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, append(append([]byte{0x41}, wasmtest.SLEB32(113)...), 0x0b)))))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if kind == "combined" {
		sections = append(sections, wasmtest.Section(12, []byte{1}))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(codes...)))
	if kind == "combined" {
		sections = append(sections, wasmtest.Section(11, []byte{1, 1, 1, 0xcc}))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceFunctionResources(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, wide := range []bool{false, true} {
					for _, memoryLast := range []bool{false, true} {
						key := fmt.Sprintf("reference-resources-v%t-t%t-w%t", vector, tails, wide)
						t.Run(fmt.Sprintf("artifact%t-%s-memoryLast%t", artifact, key, memoryLast), func(t *testing.T) {
							pc := rv64NativeTailCompile(t, key, wasmtest.ClosedReferenceResources(vector, tails, wide), artifact)
							if !pc.nativeReferenceFunctionABI || pc.MemMaxPages != 3 || len(pc.memoryExportMap()) != 4 || len(pc.GlobalExports) != 5 {
								t.Fatal("resource metadata/reservation")
							}
							rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
							defer rt.Close()
							producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
							im := NewImports()
							memories := make([]*Memory, 2)
							for index := range memories {
								for _, prefix := range []string{"memory", "alias"} {
									name := fmt.Sprintf("%s%d", prefix, index)
									memory, err := producer.ExportedMemory(name)
									if err != nil || memories[index] != nil && memories[index] != memory {
										t.Fatal("memory alias identity", name, err)
									}
									memories[index] = memory
									im.Memory("env", name, memory)
								}
							}
							var counter, payload *Global
							for _, name := range []string{"counter", "counter-alias", "payload", "payload-alias"} {
								global, err := producer.ExportedGlobalObject(name)
								if err != nil {
									t.Fatal(err)
								}
								if name == "counter" {
									counter = global
								} else if name == "counter-alias" && counter != global {
									t.Fatal("counter alias")
								}
								if name == "payload" {
									payload = global
								} else if name == "payload-alias" && payload != global {
									t.Fatal("payload alias")
								}
								im.Global("env", name, global)
							}
							if memory, err := producer.ExportedMemory("absent"); memory != nil || err == nil {
								t.Fatal("memory export name fallback")
							}
							if global, err := producer.ExportedGlobalObject("absent"); global != nil || err == nil {
								t.Fatal("global export name fallback")
							}
							target, err := producer.ExportedFunc("run")
							if err != nil {
								t.Fatal(err)
							}
							im.Function("env", "target", target)
							callers := make(map[string]*Instance)
							compiled := make(map[string]*Compiled)
							for _, kind := range []string{"combined", "memory", "globals"} {
								cc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-borrow-v%t-w%t-%s", vector, wide, kind), rv64ReferenceResourceConsumer(vector, wide, kind), artifact)
								compiled[kind] = cc
								callers[kind] = rv64NativeTokenInstantiate(t, rt, cc, im)
							}
							if callers["combined"].jm != producer.jm || callers["combined"].nativeContext == producer.nativeContext {
								t.Fatal("fixture must share memory0 with distinct contexts")
							}
							args := []uint64{0xdeadbeef00000003, 0x7ff0000000000001}
							if vector {
								args = append(args, 0x80000000deadbeef)
							}
							if err := counter.Set(0xcafe000000000029); err != nil {
								t.Fatal(err)
							}
							// Read the host write inside an internal reference-bearing call.
							rv64MMCall(t, callers["combined"], "call", append(slices.Clone(args[1:]), 53, 0), args...)
							for _, i := range []int{1, 2} {
								rv64MMCall(t, callers["memory"], fmt.Sprintf("grow%d", i), []uint64{1}, 2)
							}
							rv64MMCall(t, callers["combined"], "memories", []uint64{3, 3, 3, 0xcc})
							rv64MMCall(t, producer, "size0", []uint64{3})
							rv64MMCall(t, producer, "size1", []uint64{3})
							fn, err := callers["combined"].WasmFunc("call")
							if err != nil {
								t.Fatal(err)
							}
							session, err := fn.OpenSession()
							if err != nil {
								t.Fatal(err)
							}
							wantCall := append(slices.Clone(args[1:]), 65, 0)
							got, err := session.Invoke(args...)
							session.Close()
							if err != nil || !slices.Equal(got, wantCall) {
								t.Fatal("prepared shared-resource call", got, err)
							}
							if allocations := testing.AllocsPerRun(10, func() {
								wantCall[len(wantCall)-2] += 12
								got, err := callers["combined"].Invoke("call", args...)
								if err != nil || !slices.Equal(got, wantCall) {
									panic("warm resource call")
								}
							}); allocations != 0 {
								t.Fatal("warm resource call allocations", allocations)
							}
							if err := producer.Close(); err != nil {
								t.Fatal(err)
							}
							if err := pc.Close(); err != nil {
								t.Fatal(err)
							}
							if counter.Get() != 0 || counter.Set(1) == nil || memories[0].UnsafeBytes() != nil {
								t.Fatal("closed producer allowed host state access")
							}
							for _, kind := range []string{"memory", "globals"} {
								mod, err := rt.Module(compiled[kind])
								if err != nil {
									t.Fatal(err)
								}
								late, err := rt.Instantiate(context.Background(), mod, WithImports(im))
								mod.Close()
								if late != nil {
									late.Close()
								}
								if late != nil || err == nil {
									t.Fatal("late resource attachment", kind, err)
								}
							}
							rv64MMCall(t, callers["globals"], "set", nil, args...)
							rv64MMCall(t, callers["combined"], "call", append(slices.Clone(args[1:]), 15, 0), args...)
							wantGlobal := append([]uint64{15, 15}, args[1:]...)
							rv64MMCall(t, callers["globals"], "globals", wantGlobal)
							if err := callers["combined"].Close(); err != nil {
								t.Fatal(err)
							}
							// Only independent resource borrowers now keep the producer alive.
							first, last := "memory", "globals"
							if memoryLast {
								first, last = last, first
							}
							if err := callers[first].Close(); err != nil {
								t.Fatal(err)
							}
							if producer.jm == nil || memories[0].jobMemory() == nil || memories[1].jobMemory() == nil || len(counter.cell) == 0 || len(payload.cell) == 0 || pc.codeCache.mem == nil {
								t.Fatal("resource-only borrower lost producer storage", last)
							}
							if memoryLast {
								rv64MMCall(t, callers[last], "memories", []uint64{3, 3, 3, 0xcc})
								want := binary.LittleEndian.AppendUint64(nil, args[1])
								if vector {
									want = binary.LittleEndian.AppendUint64(want, args[2])
								}
								if !bytes.Equal(memories[0].jobMemory().HostBytes()[64:64+len(want)], want) {
									t.Fatal("resource-only retained payload")
								}
							} else {
								rv64MMCall(t, callers[last], "globals", wantGlobal)
							}
							if err := callers[last].Close(); err != nil {
								t.Fatal(err)
							}
							if producer.jm != nil || memories[0].jobMemory() != nil || memories[1].jobMemory() != nil || len(counter.cell) != 0 || len(payload.cell) != 0 || pc.codeCache.mem != nil {
								t.Fatal("last resource borrower retained producer storage")
							}
						})
					}
				}
			}
		}
	}
}
