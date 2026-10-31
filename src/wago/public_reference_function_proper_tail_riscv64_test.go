//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// No tables, globals, memory, or imports: the private typed parameter alone
// requires full-width descriptor homes. Numeric tail targets alternate arity.
func rv64ReferenceProperBareModule(vector bool) []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f},
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.I32})}
	functions := []byte{4, 0, 1, 2, 3}
	a := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 42, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x42}
	a = append(a, wasmtest.SLEB64(-0x123456789abcdef)...)
	a = append(a, 0xd2, 3, 0x15, 3, 0x0b, 0x0b)
	b := []byte{0x20, 1, 0x42}
	b = append(b, wasmtest.SLEB64(-0x123456789abcdef)...)
	b = append(b, 0x52, 0x04, 0x40, 0, 0x0b, 0xd2, 0, 0x20, 0, 0x12, 1, 0x0b)
	codes := [][]byte{wasmtest.Code(a), wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x15, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x04, 0x63, 0, 0xd0, 0, 0x05, 0xd2, 0, 0x0b, 0x20, 1, 0x10, 1, 0x0b}), wasmtest.Code(b)}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions[0]++
		functions = append(functions, 5)
		codes = append(codes, wasmtest.Code([]byte{0x20, 0, 0x0b}))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, functions),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("safe", 0, 0))),
		wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 3}), wasmtest.Section(10, wasmtest.Vec(codes...)))
}

func TestRV64PublicReferenceFunctionProperReferenceTailBare(t *testing.T) {
	for _, artifact := range []bool{false} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-proper-bare-v%t", vector), rv64ReferenceProperBareModule(vector), artifact)
				if c.tableCount() != 0 || len(c.Globals) != 0 || len(c.ValueTypes) != 0 || !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || !c.NeedsFuncRefDescs || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
					t.Fatal("bare private typed-tail contract")
				}
				m := rv64NativeTailModule(c)
				if m == nil || len(m.Plans) != 2 {
					t.Fatal("private/varying-arity proper plans")
				}
				for _, p := range m.Plans {
					if p.Kind != encoder.NativeTailReference || p.ReferenceBase == 0 {
						t.Fatal("reference home", p)
					}
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rv64HostCertificateCloseRuntime(t, rt)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				rv64RequirePublishedLocalFunction(t, in, 1)
				for _, null := range []uint64{0, 1, 0, 1, 0} {
					got, err := in.Invoke("run", 0xcafefeed00000000|null, 0xdeadbeef00001388)
					if null == 0 {
						if err != nil || !slices.Equal(got, []uint64{42}) {
							t.Fatal("5000 varying-arity proper tails", got, err)
						}
					} else {
						trap := ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
						if len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
							t.Fatal("bare private null origin", trap.Frames)
						}
					}
					if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || state.tableDepth != 0 || rv64EntryCapability(in) != 0 {
						t.Fatal("bare root leaked generation")
					}
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				if got, err := session.Invoke(0, 5000); err != nil || !slices.Equal(got, []uint64{42}) {
					t.Fatal("prepared private reference tail", got, err)
				}
				session.Close()
				func() {
					gate := in.lockInvocation(0)
					defer gate.unlockInvocation()
					entry, err := in.beginNativeEntry()
					if err != nil {
						t.Fatal(err)
					}
					defer in.unlockNativeEntry(entry)
					generations := entry.tables.targets.tailActive
					if len(generations) != 1 {
						t.Fatal("actual private generation")
					}
					if allocs := testing.AllocsPerRun(5, func() {
						if err := rv64ActivateNativeTailGenerations(generations); err != nil {
							panic(err)
						}
					}); allocs != 0 {
						t.Fatal("warm private generation activation", allocs)
					}
				}()
				// An artifact cannot change a call site's recorded type while
				// retaining the original compiled execution contract.
				metadata := rv64ReferenceImportTailCopy(c)
				rv64NativeTailModule(metadata).Plans[1].TypeIndex = 4

			})
		}
	}
}

func TestRV64PublicReferenceFunctionProperReferenceTailRetained(t *testing.T) {
	for _, vector := range []bool{false, true} {
		rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
		defer rv64HostCertificateCloseRuntime(t, rt)
		base := rv64NativeTailCompile(t, "proper-retained", rv64ReferenceProperBareModule(vector), false)
		for name, change := range map[string]func(*Compiled){
			"typed":          func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
			"tail":           func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall },
			"descriptors":    func(c *Compiled) { c.NeedsFuncRefDescs = false },
			"private-marker": func(c *Compiled) { c.nativeReferenceFunctionABI = false },
			"kind-indirect":  func(c *Compiled) { rv64NativeTailModule(c).Plans[1].Kind = encoder.NativeTailIndirect },
			"import":         func(c *Compiled) { rv64NativeTailModule(c).Plans[1].ImportIndex = 1 },
			"private-type":   func(c *Compiled) { rv64NativeTailModule(c).Plans[1].TypeIndex = 1 },
			"unbound-type":   func(c *Compiled) { rv64NativeTailModule(c).Plans[1].TypeIndex = uint32(len(c.Types)) },
			"target-home":    func(c *Compiled) { p := &rv64NativeTailModule(c).Plans[1]; p.ReferenceBase = p.FrameBytes },
			"argument-home":  func(c *Compiled) { p := &rv64NativeTailModule(c).Plans[1]; p.ReferenceBase = p.SourceBase },
			"unbound-actual": func(c *Compiled) { c.Funcs = slices.Clone(c.Funcs); c.Funcs[1].TypeIndex = uint32(len(c.Types)) },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				c := cloneCompiledMetadata(base)
				c.codeCache, c.validateMemo = nil, &validateMemo{}
				rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: slices.Clone(rv64NativeTailModule(base).Plans)})
				c.codeCache.stagedFeatures = base.stagedFeatures()
				change(c)
				if err := railshotValidateCompiled(c); err == nil || (name != "private-marker" && !errors.Is(err, railshot.ErrUnsupported)) {
					t.Fatal("malformed metadata accepted", err)
				}
				if err := c.prepareCodeMapping(); err != nil {
					t.Fatal(err)
				}
				cache := c.loadCodeCache()
				base, sealed, refs := cache.base, cache.sealed, cache.refs
				t.Cleanup(func() { c.Close() })
				mod, err := rt.Module(c)
				var in *Instance
				if err == nil {
					in, err = rt.Instantiate(context.Background(), mod)
					mod.Close()
				}
				if in != nil || err == nil || (name != "private-marker" && !errors.Is(err, railshot.ErrUnsupported)) || cache.base != base || cache.sealed != sealed || cache.refs != refs {
					t.Fatal("invalid retained private tail published", err, in, cache.base, base, cache.sealed, sealed, cache.refs, refs)
				}
			})
		}
	}
}
