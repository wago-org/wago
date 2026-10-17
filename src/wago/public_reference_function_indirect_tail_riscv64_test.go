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
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionProperIndirectTailBare(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-indirect-bare-v%t", vector), wasmtest.ClosedReferenceBareIndirectTails(vector), artifact)
				if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
					t.Fatal("untyped indirect private contract")
				}
				m := rv64NativeTailModule(c)
				if m == nil || len(m.Plans) != 2 {
					t.Fatal("alternating-arity indirect plans")
				}
				for _, p := range m.Plans {
					if p.Kind != encoder.NativeTailIndirect || p.ReferenceBase == 0 {
						t.Fatal("full-width indirect home", p)
					}
				}
				cfg := compatibilityDefaultConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureTailCall).WithNativeStackBytes(MinNativeStackBytes)
				cfg.railshotState.tailProducts = true
				if !artifact {
					untyped, err := cfg.Compile(wasmtest.ClosedReferenceBareIndirectTails(vector))
					if err != nil {
						t.Fatal("source without typed permission", err)
					}
					t.Cleanup(func() { untyped.Close() })
					if !bytes.Equal(untyped.code, c.code) || untyped.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
						t.Fatal("typed permission changed untyped image")
					}
				}
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rv64HostCertificateCloseRuntime(t, rt)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				rv64RequirePublishedLocalFunction(t, in, 1)
				// Public arguments establish end-to-end i32 normalization. The
				// decoded backend sentinels separately cover dirty incoming carriers.
				for _, index := range []uint64{0, 1, 0, 3, 4, 5, 0xffffffff, 2, 0} {
					got, err := in.Invoke("run", 0xcafefeed00000000|index, 0xdeadbeef00001388)
					if index == 0 || index == 4 {
						if err != nil || !slices.Equal(got, []uint64{42}) {
							t.Fatal("5000 private indirect cycles", index, got, err)
						}
					} else {
						code := coreruntime.TrapIndirectOutOfBounds
						if index == 2 || index == 3 {
							code = coreruntime.TrapIndirectWrongSig
						}
						trap := ownedTableTrap(t, err, code)
						if len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
							t.Fatal("private indirect trap origin", trap.Frames)
						}
					}
					state := &in.pluginState.Load().railshotInvocation
					if state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || state.tableDepth != 0 || rv64EntryCapability(in) != 0 {
						t.Fatal("indirect root leaked generation")
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
				if got, err := session.Invoke(4, 5000); err != nil || !slices.Equal(got, []uint64{42}) {
					t.Fatal("prepared indirect cycles", got, err)
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
						t.Fatal("private indirect generation")
					}
					if allocs := testing.AllocsPerRun(5, func() {
						if err := rv64ActivateNativeTailGenerations(generations); err != nil {
							panic(err)
						}
					}); allocs != 0 {
						t.Fatal("warm indirect activation", allocs)
					}
				}()
			})
		}
	}
}

func TestRV64PublicReferenceFunctionProperIndirectTailRetained(t *testing.T) {
	for _, vector := range []bool{false, true} {
		rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
		defer rv64HostCertificateCloseRuntime(t, rt)
		base := rv64NativeTailCompile(t, "indirect-retained", wasmtest.ClosedReferenceBareIndirectTails(vector), false)
		// Funcref table metadata itself requires the directory even when the
		// optional explicit descriptor flag is absent. This is a valid shape.
		metadata := rv64ReferenceImportTailCopy(base)
		if !metadata.needsFuncRefDescs() || metadata.validate() != nil {
			t.Fatal("derived table descriptor contract")
		}
		blob, err := metadata.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { loaded.Close() })
		valid := rv64NativeTokenInstantiate(t, rt, loaded, NewImports())
		if len(valid.funcRefDescs) == 0 {
			t.Fatal("derived directory not allocated")
		}
		if got, err := valid.Invoke("run", 0, 0); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatal("derived descriptor execution", got, err)
		}

		for name, change := range map[string]func(*Compiled){
			"descriptor-witness": func(c *Compiled) { c.NeedsFuncRefDescs = false },
			"context-witness":    func(c *Compiled) { c.nativeTableReferenceABI = false },
			"tableless":          func(c *Compiled) { c.HasTable = false },
			"table64":            func(c *Compiled) { c.TableAddr64 = true },
			"import-ingress":     func(c *Compiled) { c.tableImport = "env.table" },
			"export-ingress":     func(c *Compiled) { c.tableExports = map[string]int{"escape": 0} },
			"private-type":       func(c *Compiled) { rv64NativeTailModule(c).Plans[1].TypeIndex = 1 },
			"unbound-type":       func(c *Compiled) { rv64NativeTailModule(c).Plans[1].TypeIndex = uint32(len(c.Types)) },
			"inactive-import":    func(c *Compiled) { rv64NativeTailModule(c).Plans[1].ImportIndex = 1 },
			"home-overlap":       func(c *Compiled) { p := &rv64NativeTailModule(c).Plans[1]; p.ReferenceBase = p.SourceBase },
			"home-outside":       func(c *Compiled) { p := &rv64NativeTailModule(c).Plans[1]; p.ReferenceBase = p.FrameBytes },
			"descriptors":        func(c *Compiled) { c.NeedsFuncRefDescs = false; c.TableType = ValExternRef },
			"kind-reference":     func(c *Compiled) { rv64NativeTailModule(c).Plans[1].Kind = encoder.NativeTailReference },
			"unbound-actual":     func(c *Compiled) { c.Funcs = slices.Clone(c.Funcs); c.Funcs[1].TypeIndex = uint32(len(c.Types)) },
			"tail-feature":       func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				c := rv64ReferenceImportTailCopy(base)
				change(c)
				if err := c.validate(); err == nil {
					t.Fatal("malformed indirect metadata accepted")
				}
				if err := c.prepareCodeMapping(); err != nil {
					t.Fatal(err)
				}
				cache := c.loadCodeCache()
				address, sealed, refs := cache.base, cache.sealed, cache.refs
				t.Cleanup(func() { c.Close() })
				mod, err := rt.Module(c)
				var in *Instance
				if err == nil {
					in, err = rt.Instantiate(context.Background(), mod)
					mod.Close()
				}
				if err == nil || in != nil || !errors.Is(err, railshot.ErrUnsupported) || cache.base != address || cache.sealed != sealed || cache.refs != refs {
					t.Fatal("bad indirect metadata published", err, cache.base, address, cache.sealed, sealed, cache.refs, refs)
				}
			})
		}
	}
}
