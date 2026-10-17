//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionImmutableCellsSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 6)
}
func TestRV64PublicReferenceFunctionImmutableCellsParked(t *testing.T) {
	rv64ReferenceFunctionNumericCallsParked(t, 12)
}
func TestRV64PublicReferenceFunctionImmutableCellsTargetParked(t *testing.T) {
	rv64ReferenceFunctionNumericCallsParked(t, 13)
	rv64ReferenceFunctionNumericCallsParked(t, 14)
}
func TestRV64PublicReferenceFunctionImmutableCellsForeignOwners(t *testing.T) {
	rv64ReferenceFunctionNumericCallsForeignOwners(t, 6)
}
func TestRV64PublicReferenceFunctionImmutableCellsRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 6)
}
func TestRV64PublicReferenceFunctionImmutableCellsQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 6)
}

func TestRV64PublicReferenceFunctionImmutableCellsBare(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, null := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-null%t", artifact, vector, null), func(t *testing.T) {
					source := wasmtest.ClosedReferenceImmutableCellsBare(vector, null)
					c := rv64NativeTailCompile(t, fmt.Sprintf("immutable-bare-v%t-null%t", vector, null), source, artifact)
					if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.tableCount() != 0 || len(c.ValueTypes) != 1 || c.Globals[0].Mutable || !c.Globals[0].HasValueType || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || c.requiredFeatures.IsEnabled(CoreFeatureTailCall) || c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
						t.Fatal("immutable-only exact contract")
					}
					cfg := rv64NativeTailConfig().WithFeature(CoreFeatureTypedFunctionReferences, false)
					if !artifact {
						plain, err := cfg.Compile(source)
						if err != nil {
							t.Fatal("untyped immutable source", err)
						}
						if !bytes.Equal(plain.code, c.code) {
							t.Fatal("typed permission changed immutable code")
						}
						plain.Close()
					}
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rv64HostCertificateCloseRuntime(t, rt)
					first := rv64NativeTokenInstantiate(t, rt, c, NewImports())
					second := rv64NativeTokenInstantiate(t, rt, c, NewImports())
					for _, in := range []*Instance{first, second} {
						rows := 0
						if !null {
							rows = 1 + len(c.Funcs)
						}
						if c.needsFuncRefDescs() == null || len(in.funcRefDescs) != rows*coreruntime.FuncRefDescBytes || rows != 0 && binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.FuncRefContextOffset:]) != uint64(in.nativeContext) {
							t.Fatal("immutable header/directory extent or context", c.needsFuncRefDescs(), len(in.funcRefDescs))
						}
						for _, name := range []string{"cell", "0"} {
							if global, err := in.ExportedGlobalObject(name); global != nil || err == nil {
								t.Fatal("private immutable cell acquired exported owner", name, global, err)
							}
						}
					}
					c.Close()
					runtime.GC()
					want := uint64(0)
					if null {
						want = 1
					}
					value := readGlobalObject(first.globalCells[0], ValFuncRef)
					if null {
						if value != 0 {
							t.Fatal("immutable null bits", value)
						}
					} else {
						index, ok := first.funcrefDescriptorIndex(value)
						if !ok || index != 2 || value>>32 == 0 || bytes.Equal(first.funcRefDescs[2*40:3*40], make([]byte, 40)) {
							t.Fatal("immutable full callable descriptor", value, index, ok)
						}
					}
					for range 3 {
						importedGlobalInvoke(t, first, "run", []uint64{want})
					}
					if first.c.Globals[0].Mutable || readGlobalObject(first.globalCells[0], ValFuncRef) != value {
						t.Fatal("immutable cell changed")
					}
					first.Close()
					importedGlobalInvoke(t, second, "run", []uint64{want})
					if err := second.Close(); err != nil || second.hasPhysicalResources() {
						t.Fatal("immutable cell release", err)
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceFunctionImmutableCellsRetained(t *testing.T) {
	for _, vector := range []bool{false, true} {
		base := rv64NativeTailCompile(t, "immutable-retained", wasmtest.ClosedReferenceImmutableCellsBare(vector, false), false)
		for name, mutate := range map[string]func(*Compiled){
			"private": func(c *Compiled) { c.Globals[0].InitFunc = 1 },
			"missing": func(c *Compiled) { c.Globals[0].InitFunc = uint32(len(c.Funcs)) },
			"export":  func(c *Compiled) { c.GlobalExports = map[string]int{"cell": 0} },
			"global-copy": func(c *Compiled) {
				c.Globals[0].HasInitFunc = false
				c.Globals[0].InitFunc = 0
				c.Globals[0].HasInitGlobal = true
			},
			"expr": func(c *Compiled) {
				c.Globals[0].HasInitFunc = false
				c.Globals[0].InitFunc = 0
				c.Globals[0].InitExpr = []byte{0xd2, 2, 0x0b}
			},
			"bits":            func(c *Compiled) { c.Globals[0].Bits = 1 },
			"inactive-vector": func(c *Compiled) { c.Globals[0].V128[0] = 1 },
			"pool":            func(c *Compiled) { c.Globals[0].ValueTypeIndex = uint32(len(c.ValueTypes)) },
			"nonnullable":     func(c *Compiled) { c.ValueTypes[0].Ref.Nullable = false },
			"context":         func(c *Compiled) { c.nativeTableContextABI = false },
		} {
			t.Run(fmt.Sprintf("v%t-%s", vector, name), func(t *testing.T) {
				c := rv64ReferenceImportTailCopy(base)
				if err := c.validate(); err != nil {
					t.Fatal("unchanged immutable metadata", err)
				}
				mutate(c)
				if err := c.validate(); err == nil {
					t.Fatal("invalid immutable retained cell", err)
				}
				if name != "inactive-global" && name != "inactive-vector" {
					blob, err := c.MarshalBinary()
					if err == nil {
						loaded, err := LoadTrustedArtifact(blob)
						if loaded != nil {
							loaded.Close()
						}
						if err == nil {
							t.Fatal("invalid immutable artifact", name)
						}
					} else {
						t.Log("writer rejected invalid metadata", name, err)
					}
				}
				if err := c.prepareCodeMapping(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { c.Close() })
				cache := c.loadCodeCache()
				address, sealed, refs := cache.base, cache.sealed, cache.refs
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				mod, err := rt.Module(c)
				var in *Instance
				if err == nil {
					in, err = rt.Instantiate(context.Background(), mod)
					mod.Close()
				}
				if err == nil || in != nil || cache.base != address || cache.sealed != sealed || cache.refs != refs {
					t.Fatal("immutable rejection published RW staging", err)
				}
			})
		}
		v := rv64ExpressionMetadataCopy(base)
		v.Globals[0].InitGlobal = 1
		v.Globals[0].V128[0] = 1
		blob, err := v.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		defer loaded.Close()
		if loaded.Globals[0].Mutable || loaded.Globals[0].InitGlobal != 0 || loaded.Globals[0].V128 != (V128{}) {
			t.Fatal("immutable canonical repair/mutability")
		}
		rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
		defer rv64HostCertificateCloseRuntime(t, rt)
		in := rv64NativeTokenInstantiate(t, rt, loaded, NewImports())
		importedGlobalInvoke(t, in, "run", []uint64{0})
	}
}

func TestRV64PublicReferenceFunctionImmutableCellsBounds(t *testing.T) {
	base := rv64NativeTailCompile(t, "immutable-budget", wasmtest.ClosedReferenceImmutableCellsBare(false, true), false)
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("bytes+%d", extra), func(t *testing.T) {
			c := rv64ExpressionMetadataCopy(base)
			// Declared carriers8 + one exact immutable global18 + export3 +
			// passive data header4 =33; code is independently bounded.
			c.PassiveData = []PassiveDataInit{{Bytes: make([]byte, railshot.MaxModuleBytes-33+extra)}}
			if err := rv64ValidateReferenceFunctions(c); (err == nil) != (extra == 0) {
				t.Fatal("complete immutable aggregate", err)
			}
		})
	}
	for _, count := range []int{256, 257} {
		t.Run(fmt.Sprintf("globals%d", count), func(t *testing.T) {
			c := rv64ExpressionMetadataCopy(base)
			c.Globals = make([]GlobalDef, count)
			for i := range c.Globals {
				c.Globals[i] = base.Globals[0]
			}
			if err := rv64ValidateReferenceFunctions(c); (err == nil) != (count == 256) {
				t.Fatal("immutable global count bound", err)
			}
		})
	}
}

// Keep the mixed immutable dispatch identity distinct from its mutable saved
// carriers. The immutable cell survives callbacks, so they are not sole roots.
func rv64ReferenceImmutableCellCheck(t *testing.T, in *Instance, targetValue uint64) {
	t.Helper()
	if len(in.c.Globals) == 17 && (in.c.Globals[16].Mutable || readGlobalObject(in.globalCells[16], ValFuncRef) != targetValue) {
		t.Fatal("immutable target identity/mutability")
	}
}
