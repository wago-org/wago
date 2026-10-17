//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportsConsumer(vector, shared bool, route string) []byte {
	return rv64ReferenceImportsConsumerWidth(vector, shared, true, route)
}

func rv64ReferenceImportsConsumerWidth(vector, shared, wide bool, route string) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)}
	if shared {
		flag := byte(1)
		if wide {
			flag = 5
		}
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, flag, 1, 3))
	}
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, wasm.I32}, []wasm.ValType{typ, wasm.I32, wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
	body := []byte{0x20, 0, 0x20, 1, 0x20, 2}
	switch route {
	case "ordinary":
		body = append(body, 0x10, 0)
	case "static-tail":
		body = append(body, 0x12, 0)
	case "reference-call":
		body = append(body, 0xd2, 0, 0x14, 0)
	case "reference-tail":
		body = append(body, 0xd2, 0, 0x15, 0)
	default:
		panic("invalid reference import consumer")
	}
	if route == "ordinary" || route == "reference-call" {
		body = append(body, 0x23, 0, 0x41)
		body = append(body, wasmtest.SLEB32(113)...)
		body = append(body, 0x47, 4, 0x40, 0, 0x0b)
	}
	body = append(body, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 1})}
	if !shared {
		sections = append(sections, wasmtest.Section(5, []byte{1, 5, 1, 3}))
	}
	init := append([]byte{0x41}, wasmtest.SLEB32(113)...)
	init = append(init, 0x0b)
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, init))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("probe", 0, 2))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x23, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func rv64ReferenceImportRelay() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("alias", 0, 0))))
}

func TestRV64PublicReferenceFunctionImportsForeignOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, route := range []string{"ordinary", "static-tail", "reference-call", "reference-tail"} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-shared%t-%s", artifact, vector, shared, route), func(t *testing.T) {
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
						native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
						binding, err := native.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						rc := rv64NativeTailCompile(t, "reference-imports-relay", rv64ReferenceImportRelay(), artifact)
						var relays []*Instance
						for range 2 {
							relay := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Function("env", "target", binding))
							relays = append(relays, relay)
							binding, err = relay.ExportedFunc("alias")
							if err != nil {
								t.Fatal(err)
							}
						}
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imports-v%t-ttrue-wtrue", vector), wasmtest.ClosedReferenceImports(vector, true, true), artifact)
						calls := 0
						sharedHost := func(v int32) int32 {
							if calls%2 != 0 || v != -3 {
								panic("shared identity")
							}
							calls++
							return v + 1
						}
						owned := rv64DynamicHostBinding(t, rt, true, func(v int32) int32 {
							if calls%2 != 1 || v != -2 {
								panic("owned identity")
							}
							calls++
							return v + 2
						}, pc.importFuncSigs[2])
						producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Function("env", "native", binding).Function("env", "shared", sharedHost).Function("env", "owned", owned))
						export, err := producer.ExportedFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						cc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imports-consumer-v%t-s%t-%s", vector, shared, route), rv64ReferenceImportsConsumer(vector, shared, route), artifact)
						imports := NewImports().Function("env", "target", export)
						var memory *Memory
						if shared {
							memory, err = producer.ExportedMemory("memory0")
							if err != nil {
								t.Fatal(err)
							}
							imports.Memory("env", "memory", memory)
						}
						caller := rv64NativeTokenInstantiate(t, rt, cc, imports)
						if (caller.jm.LinMemBase() == producer.jm.LinMemBase()) != shared {
							t.Fatal("physical shared/distinct home")
						}
						if route == "reference-tail" {
							func() {
								gate := caller.lockInvocation(0)
								defer gate.unlockInvocation()
								entry, err := caller.beginNativeEntry()
								if err != nil {
									t.Fatal(err)
								}
								defer caller.unlockNativeEntry(entry)
								found := false
								for _, g := range entry.tables.targets.tailActive {
									for i, owner := range g.owners {
										if owner == producer {
											counts, _, err := rv64NativeTailOwnerCounts(owner)
											if err != nil || counts.NonCallable != 0 || counts.TargetNonCallable != 0 || counts.MaskSlots != counts.Functions || counts.Targets != counts.Functions || counts.HostSlots != counts.Functions || counts.Proxies != 1 || g.snapshots[i] == nil || g.snapshots[i].Validate() != nil {
												t.Fatal("actual sparse imported generation", counts, err)
											}
											found = true
										}
									}
								}
								if !found {
									t.Fatal("missing sparse imported producer")
								}
							}()
						}
						for _, in := range append([]*Instance{producer, native}, relays...) {
							if err := in.Close(); err != nil {
								t.Fatal(err)
							}
						}
						for _, code := range []*Compiled{pc, nc, rc} {
							if err := code.Close(); err != nil {
								t.Fatal(err)
							}
						}
						args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
						if vector {
							args = append(args, 0x80000000deadbeef)
						}
						want := append(slices.Clone(args[1:]), 9, 0)
						args = append(args, 0)
						for _, choice := range []uint64{0, 1, 2, 3, 4} {
							null := uint64(0)
							if choice == 1 {
								null = 1
							}
							args[len(args)-1] = 0xcafefeed00000000 | choice
							want[len(want)-1] = null
							before := calls
							if got, err := caller.Invoke("call", args...); err != nil || !slices.Equal(got, want) || calls != before+2 || readGlobalObject(producer.globalCells[3], ValI32) != null {
								t.Fatal("retired foreign imported reference owner", got, calls, err)
							}
							if got, err := caller.Invoke("probe"); err != nil || !slices.Equal(got, []uint64{113}) {
								t.Fatal("caller state restoration", got, err)
							}
						}
						if err := caller.Close(); err != nil {
							t.Fatal(err)
						}
						if memory != nil {
							if err := memory.Close(); err != nil {
								t.Fatal(err)
							}
						}
						for _, in := range append([]*Instance{caller, producer, native}, relays...) {
							if !in.resourcesClosed {
								t.Fatal("physical graph survived final release")
							}
						}
					})
				}
			}
		}
	}
}
