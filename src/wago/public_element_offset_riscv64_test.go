//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64GlobalOffsetElementModule(t *testing.T, types []wasm.ValType, borrowed, expression, empty bool) []byte {
	t.Helper()
	source := rv64ExpressionElementModule(types, 6, borrowed)
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0), wasmtest.GlobalImportEntry("env", "float", wasm.F32, false)}
	if borrowed {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 2))
	}
	imports = append(imports, wasmtest.GlobalImportEntry("env", "offset", wasm.I32, false))
	source = rv64ReplaceTableStageSection(t, source, 2, wasmtest.Section(2, wasmtest.Vec(imports...)))
	e := []byte{0, 0x23, 1, 0x0b}
	if expression {
		e = []byte{6, 0, 0x23, 1, 0x0b, 0x70}
	}
	if empty {
		e = append(e, 0)
	} else if expression {
		e = append(e, 1, 0xd2, 0, 0x0b)
	} else {
		e = append(e, 1, 0)
	}
	return rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(e)))
}

func TestRV64PublicImportedElementOffset(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			for _, expression := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-borrowed%t-expression%t-artifact%t", vector, borrowed, expression, artifact), func(t *testing.T) {
						types := []wasm.ValType{wasm.I32, wasm.F32, wasm.F64}
						if vector {
							types = append(types, wasm.V128)
						}
						key := fmt.Sprintf("global-offset-types%d-borrowed%t-expression%t", len(types), borrowed, expression)
						p := rv64TableStageCompile(t, key, rv64GlobalOffsetElementModule(t, types, borrowed, expression, false), artifact)
						c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, false, false, true, 2), artifact)
						g := NewGlobalI32(1, false)
						defer g.Close()
						f := NewGlobalF32(0, false)
						defer f.Close()
						// The offset consumer must read the low i32, regardless of raw upper bits.
						binary.LittleEndian.PutUint64(g.cell, 0xfeedface00000001)
						imports := NewImports().Global("env", "float", f).Global("env", "offset", g)
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
							leaf, err = Instantiate(n)
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
						in, err := Instantiate(p, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if !borrowed {
							table, err = in.ExportedTable("table")
							if err != nil {
								t.Fatal(err)
							}
						}
						if !bytes.Equal(table.desc[8:40], make([]byte, 32)) || bytes.Equal(table.desc[40:72], make([]byte, 32)) {
							t.Fatal("wrong global namespace/offset normalization")
						}
						consumer, err := Instantiate(c, NewImports().Table("env", "table", table))
						if err != nil {
							t.Fatal(err)
						}
						defer consumer.Close()
						_ = in.Close()
						_ = p.Close()
						if leaf != nil {
							_ = leaf.Close()
						}
						input, want := rv64NativeVectorInputs(types)
						if got, err := consumer.Invoke("call", append(input, 1)...); err != nil || !slices.Equal(got, append(want, 7)) {
							t.Fatal("retired global-offset initializer", got, err)
						}
						if _, err := consumer.Invoke("clear", 1); err != nil {
							t.Fatal(err)
						}
						if borrowed && (!in.resourcesClosed || leaf != nil && !leaf.resourcesClosed) {
							t.Fatal("offset initializer retained last owner")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicImportedElementOffsetBounds(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("empty%t-artifact%t", empty, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("global-offset-bounds-empty%t", empty), rv64GlobalOffsetElementModule(t, []wasm.ValType{wasm.I32}, true, true, empty), artifact)
				for _, offset := range []int32{0, 1, 2, 3, -1} {
					t.Run(fmt.Sprint(offset), func(t *testing.T) {
						table, err := NewTable(2, 2)
						if err != nil {
							t.Fatal(err)
						}
						defer table.Close()
						g := NewGlobalI32(offset, false)
						defer g.Close()
						f := NewGlobalF32(0, false)
						defer f.Close()
						in, err := Instantiate(c, NewImports().Table("env", "table", table).Global("env", "offset", g).Global("env", "float", f).Function("env", "host", func(v int32) int32 { return v }))
						if in != nil {
							defer in.Close()
						}
						valid := offset >= 0 && (offset < 2 || empty && offset == 2)
						if valid != (err == nil) || valid != (in != nil) {
							t.Fatal("resolved offset bound", offset, empty, err)
						}
						if !valid {
							if !bytes.Equal(table.desc[8:], make([]byte, len(table.desc)-8)) {
								t.Fatal("out-of-bounds offset wrote table")
							}
							if table.owner.importers != 0 || table.owner.nativeLeases != 0 || len(table.retained) != 0 || g.owner.importers != 0 || f.owner.importers != 0 {
								t.Fatal("failed offset initialization retained attachments")
							}
						}
					})
				}
			})
		}
	}
}

func TestRV64PublicImportedElementOffsetArtifact(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source metadata mutation fixture")
	}
	c := rv64TableStageCompile(t, "global-offset-artifact", rv64GlobalOffsetElementModule(t, []wasm.ValType{wasm.I32}, false, true, false), false)
	for _, mutate := range []func(*Compiled){
		func(v *Compiled) { v.Elems[0].Offset.Global = -1 }, func(v *Compiled) { v.Elems[0].Offset.Global = 0 }, func(v *Compiled) { v.Elems[0].Offset.Global = 2 }, func(v *Compiled) { v.Elems[0].Offset.Base = 1 }, func(v *Compiled) { v.Elems[0].Offset.Expr = []byte{0x23, 1, 0x0b} }, func(v *Compiled) { v.GlobalImports[1].Mutable = true; v.Globals[1].Mutable = true },
	} {
		v := *c.executionView()
		v.Elems = slices.Clone(v.Elems)
		v.Globals = slices.Clone(v.Globals)
		v.GlobalImports = slices.Clone(v.GlobalImports)
		mutate(&v)
		if err := railshotValidateCompiled(&v); err == nil {
			t.Fatal("unqualified offset metadata admitted")
		}
		blob, err := marshalCompiled(&v)
		if err != nil {
			continue
		}
		loaded, err := LoadTrustedArtifact(blob)
		if loaded != nil {
			loaded.Close()
		}
		if err == nil {
			t.Fatal("unqualified offset artifact admitted")
		}
	}
}

func TestRV64PublicImportedElementOffsetIndex255(t *testing.T) {
	rv64TableFunctionImportsAggregateBoundary(t, true, true, true)
}

func TestRV64PublicImportedElementOffsetFailureOrder(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			source := rv64GlobalOffsetElementModule(t, []wasm.ValType{wasm.I32}, true, true, false)
			p := rv64TableStageCompile(t, "global-offset-order-base", source, artifact)
			// A complete first segment remains visible when a later segment fails.
			first := []byte{6, 0, 0x23, 1, 0x0b, 0x70, 1, 0xd2, 0, 0x0b}
			bad := rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(first, rv64ExpressionSegment(6, 2, []byte{0xd2, 0, 0x0b}))))
			b := rv64TableStageCompile(t, "global-offset-order-bad", bad, artifact)
			// One segment must validate its entire range before writing anything.
			partial := append(slices.Clone(first[:6]), 2, 0xd0, 0x70, 0x0b, 0xd2, 0, 0x0b)
			tooLong := rv64TableStageCompile(t, "global-offset-order-partial", rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(partial))), artifact)
			table, err := NewTable(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			g := NewGlobalI32(1, false)
			defer g.Close()
			f := NewGlobalF32(0, false)
			defer f.Close()
			imports := NewImports().Table("env", "table", table).Global("env", "offset", g).Global("env", "float", f).Function("env", "host", func(v int32) int32 { return v + 1 })
			in, err := Instantiate(p, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			before := slices.Clone(table.desc)
			for range 3 {
				failed, err := Instantiate(tooLong, imports)
				if failed != nil {
					defer failed.Close()
				}
				if failed != nil || err == nil || !bytes.Equal(table.desc, before) {
					t.Fatal("failing segment changed populated table", err)
				}
				if table.owner.importers != 1 || table.owner.nativeLeases != 0 || g.owner.importers != 1 || f.owner.importers != 1 {
					t.Fatal("failed segment leaked attachments")
				}
			}
			replacement := NewImports().Table("env", "table", table).Global("env", "offset", g).Global("env", "float", f).Function("env", "host", func(v int32) int32 { return v + 7 })
			failed, err := Instantiate(b, replacement)
			if failed != nil {
				defer failed.Close()
			}
			if failed != nil || err == nil {
				t.Fatal("later out-of-bounds segment admitted", err)
			}
			_ = b.Close()
			if got, err := in.Invoke("call", 5, 1); err != nil || !slices.Equal(got, []uint64{12}) {
				t.Fatal("failed constructor lost earlier reference owner", got, err)
			}
			if b.codeCache.mem == nil {
				t.Fatal("live earlier segment lost executable mapping")
			}
			if _, err := in.Invoke("clear", 1); err != nil {
				t.Fatal(err)
			}
			if b.codeCache.mem != nil || table.owner.importers != 1 || table.owner.nativeLeases != 0 || g.owner.importers != 1 || f.owner.importers != 1 {
				t.Fatal("cleared failed-construction owner retained resources")
			}
		})
	}
}
