//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64DynamicMixedHostModule() []byte {
	var imports [][]byte
	for _, name := range []string{"native", "go", "owned"} {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0, 0))
	}
	var body []byte
	for selector := byte(3); selector < 9; selector++ {
		body = append(body, 0x20, 1, 0x41, selector, 0x46, 0x04, 0x7f, 0x20, 0)
		if selector < 6 {
			body = append(body, 0x12, selector-3)
		} else {
			body = append(body, 0xd2, selector-6, 0x15, 0)
		}
		body = append(body, 0x05)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x13, 0, 0)
	for range 7 {
		body = append(body, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(4, []byte{1, 0x70, 0, 3}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 3))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 3, 0, 1, 2}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicDynamicHostTailMixedOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			pc := rv64NativeTailCompile(t, "dynamic-host-mixed-producer", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
			producer := rv64HostTailInstantiate(t, rt, pc, nil)
			export, err := producer.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			c := rv64NativeTailCompile(t, "dynamic-host-mixed-imports", rv64DynamicMixedHostModule(), artifact)
			owned := rv64DynamicHostBinding(t, rt, true, func(v int32) int32 { return v + 2 }, c.importFuncSigs[2])
			in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "native", export).Function("env", "go", func(v int32) int32 { return v + 1 }).Function("env", "owned", owned))
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := pc.Close(); err != nil {
				t.Fatal(err)
			}
			for selector := uint64(0); selector < 9; selector++ {
				if got, err := in.Invoke("call", 40, selector); err != nil || !slices.Equal(got, []uint64{40 + selector%3}) {
					t.Fatal("mixed native/shared/owned direct/indirect/reference", selector, got, err)
				}
			}
		})
	}
}

func rv64DynamicHostGrowthModule(replacement bool) []byte {
	types := []wasm.ValType{wasm.I32}
	table := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 3, 3)
	imports := [][]byte{table}
	function := uint32(0)
	body := []byte{0x20, 0, 0x41, 0, 0x11, 0, 0, 0x1a, 0x20, 0, 0x41, 0, 0x13, 0, 0, 0x0b}
	if replacement {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))
		body, function = []byte{0x20, 0, 0x0b}, 1
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, function)))}
	if replacement {
		sections = append(sections, wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 3, 0, 1, 1}))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))...)
}

func TestRV64PublicDynamicHostTailParkedGraphGrowth(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-owned%t", artifact, owned), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				types := []wasm.ValType{wasm.I32}
				pc := rv64NativeTailCompile(t, "dynamic-host-growth-producer", rv64DynamicNativeImportModule(types, true, true), artifact)
				cc := rv64NativeTailCompile(t, "dynamic-host-growth-caller", rv64DynamicHostGrowthModule(false), artifact)
				rc := rv64NativeTailCompile(t, "dynamic-host-growth-replacement", rv64DynamicHostGrowthModule(true), artifact)
				rmod, err := rt.Module(rc)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = rmod.Close() })
				var producer, caller, replacement *Instance
				var table *Table
				calls := 0
				second := rv64DynamicHostBinding(t, rt, owned, func(v int32) int32 {
					calls++
					scope := railshotCurrentNativeTableTargets(caller).targets
					if scope == nil || len(scope.tailGenerations) <= len(scope.tailActive) || producer.resourcesClosed {
						panic("resumed graph lost old host generation")
					}
					return v + 2
				}, rc.importFuncSigs[0])
				first := rv64DynamicHostBinding(t, rt, owned, func(v int32) int32 {
					calls++
					var err error
					// A genuine active initializer replaces the imported table row
					// while the first callback's code and owner remain parked.
					replacement, err = rt.Instantiate(context.Background(), rmod, WithImports(NewImports().Table("env", "table", table).Function("env", "target", second)))
					if err != nil {
						panic(err)
					}
					if err := producer.Close(); err != nil {
						panic(err)
					}
					if err := producer.c.Close(); err != nil {
						panic(err)
					}
					return v + 1
				}, pc.importFuncSigs[0])
				producer = rv64HostTailInstantiate(t, rt, pc, NewImports().Function("env", "target", first))
				table, err = producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				caller = rv64HostTailInstantiate(t, rt, cc, NewImports().Table("env", "table", table))
				if got, err := caller.Invoke("call", 40); err != nil || !slices.Equal(got, []uint64{42}) || calls != 2 || replacement == nil {
					t.Fatal("parked host graph replacement", got, calls, err)
				}
				if state := &caller.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.tableDepth != 0 || rv64EntryCapability(caller) != 0 {
					t.Fatal("host graph growth retained entry generations")
				}
			})
		}
	}
}

func TestRV64PublicDynamicHostTailDeepPrepared(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			for _, reference := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-owned%t-reference%t", artifact, owned, reference), func(t *testing.T) {
					types := []wasm.ValType{wasm.I32}
					edge := func(index byte) []byte {
						if reference {
							return []byte{0xd2, index, 0x15, 0}
						}
						return []byte{0x41, index, 0x13, 0, 0}
					}
					body := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 47}
					body = append(body, edge(0)...)
					body = append(body, 0x05, 0x20, 0, 0x41, 1, 0x6b)
					body = append(body, edge(1)...)
					body = append(body, 0x0b, 0x0b)
					im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
					source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(im)),
						wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
						wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 2, 0, 1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-deep-reference%t", reference), source, artifact)
					c.codeCache.setNativeStackBytes(MinNativeStackBytes)
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
					defer rv64HostCertificateCloseRuntime(t, rt)
					calls := 0
					binding := rv64DynamicHostBinding(t, rt, owned, func(v int32) int32 { calls++; return v + 1 }, c.importFuncSigs[0])
					in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
					fn, err := in.WasmFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					prepared, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer prepared.Close()
					if got, err := prepared.Invoke(20000); err != nil || !slices.Equal(got, []uint64{48}) || calls != 1 {
						t.Fatal("20000 dynamic tails ending at host", got, calls, err)
					}
				})
			}
		}
	}
}
