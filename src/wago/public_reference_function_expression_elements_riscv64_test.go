//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionExpressionElementsSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 5)
}
func TestRV64PublicReferenceFunctionExpressionElementsParked(t *testing.T) {
	rv64ReferenceFunctionNumericCallsParked(t, 9)
}
func TestRV64PublicReferenceFunctionExpressionElementsTargetParked(t *testing.T) {
	rv64ReferenceFunctionNumericCallsParked(t, 10)
	rv64ReferenceFunctionNumericCallsParked(t, 11)
}
func TestRV64PublicReferenceFunctionExpressionElementsForeignOwners(t *testing.T) {
	rv64ReferenceFunctionNumericCallsForeignOwners(t, 5)
}
func TestRV64PublicReferenceFunctionExpressionElementsRetiredRelays(t *testing.T) {
	rv64ReferenceFunctionNumericCallsRetiredRelays(t, 5)
}
func TestRV64PublicReferenceFunctionExpressionElementsQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 5)
}

func rv64ExpressionReplaceSection(t *testing.T, source []byte, section byte, replacement []byte) []byte {
	t.Helper()
	r := wasm.ReaderFrom(source[8:])
	sections := [][]byte{}
	found := false
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			t.Fatal(err)
		}
		n, err := r.U32()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := r.Bytes(int(n))
		if err != nil {
			t.Fatal(err)
		}
		if id == section {
			payload = replacement
			found = true
		}
		sections = append(sections, wasmtest.Section(id, payload))
	}
	if !found {
		t.Fatal("missing fixture section", section)
	}
	return wasmtest.Module(sections...)
}

func rv64PrivateExpressionElementsModule(t *testing.T, vector bool) []byte {
	t.Helper()
	elements := make([][]byte, 256)
	elements[0] = []byte{4, 0x41, 0, 0x0b, 1, 0xd2, 0, 0x0b}
	elements[1] = []byte{7, 0x70, 1, 0xd2, 1, 0x0b}
	elements[2] = []byte{5, 0x70, 3, 0xd2, 1, 0x0b, 0xd0, 0x70, 0x0b, 0xd2, 0, 0x0b}
	// Explicit nullable spelling is the same canonical untyped funcref; this
	// empty active segment still performs its offset range check at construction.
	elements[3] = []byte{6, 0, 0x41, 0, 0x0b, 0x63, 0x70, 0}
	for i := 4; i < 255; i++ {
		elements[i] = []byte{7, 0x70, 0}
	}
	elements[255] = []byte{5, 0x70, 1, 0xd2, 1, 0x0b}
	source := rv64ExpressionReplaceSection(t, privateTableElementsModule(), 9, wasmtest.Vec(elements...))
	return rv64ReferenceTableBulkPrivateModule(t, source, vector)
}

func TestRV64PublicReferenceFunctionExpressionElementsRows(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("expression-rows-v%t", vector), rv64PrivateExpressionElementsModule(t, vector), artifact)
				if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
					t.Fatal("untyped literal witnesses")
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				first := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				second := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				c.Close()
				runtime.GC()
				if len(first.c.passiveElems) != 256 || !first.c.Elems[0].HasValueType || !first.c.Elems[1].HasValueType || !first.c.passiveElems[255].HasValueType {
					t.Fatal("exact expression type/original slots")
				}
				// Seed row1 with a complete nonzero numeric row before null init.
				importedGlobalInvoke(t, first, "init-last", nil, 1, 0, 1)
				if bytes.Equal(privateTableBytes(first)[40:72], make([]byte, 32)) {
					t.Fatal("null overwrite oracle not seeded")
				}
				importedGlobalInvoke(t, first, "init-passive", nil, 0xfeedface00000000, 0xdeadbeef00000000, 0xcafebabe00000003)
				assertPrivateTableTargets(t, first, 14, 0, 12, 0)
				if !bytes.Equal(privateTableBytes(first)[40:72], make([]byte, coreruntime.TableEntryBytes)) {
					t.Fatal("partial null row replacement")
				}
				importedGlobalInvoke(t, first, "call", []uint64{14}, 0, 5)
				trap := ownedTableTrap(t, invokePrivateReference(t, first, "call", 1, 5), coreruntime.TrapIndirectOutOfBounds)
				rv64ExpressionTrapOrigin(t, trap, 13, 5)
				importedGlobalInvoke(t, first, "call", []uint64{12}, 2, 5)
				before := slices.Clone(privateTableBytes(first))
				for _, args := range [][]uint64{{0, 3, 1}, {2, 0, 3}, {5, 0, 0}} {
					trap := ownedTableTrap(t, invokePrivateReference(t, first, "init-passive", args...), coreruntime.TrapTableOutOfBounds)
					rv64ExpressionTrapOrigin(t, trap, 16, 7)
					if !slices.Equal(before, privateTableBytes(first)) {
						t.Fatal("failed expression range wrote rows")
					}
				}
				importedGlobalInvoke(t, first, "drop-passive", nil)
				importedGlobalInvoke(t, first, "drop-passive", nil)
				ownedTableTrap(t, invokePrivateReference(t, first, "init-passive", 0, 0, 1), coreruntime.TrapTableOutOfBounds)
				importedGlobalInvoke(t, second, "init-passive", nil, 0, 0, 3)
				assertPrivateTableTargets(t, second, 14, 0, 12, 0)
				for _, name := range []string{"init-active", "init-declarative", "init-empty-active"} {
					importedGlobalInvoke(t, first, name, nil, 4, 0, 0)
					ownedTableTrap(t, invokePrivateReference(t, first, name, 0, 0, 1), coreruntime.TrapTableOutOfBounds)
				}
				if err := first.Close(); err != nil {
					t.Fatal(err)
				}
				importedGlobalInvoke(t, second, "call", []uint64{14}, 0, 5)
				if err := second.Close(); err != nil || second.hasPhysicalResources() {
					t.Fatal("expression resources retained", err)
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionExpressionElementsDeclarations(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, drop := range []bool{false, true} {
				for _, trailing := range []bool{false, true} {
					if trailing && !drop {
						continue
					}
					for _, payload := range []int{0, 1, 2} {
						t.Run(fmt.Sprintf("artifact%t-vector%t-drop%t-trailing%t-p%d", artifact, vector, drop, trailing, payload), func(t *testing.T) {
							source := wasmtest.ClosedReferenceDeclarativeExpressions(vector, drop, payload, trailing)
							c := rv64NativeTailCompile(t, fmt.Sprintf("expression-decl-v%t-drop%t-trailing%t-p%d", vector, drop, trailing, payload), source, artifact)
							pool, slots := 0, 0
							if drop {
								pool, slots = 1, 256
								if trailing {
									pool, slots = 0, 255
								}
							}
							if len(c.ValueTypes) != pool || len(c.passiveElems) != slots || c.tableCount() != 0 || len(c.Globals) != 0 || c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || c.requiredFeatures.IsEnabled(CoreFeatureTailCall) || !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
								t.Fatal("declaration-only retained pool/witness", len(c.ValueTypes), len(c.passiveElems), c.requiredFeatures)
							}
							cfg := rv64NativeTailConfig().WithFeature(CoreFeatureTypedFunctionReferences, false)
							if !artifact {
								plain, err := cfg.Compile(source)
								if err != nil {
									t.Fatal("untyped source permission", err)
								}
								plain.Close()
							}
							rt := NewRuntime(WithRuntimeConfig(cfg))
							defer rv64HostCertificateCloseRuntime(t, rt)
							first := rv64NativeTokenInstantiate(t, rt, c, NewImports())
							second := rv64NativeTokenInstantiate(t, rt, c, NewImports())
							c.Close()
							runtime.GC()
							var directory []byte
							if drop {
								directory = unsafe.Slice((*byte)(offHeapPtr(first.jm.PassiveElemPtr())), slots*16)
								for i := range slots {
									if binary.LittleEndian.Uint32(directory[i*16+8:]) != 0 {
										t.Fatal("declarative slot live", i)
									}
								}
								binary.LittleEndian.PutUint32(directory[(slots-1)*16+12:], 0x11223344)
							}
							before := slices.Clone(directory)
							for range 3 {
								importedGlobalInvoke(t, first, "run", []uint64{42})
							}
							if !slices.Equal(before, directory) {
								t.Fatal("repeated drop touched descriptor pointer/padding")
							}
							first.Close()
							importedGlobalInvoke(t, second, "run", []uint64{42})
							if err := second.Close(); err != nil || second.hasPhysicalResources() {
								t.Fatal("tableless drop resources", err)
							}
						})
					}
				}
			}
		}
	}
}

func rv64ExpressionMetadataCopy(c *Compiled) *Compiled {
	v := cloneCompiledMetadata(c.executionView())
	indexes := *c.loadCompileIndexes()
	v.validateMemo = &validateMemo{compileIndexes: &indexes, importModuleEnds: slices.Clone(c.loadValidateMemo().importModuleEnds)}
	v.codeCache = nil
	v.ensureCodeCache()
	v.codeCache.stagedFeatures = c.stagedFeatures()
	v.Elems = slices.Clone(c.Elems)
	v.passiveElems = slices.Clone(c.passiveElems)
	v.ValueTypes = slices.Clone(c.ValueTypes)
	for i := range v.Elems {
		v.Elems[i].Values = slices.Clone(c.Elems[i].Values)
	}
	for i := range v.passiveElems {
		v.passiveElems[i].Values = slices.Clone(c.passiveElems[i].Values)
	}
	return v
}

func TestRV64PublicReferenceFunctionExpressionElementsRetained(t *testing.T) {
	for _, vector := range []bool{false, true} {
		base := rv64NativeTailCompile(t, "expression-retained", rv64PrivateExpressionElementsModule(t, vector), false)
		baseline, err := base.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		valid, err := LoadTrustedArtifact(baseline)
		if err != nil || valid == nil {
			t.Fatal("valid expression artifact", err)
		}
		valid.Close()
		for name, mutate := range map[string]func(*Compiled){
			"active-pool":     func(c *Compiled) { c.Elems[0].ValueTypeIndex = uint32(len(c.ValueTypes)) },
			"passive-pool":    func(c *Compiled) { c.passiveElems[2].ValueTypeIndex = uint32(len(c.ValueTypes)) },
			"dropped-pool":    func(c *Compiled) { c.passiveElems[1].ValueTypeIndex = uint32(len(c.ValueTypes)) },
			"wrong-type":      func(c *Compiled) { c.Elems[0].RefType = ValExternRef },
			"legacy-null":     func(c *Compiled) { c.passiveElems[2].HasValueType = false; c.passiveElems[2].ValueTypeIndex = 0 },
			"null-func":       func(c *Compiled) { c.passiveElems[2].Values[1].FuncIndex = 1 },
			"null-global":     func(c *Compiled) { c.passiveElems[2].Values[1].GlobalIndex = 1 },
			"global":          func(c *Compiled) { c.passiveElems[2].Values[0].HasGlobal = true },
			"i31":             func(c *Compiled) { c.passiveElems[2].Values[0].I31Wrap = true },
			"repeat":          func(c *Compiled) { c.passiveElems[2].Values[0].RepeatPrevious = true },
			"expr":            func(c *Compiled) { c.passiveElems[2].Values[0].Expr = []byte{0xd2, 0, 0x0b} },
			"private":         func(c *Compiled) { c.passiveElems[2].Values[0].FuncIndex = 13 },
			"missing":         func(c *Compiled) { c.Elems[0].Values[0].FuncIndex = uint32(c.NumImports + len(c.Funcs)) },
			"dropped-payload": func(c *Compiled) { c.passiveElems[1].Values = []RefInit{{Null: true}} },
			"bulk-witness":    func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
			"inactive-offset": func(c *Compiled) { c.passiveElems[2].Offset.Base = 1 },
			"inactive-table":  func(c *Compiled) { c.passiveElems[2].TableIndex = 1 },
		} {
			t.Run(fmt.Sprintf("v%t-%s", vector, name), func(t *testing.T) {
				c := rv64ExpressionMetadataCopy(base)
				mutate(c)
				if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("malformed expression accepted", err)
				}
				// These fields survive canonical encoding; exercise actual decode
				// rejection. Inactive null fields are tested as safe repair below.
				switch name {
				case "private", "missing", "legacy-null", "dropped-payload", "inactive-table", "inactive-offset", "active-pool", "passive-pool", "dropped-pool", "wrong-type":
					blob, err := c.MarshalBinary()
					if err != nil {
						// Shared metadata validation rejects these before writing.
						switch name {
						case "missing", "active-pool", "passive-pool", "dropped-pool", "wrong-type":
							t.Log("writer rejected persistent metadata", name)
						default:
							t.Fatal("unexpected mutation writer rejection", name, err)
						}
					} else {
						loaded, err := LoadTrustedArtifact(blob)
						if loaded != nil {
							loaded.Close()
						}
						if err == nil {
							t.Fatal("persistent invalid expression artifact decoded", name)
						}
					}
				case "bulk-witness":
					// The writer derives bulk permission from segment state. This is
					// canonical repair, while direct missing-witness metadata fails.
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					if !loaded.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) || railshotValidateCompiled(loaded) != nil {
						t.Fatal("bulk witness repair")
					}
					loaded.Close()
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
				if err == nil || in != nil || !errors.Is(err, railshot.ErrUnsupported) || cache.base != address || cache.sealed != sealed || cache.refs != refs {
					t.Fatal("rejected expression published/retained staging", err)
				}
			})
		}
		// Null's codec tag discards inactive fields. Reject direct malformed
		// metadata, but accept independently validated safe canonical wire repair.
		v := rv64ExpressionMetadataCopy(base)
		v.passiveElems[2].Values[1].FuncIndex = 1
		v.passiveElems[2].Values[1].GlobalIndex = 2
		if err := railshotValidateCompiled(v); err == nil {
			t.Fatal("direct null inactive fields")
		}
		blob, err := v.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		defer loaded.Close()
		null := loaded.passiveElems[2].Values[1]
		if !null.Null || null.FuncIndex != 0 || null.HasGlobal || null.GlobalIndex != 0 || null.I31Wrap || null.RepeatPrevious || len(null.Expr) != 0 {
			t.Fatal("null codec did not canonicalize")
		}
		in := rv64ReferenceTableBulkInstantiate(t, loaded)
		importedGlobalInvoke(t, in, "init-last", nil, 1, 0, 1)
		importedGlobalInvoke(t, in, "init-passive", nil, 0, 0, 3)
		if !bytes.Equal(privateTableBytes(in)[40:72], make([]byte, 32)) {
			t.Fatal("repaired null not executed")
		}
	}
}

func TestRV64PublicReferenceFunctionExpressionElementsBounds(t *testing.T) {
	base := rv64NativeTailCompile(t, "expression-bounds", rv64PrivateExpressionElementsModule(t, false), false)
	for _, count := range []int{railshot.MaxOwnedTableEntries, railshot.MaxOwnedTableEntries + 1} {
		t.Run(fmt.Sprintf("values%d", count), func(t *testing.T) {
			c := rv64ExpressionMetadataCopy(base)
			c.Elems[0].HasValueType, c.Elems[0].ValueTypeIndex = false, 0
			c.Elems[0].Values = make([]RefInit, 512)
			c.passiveElems[2].Values = make([]RefInit, count-512)
			for i := range c.passiveElems[2].Values {
				c.passiveElems[2].Values[i].Null = true
			}
			c.passiveElems[255].Values = nil
			if err := rv64ValidateReferenceFunctions(c); (err == nil) != (count == railshot.MaxOwnedTableEntries) {
				t.Fatal("retained combined legacy/expression bound", err)
			}
		})
	}
	for _, hasTable := range []bool{false, true} {
		for _, extra := range []int{0, 1} {
			t.Run(fmt.Sprintf("table%t-bytes+%d", hasTable, extra), func(t *testing.T) {
				c := rv64ExpressionMetadataCopy(base)
				c.Elems = nil
				c.passiveElems = []ElemInit{{Mode: ElemModeDeclarative, RefType: ValFuncRef, HasValueType: true, ValueTypeIndex: c.TableValueTypeIndex}}
				remaining := 20 - extra // expression slot16 + exact pool binding4
				if hasTable {
					remaining += 21
				} else {
					c.HasTable = false
					c.TableHasValueType = false
					c.TableValueTypeIndex = 0
				}
				if err := rv64ValidateReferenceFunctionTable(c, &remaining); (err == nil) != (extra == 0) || extra == 0 && remaining != 0 {
					t.Fatal("retained expression header aggregate", err, remaining)
				}
			})
		}
	}
	// Declaration-only retained state charges its exact binding even without a
	// table/global. A payload follows the same aggregate1MiB budget.
	decl := rv64NativeTailCompile(t, "expression-decl-budget", wasmtest.ClosedReferenceDeclarativeExpressions(false, true, 1), false)
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("complete-bytes+%d", extra), func(t *testing.T) {
			c := rv64ExpressionMetadataCopy(decl)
			// Declared carriers8 +256 exact expression slots5120 + export3
			// + passive data header4 =5135. Native code has a separate bound.
			c.PassiveData = []PassiveDataInit{{Bytes: make([]byte, railshot.MaxModuleBytes-5135+extra)}}
			if err := rv64ValidateReferenceFunctions(c); (err == nil) != (extra == 0) {
				t.Fatal("complete retained expression aggregate", err)
			}
		})
	}
}

func rv64ExpressionTrapOrigin(t *testing.T, trap *coreruntime.TrapError, function uint32, pc uint32) {
	t.Helper()
	if len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != function || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
		t.Fatal("expression trap source", trap.Frames, function, pc)
	}
}

func TestRV64PublicReferenceFunctionExpressionElementsFeatureGates(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("v%t", vector), func(t *testing.T) {
			cfg := rv64NativeTailConfig().WithFeature(CoreFeatureTypedFunctionReferences, false)
			source := rv64PrivateExpressionElementsModule(t, vector)
			c, err := cfg.Compile(source)
			if err != nil {
				t.Fatal("explicit nullable literal without typed permission", err)
			}
			c.Close()
			for _, source := range [][]byte{source, wasmtest.ClosedReferenceDeclarativeExpressions(vector, true, 1)} {
				c, err := cfg.WithFeature(CoreFeatureBulkMemoryOperations, false).Compile(source)
				if c != nil {
					c.Close()
				}
				if c != nil || err == nil {
					t.Fatal("expression source bulk feature gate", err)
				}
			}
			decl := rv64NativeTailCompile(t, "expression-empty-tableless", wasmtest.ClosedReferenceDeclarativeExpressions(vector, true, 0), false)
			for _, passive := range []bool{false, true} {
				v := rv64ExpressionMetadataCopy(decl)
				if passive {
					v.passiveElems[255].Mode = ElemModePassive
				} else {
					v.passiveElems = nil // exact pool no longer has a justified binding
				}
				if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("tableless passive/unused exact type admitted", passive, err)
				}
				blob, err := v.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("tableless passive/unused type artifact admitted", passive)
				}
			}
		})
	}
}
