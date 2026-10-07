//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ExpressionSegment(flags, offset byte, values ...[]byte) []byte {
	e := []byte{flags}
	if flags == 6 {
		e = append(e, 0)
	}
	if flags == 4 || flags == 6 {
		e = append(e, 0x41, offset, 0x0b)
	}
	if flags != 4 {
		e = append(e, 0x70)
	}
	e = append(e, wasmtest.Vec(values...)...)
	return e
}

func rv64ExpressionElementModule(types []wasm.ValType, flags byte, borrowed bool) []byte {
	callerTypes := append(slices.Clone(types), wasm.I32)
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0)}
	if borrowed {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 2))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(callerTypes, types), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{6, 1, 2, 0, 3, 3, 2})}
	if !borrowed {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 2, 2}))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("install", 0, 2), wasmtest.ExportEntry("local", 0, 3), wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("setlocal", 0, 5), wasmtest.ExportEntry("safe", 0, 6), wasmtest.ExportEntry("table", 1, 0))), wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(flags, 0, []byte{0xd2, 0, 0x0b}, []byte{0xd0, 0x70, 0x0b}))))
	var caller, local []byte
	for i := range types {
		caller = append(caller, 0x20, byte(i))
		local = append(local, 0x20, byte(i))
		if i == 0 {
			local = append(local, 0x41, 9, 0x6a)
		}
	}
	caller = append(caller, 0x20, byte(len(types)), 0x11, 0, 0, 0x0b)
	local = append(local, 0x0b)
	install := []byte{0x41, 0, 0x41, 0, 0x41, 2, 0xfc, 12, 0, 0, 0xfc, 13, 0, 0xfc, 16, 0, 0x0b}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(install), wasmtest.Code(local), wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd2, 3, 0x26, 0, 0xfc, 16, 0, 0x0b}), wasmtest.Code([]byte{0x41, 9, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestRV64PublicExpressionElements(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, flags := range []byte{4, 5, 6, 7} {
			for _, borrowed := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-flags%d-borrowed%t-artifact%t", vector, flags, borrowed, artifact), func(t *testing.T) {
						types := []wasm.ValType{wasm.I32, wasm.F32, wasm.F64}
						if vector {
							types = append(types, wasm.V128)
						}
						key := fmt.Sprintf("expr-types%d-flags%d-borrowed%t", len(types), flags, borrowed)
						p := rv64TableStageCompile(t, key, rv64ExpressionElementModule(types, flags, borrowed), artifact)
						c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, false, false, true, 2), artifact)
						v := p.executionView()
						plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
						if !v.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) || len(v.passiveElems) != 1 {
							t.Fatal("element feature/index metadata")
						}
						for _, e := range append(slices.Clone(v.Elems), v.passiveElems...) {
							exact, err := v.elemExactType(e)
							if err != nil || !e.HasValueType || exact != plain {
								t.Fatal("lost exact nullable segment type", err)
							}
						}
						imports := NewImports()
						var table *Table
						var leaf *Instance
						if borrowed {
							var err error
							table, err = NewTable(2, 2)
							if err != nil {
								t.Fatal(err)
							}
							defer table.Close()
							imports.Table("env", "table", table)
						}
						if vector {
							n := rv64TableStageCompile(t, key+"-leaf", rv64PublicNativeIdentityModule(types), artifact)
							var err error
							leaf, err = Instantiate(n, NewImports())
							if err != nil {
								t.Fatal(err)
							}
							defer leaf.Close()
							ex, err := leaf.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							imports.Function("env", "host", ex)
						} else {
							imports.Function("env", "host", HostCallFunc(func(call HostCall) { copy(call.ResultSlots(), call.ParamSlots()) }))
						}
						producer, err := Instantiate(p, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						if !borrowed {
							table, err = producer.ExportedTable("table")
							if err != nil {
								t.Fatal(err)
							}
						}
						consumer, err := Instantiate(c, NewImports().Table("env", "table", table))
						if err != nil {
							t.Fatal(err)
						}
						defer consumer.Close()
						input, want := rv64NativeVectorInputs(types)
						if flags == 5 {
							if _, err := producer.Invoke("install"); err != nil {
								t.Fatal("passive init", err)
							}
						} else if _, err := producer.Invoke("install"); err == nil {
							t.Fatal("active/declarative payload not dropped")
						}
						if flags != 7 {
							if got, err := producer.Invoke("call", append(slices.Clone(input), 0)...); err != nil || !slices.Equal(got, want) {
								t.Fatal("expression imported reference", got, err)
							}
						}
						_, nullErr := producer.Invoke("call", append(slices.Clone(input), 1)...)
						var nullTrap *coreruntime.TrapError
						if !errors.As(nullErr, &nullTrap) || nullTrap.Code != coreruntime.TrapIndirectOutOfBounds || !bytes.Equal(table.desc[8+coreruntime.TableEntryBytes:8+2*coreruntime.TableEntryBytes], make([]byte, coreruntime.TableEntryBytes)) {
							t.Fatal("null expression row/trap", nullErr)
						}
						if _, err := producer.Invoke("setlocal", 1); err != nil {
							t.Fatal(err)
						}
						want[0] = uint64(uint32(want[0] + 9))
						want = append(want, 7)
						_ = producer.Close()
						_ = p.Close()
						if leaf != nil {
							_ = leaf.Close()
						}
						if flags != 7 {
							_, raw := rv64NativeVectorInputs(types)
							if got, err := consumer.Invoke("call", append(slices.Clone(input), 0)...); err != nil || !slices.Equal(got, append(raw, 7)) {
								t.Fatal("retired expression import", got, err)
							}
						}
						if got, err := consumer.Invoke("call", append(slices.Clone(input), 1)...); err != nil || !slices.Equal(got, want) {
							t.Fatal("retired expression table", got, err)
						}
						for _, i := range []uint64{0, 1} {
							if _, err := consumer.Invoke("clear", i); err != nil {
								t.Fatal(err)
							}
						}
						if borrowed && (!producer.resourcesClosed || leaf != nil && !leaf.resourcesClosed) {
							t.Fatal("expression table retained final owner")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicExpressionElementArtifactAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source metadata mutation fixture")
	}
	source := rv64ExpressionElementModule([]wasm.ValType{wasm.I32}, 4, false)
	if c, err := Compile(ownedTableConfig().WithFeature(CoreFeatureReferenceTypes, false), source); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("disabled reference types accepted", err)
	}
	c := rv64TableStageCompile(t, "expr-artifact-admission", source, false)
	for index, mutate := range []func(*Compiled){
		func(v *Compiled) { v.Elems[0].HasValueType = false },
		func(v *Compiled) { v.Elems[0].ValueTypeIndex = ^uint32(0) },
		func(v *Compiled) { v.ValueTypes[v.Elems[0].ValueTypeIndex].Ref.Nullable = false },
		func(v *Compiled) { v.Elems[0].Values[1].FuncIndex = 1 },
		func(v *Compiled) { v.Elems[0].Values[0].HasGlobal = true },
		func(v *Compiled) { v.Elems[0].Values[0].Expr = []byte{0xd2, 0, 0x0b} },
		func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
	} {
		v := *c.executionView()
		v.Elems = slices.Clone(v.Elems)
		v.Elems[0].Values = slices.Clone(v.Elems[0].Values)
		v.ValueTypes = slices.Clone(v.ValueTypes)
		mutate(&v)
		if err := railshotValidateCompiled(&v); err == nil {
			t.Fatal("unqualified expression metadata admitted")
		}
		blob, err := marshalCompiled(&v)
		if err != nil {
			continue // Structural metadata validation may reject before serialization.
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err == nil && (index == 3 || index == 6) {
			// The codec stores null as a tag without an unused function index,
			// and derives feature requirements from structural metadata.
			repaired := loaded.executionView()
			if err := railshotValidateCompiled(repaired); err != nil || !repaired.Elems[0].Values[1].Null || repaired.Elems[0].Values[1].FuncIndex != 0 || !repaired.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
				loaded.Close()
				t.Fatal("codec did not canonicalize expression metadata", index, err)
			}
			loaded.Close()
			continue
		}
		if loaded != nil {
			loaded.Close()
		}
		if err == nil {
			t.Fatal("unqualified expression artifact admitted", index)
		}
	}
}

func TestRV64PublicExpressionElementPreflight(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			types := []wasm.ValType{wasm.I32}
			source := rv64ExpressionElementModule(types, 4, true)
			p := rv64TableStageCompile(t, "expr-preflight-base", source, artifact)
			// Earlier segments persist when a later segment is out of bounds.
			bad := rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(4, 0, []byte{0xd0, 0x70, 0x0b}), rv64ExpressionSegment(6, 2, []byte{0xd2, 0, 0x0b}))))
			b := rv64TableStageCompile(t, "expr-preflight-bad", bad, artifact)
			table, err := NewTable(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			imports := NewImports().Table("env", "table", table).Function("env", "host", func(v int32) int32 { return v + 1 })
			in, err := Instantiate(p, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			before := slices.Clone(table.desc)
			if failed, err := Instantiate(b, imports); err == nil || failed != nil {
				if failed != nil {
					failed.Close()
				}
				t.Fatal("out-of-bounds expression initialization accepted")
			}
			clear(before[8 : 8+coreruntime.TableEntryBytes])
			if !bytes.Equal(table.desc, before) {
				t.Fatal("failed initialization lost source-order effects")
			}
			_, nullErr := in.Invoke("call", 5, 0)
			var nullTrap *coreruntime.TrapError
			if !errors.As(nullErr, &nullTrap) || nullTrap.Code != coreruntime.TrapIndirectOutOfBounds {
				t.Fatal("earlier null initializer did not persist", nullErr)
			}
			if got, err := in.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{9}) {
				t.Fatal("failed initialization cold reuse", got, err)
			}
			// A later valid segment overwrites an earlier entry in source order.
			overlap := rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(
				rv64ExpressionSegment(4, 0, []byte{0xd2, 0, 0x0b}, []byte{0xd0, 0x70, 0x0b}),
				rv64ExpressionSegment(6, 0, []byte{0xd2, 3, 0x0b}))))
			o := rv64TableStageCompile(t, "expr-preflight-overlap", overlap, artifact)
			next, err := Instantiate(o, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			_ = next.Close()
			if got, err := in.Invoke("call", 5, 0); err != nil || !slices.Equal(got, []uint64{14}) {
				t.Fatal("expression initialization order/retained writer", got, err)
			}
			// Within one segment, check the complete range before its first write.
			partial := rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(6, 0, []byte{0xd0, 0x70, 0x0b}, []byte{0xd0, 0x70, 0x0b}, []byte{0xd0, 0x70, 0x0b}))))
			partialCompiled := rv64TableStageCompile(t, "expr-preflight-partial", partial, artifact)
			before = slices.Clone(table.desc)
			if failed, err := Instantiate(partialCompiled, imports); err == nil || failed != nil {
				if failed != nil {
					failed.Close()
				}
				t.Fatal("oversized single segment admitted")
			}
			if !bytes.Equal(table.desc, before) {
				t.Fatal("single failing segment wrote a partial prefix")
			}
			// A successful overlap must clear every word of a nonnull row.
			clearSource := rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(4, 0, []byte{0xd2, 0, 0x0b}), rv64ExpressionSegment(6, 0, []byte{0xd0, 0x70, 0x0b}))))
			clearCompiled := rv64TableStageCompile(t, "expr-preflight-null-overlap", clearSource, artifact)
			cleared, err := Instantiate(clearCompiled, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer cleared.Close()
			if !bytes.Equal(table.desc[8:8+coreruntime.TableEntryBytes], make([]byte, coreruntime.TableEntryBytes)) {
				t.Fatal("overlapping null retained descriptor words")
			}
		})
	}
}

func TestRV64PublicExpressionElementIndex255(t *testing.T) {
	rv64TableFunctionImportsAggregateBoundary(t, true, true)
}

func TestRV64PublicExpressionElementEmpty(t *testing.T) {
	for _, flags := range []byte{4, 5, 6, 7} {
		for _, offset := range []byte{2, 3} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("flags%d-offset%d-artifact%t", flags, offset, artifact), func(t *testing.T) {
					source := rv64ExpressionElementModule([]wasm.ValType{wasm.I32}, flags, false)
					source = rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(flags, offset))))
					c := rv64TableStageCompile(t, fmt.Sprintf("expr-empty-flags%d-offset%d", flags, offset), source, artifact)
					in, err := Instantiate(c, NewImports().Function("env", "host", func(v int32) int32 { return v }))
					if in != nil {
						defer in.Close()
					}
					if (flags == 4 || flags == 6) && offset == 3 {
						if in != nil || err == nil {
							t.Fatal("empty active segment past table end admitted")
						}
						return
					}
					if err != nil {
						t.Fatal("empty segment at valid boundary", err)
					}
					if got, err := in.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{9}) {
						t.Fatal(got, err)
					}
				})
			}
		}
	}
}
