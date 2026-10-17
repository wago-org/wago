//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ImportOnlyTailOwnerModule(types []wasm.ValType) []byte {
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(im)),
		wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}))
}

func TestRV64PublicImportOnlyTailOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, kind := range []string{"go", "owned", "native", "native-vector"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, kind), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				input := []uint64{0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
				want := []uint64{0x80000001, 0x7f800123, input[2], input[3]}
				if kind == "native-vector" {
					types = append(types, wasm.V128)
					input = append(input, 0xaabbccddeeff0011, 0xffeeddccbbaa9988)
					want = append(want, input[4:]...)
				}
				key := "import-only-tail-" + kind
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				c := rv64NativeTailCompile(t, key+"-owner", rv64ImportOnlyTailOwnerModule(types), artifact)
				if len(c.Funcs) != 0 || len(c.Entry) != 0 || len(c.code) != 0 || !c.needsFuncRefDescs() {
					t.Fatal("import-only owner fabricated guest code")
				}
				calls := 0
				var binding any
				var producer *Instance
				if kind == "native" || kind == "native-vector" {
					pc := rv64NativeTailCompile(t, key+"-native", rv64PublicNativeIdentityModule(types), artifact)
					producer = rv64HostTailInstantiate(t, rt, pc, nil)
					var err error
					binding, err = producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
				} else {
					host := HostCallFunc(func(call HostCall) {
						calls++
						if !slices.Equal(call.ParamSlots(), want) {
							panic("import-only host carriers")
						}
						copy(call.ResultSlots(), input)
					})
					binding = rv64DynamicHostBinding(t, rt, kind == "owned", host, c.importFuncSigs[0])
				}
				owner := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
				table, err := owner.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				caller := rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, key+"-caller", rv64PublicDynamicTailModule(types, true), artifact), NewImports().Table("env", "table", table))
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
				if err := owner.c.Close(); err != nil {
					t.Fatal(err)
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				if producer != nil {
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
				}
				for range 3 {
					if got, err := caller.Invoke("call", append(slices.Clone(input), 0)...); err != nil || !slices.Equal(got, want) {
						t.Fatal("import-only descriptor tail", got, err)
					}
				}
				if kind == "go" || kind == "owned" {
					if calls != 3 {
						t.Fatal("import-only callback count", calls)
					}
				}
				var scope rv64NativeTableTargets
				if err := rv64TableTargetRefresh(&scope, caller); err != nil {
					t.Fatal(err)
				}
				defer scope.release()
				if err := rv64GenerationLockedForTest(func() error {
					view, err := (rv64TailCodeOwner{owner}).nativeOwnerCodeView(true)
					if err != nil || len(view) != 0 {
						return fmt.Errorf("import-only physical image: %v", err)
					}
					if _, err := (rv64TailCodeOwner{owner}).NativeTailCodeView(); !errors.Is(err, railshot.ErrUnsupported) {
						return fmt.Errorf("import-only lease became a guest frame: %v", err)
					}
					counts, _, err := rv64NativeTailOwnerCounts(owner)
					if err != nil || counts.Functions != 1 || counts.Owners != 1 {
						return fmt.Errorf("import-only owner counts: %+v %v", counts, err)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				var g *rv64NativeTailGeneration
				defer func() {
					if err := rv64GenerationLockedForTest(func() error { return g.Close() }); err != nil {
						t.Error(err)
					}
				}()
				create := func(limits rv64NativeTailGenerationLimits) error {
					return rv64GenerationLockedForTest(func() error {
						var err error
						g, err = rv64NewDefinedTailGeneration(caller, &scope, rv64NativeTailModule(caller.c).Plans, limits)
						return err
					})
				}
				if err := create(rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}); err != nil {
					t.Fatal(err)
				}
				exact := rv64NativeTailGenerationLimits{g.codeBytes, g.metadataBytes}
				if err := rv64GenerationLockedForTest(g.Close); err != nil {
					t.Fatal(err)
				}
				for _, limits := range []rv64NativeTailGenerationLimits{{exact.code - 1, exact.metadata}, {exact.code, exact.metadata - 1}} {
					if err := create(limits); g != nil || !errors.Is(err, railshot.ErrUnsupported) || scope.tailBudget.count != 0 {
						t.Fatal("import-only below exact charge allocated", err)
					}
				}
				if err := create(exact); err != nil {
					t.Fatal("import-only exact budget", err)
				}
				activate := func() error {
					return rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations([]*rv64NativeTailGeneration{g}) })
				}
				if err := activate(); err != nil {
					t.Fatal(err)
				}
				// Imported-table key lookup already allocates during reachability.
				// Keep that baseline bounded; the new snapshots add no allocations.
				baseline := testing.AllocsPerRun(4, func() {
					if err := rv64GenerationLockedForTest(scope.validateReachable); err != nil {
						panic(err)
					}
				})
				if n := testing.AllocsPerRun(4, func() {
					if err := rv64GenerationLockedForTest(func() error {
						for _, snapshot := range g.snapshots {
							if snapshot != nil {
								if err := snapshot.Validate(); err != nil {
									return err
								}
							}
						}
						return nil
					}); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("import-only snapshot validation allocated", n)
				}
				if n := testing.AllocsPerRun(4, func() {
					if err := activate(); err != nil {
						panic(err)
					}
				}); baseline > 2 || n > baseline {
					t.Fatal("import-only activation added allocations", n, baseline)
				}
			})
		}
	}
}

func TestRV64PublicImportOnlyTailParkedReentry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-owned%t", artifact, owned), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				types := []wasm.ValType{wasm.I32}
				c := rv64NativeTailCompile(t, "import-only-tail-park-owner", rv64ImportOnlyTailOwnerModule(types), artifact)
				var owner, caller *Instance
				cause := errors.New("import-only host cold trap")
				fail := false
				host := CallerHostCallFunc(func(active Caller, call HostCall) {
					view := rv64InvocationViewForTest(owner)
					before := bytes.Clone(view)
					if owner.resourcesClosed || railshotParkedNativeInvocation(owner) == 0 {
						panic("import-only parked owner retired")
					}
					if got, err := caller.InvokeFromHost(context.Background(), active, "identity", 51); err != nil || !slices.Equal(got, []uint64{51}) || !bytes.Equal(view, before) {
						panic(fmt.Sprintf("import-only nested caller %v %v", got, err))
					}
					if fail {
						panic(HostTrap{Err: cause})
					}
					call.SetI32(0, call.I32(0)+1)
				})
				binding := rv64DynamicHostBinding(t, rt, owned, host, c.importFuncSigs[0])
				owner = rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
				table, err := owner.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				cc := rv64NativeTailCompile(t, "import-only-tail-park-caller", rv64PublicDynamicTailModule(types, true), artifact)
				caller = rv64HostTailInstantiate(t, rt, cc, NewImports().Table("env", "table", table))
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
				if err := owner.c.Close(); err != nil {
					t.Fatal(err)
				}
				for _, trap := range []bool{false, true, false} {
					fail = trap
					got, err := caller.Invoke("call", 40, 0)
					if trap {
						if !errors.Is(err, cause) || len(got) != 0 {
							t.Fatal("import-only host cold trap", got, err)
						}
					} else if err != nil || !slices.Equal(got, []uint64{41}) {
						t.Fatal("import-only host resume", got, err)
					}
					if caller.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || rv64EntryCapability(caller) != 0 {
						t.Fatal("import-only reentry retained generation")
					}
				}
			})
		}
	}
}

func TestRV64PublicImportOnlyTailMixedOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			types := []wasm.ValType{wasm.I32}
			var imports [][]byte
			for _, name := range []string{"native", "go", "owned"} {
				imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0, 0))
			}
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(imports...)),
				wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 3, 0, 1, 2}))
			c := rv64NativeTailCompile(t, "import-only-tail-mixed-owner", source, artifact)
			producer := rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, "import-only-tail-mixed-native", rv64PublicNativeIdentityModule(types), artifact), nil)
			export, err := producer.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			owned := rv64DynamicHostBinding(t, rt, true, func(v int32) int32 { return v + 2 }, c.importFuncSigs[2])
			owner := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "native", export).Function("env", "go", func(v int32) int32 { return v + 1 }).Function("env", "owned", owned))
			table, err := owner.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			caller := rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, "import-only-tail-mixed-caller", rv64PublicDynamicTailModule(types, true), artifact), NewImports().Table("env", "table", table))
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			if err := owner.c.Close(); err != nil {
				t.Fatal(err)
			}
			for i := uint64(0); i < 3; i++ {
				if got, err := caller.Invoke("call", 40, i); err != nil || !slices.Equal(got, []uint64{40 + i}) {
					t.Fatal("mixed all-imports owner", i, got, err)
				}
			}
		})
	}
}

func TestRV64PublicImportOnlyTailRoot(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			types := []wasm.ValType{wasm.I32}
			pc := rv64NativeTailCompile(t, "import-only-tail-root-producer", rv64DynamicNativeImportModule(types, true, true), artifact)
			producer := rv64HostTailInstantiate(t, rt, pc, NewImports().Function("env", "target", func(v int32) int32 { return v + 1 }))
			export, err := producer.ExportedFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			params := []wasm.ValType{wasm.I32, wasm.I32}
			im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, types))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))))
			c := rv64NativeTailCompile(t, "import-only-tail-root", source, artifact)
			root := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", export))
			if err := root.c.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			for _, selector := range []uint64{0, 1, 2} {
				if got, err := root.Invoke("call", 40, selector); err != nil || !slices.Equal(got, []uint64{41}) {
					t.Fatal("empty root into dynamic host tail", got, err)
				}
			}
		})
	}
}
