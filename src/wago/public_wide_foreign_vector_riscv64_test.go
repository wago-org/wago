//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64WideForeignVectorModule(count int, producer, table, tail bool, mixed ...bool) []byte {
	values := make([]wasm.ValType, count)
	var operands []byte
	for i := range values {
		values[i] = wasm.V128
		if len(mixed) != 0 && mixed[0] && i == len(values)-1 {
			values[i] = wasm.I32
		}
		operands = append(operands, 0x20)
		operands = append(operands, wasmtest.ULEB(uint32(i))...)
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(values, values)))}
	if !producer {
		kind := []byte{0, 0}
		if table {
			kind = []byte{1, 0x70, 1, 1, 1}
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), kind...))))
	}
	sections = append(sections, wasmtest.Section(3, []byte{1, 0}))
	exports := [][]byte{}
	if producer {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
		exports = append(exports, wasmtest.ExportEntry("table", 1, 0))
	}
	index := uint32(0)
	if !producer && !table {
		index = 1
	}
	exports = append(exports, wasmtest.ExportEntry("run", 0, index))
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if producer {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))))
	}
	body := slices.Clone(operands)
	if !producer {
		opcode := byte(0x10)
		if table {
			body = append(body, 0x41, 0)
			opcode = 0x11
		}
		if tail {
			opcode += 2
		}
		body = append(body, opcode, 0)
		if table {
			body = append(body, 0)
		}
	}
	body = append(body, 0x0b)
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))...)
}

func TestRV64PublicWideVectorForeignCalls(t *testing.T) {
	for _, shape := range []struct {
		count int
		mixed bool
	}{{1, false}, {129, false}, {256, false}, {256, true}} {
		count := shape.count
		for _, table := range []bool{false, true} {
			for _, tail := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vectors%d-mixed%t/table%t/tail%t/artifact%t", count, shape.mixed, table, tail, artifact), func(t *testing.T) {
						if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" && !artifact {
							t.Skip("compiler-free artifact path")
						}
						p := rv64TableStageCompile(t, fmt.Sprintf("wide-foreign-producer-%d-mixed%t", count, shape.mixed), rv64WideForeignVectorModule(count, true, false, false, shape.mixed), artifact)
						producer, err := Instantiate(p)
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						imports := NewImports()
						if table {
							handle, err := producer.ExportedTable("table")
							if err != nil {
								t.Fatal(err)
							}
							imports.Table("env", "target", handle)
						} else {
							handle, err := producer.ExportedFunc("run")
							if err != nil {
								t.Fatal(err)
							}
							imports.Function("env", "target", handle)
						}
						c := rv64TableStageCompile(t, fmt.Sprintf("wide-foreign-consumer-%d-mixed%t-table%t-tail%t", count, shape.mixed, table, tail), rv64WideForeignVectorModule(count, false, table, tail, shape.mixed), artifact)
						consumer, err := Instantiate(c, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer consumer.Close()
						slots := 2 * count
						if shape.mixed {
							slots--
						}
						args := make([]uint64, slots)
						for i := range args {
							args[i] = uint64(i)*0x9e3779b97f4a7c15 ^ 0x7ff0000000000001
						}
						want := slices.Clone(args)
						if shape.mixed {
							want[len(want)-1] = uint64(uint32(want[len(want)-1]))
						}
						for _, closed := range []bool{false, true} {
							if closed {
								if err := producer.Close(); err != nil {
									t.Fatal(err)
								}
							}
							out, err := consumer.Invoke("run", args...)
							if err != nil || !slices.Equal(out, want) {
								t.Fatalf("foreign vector identity closed=%t slots=%d equal=%t: %v", closed, len(out), slices.Equal(out, want), err)
							}
						}
						allocations := testing.AllocsPerRun(20, func() {
							out, err := consumer.Invoke("run", args...)
							if err != nil || !slices.Equal(out, want) {
								panic("foreign vector identity changed")
							}
						})
						t.Logf("foreign vector warm allocations=%g slots=%d", allocations, len(args))
					})
				}
			}
		}
	}
}

func rv64NarrowEntryWideInternalModule() []byte {
	params := make([]wasm.ValType, 256)
	var entry []byte
	for i := range params {
		params[i] = wasm.V128
		entry = append(entry, 0xfd, 0x0c)
		entry = append(entry, make([]byte, 16)...)
	}
	entry = append(entry, 0x10, 0, 0x41, 42, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(entry))),
	)
}

func TestRV64NarrowForeignEntryWithWideInternalCall(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, table := range []bool{false, true} {
			for _, tail := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t/table%t/tail%t", artifact, table, tail), func(t *testing.T) {
					p := rv64TableStageCompile(t, "narrow-wide-internal-producer", rv64NarrowEntryWideInternalModule(), artifact)
					producer, err := Instantiate(p)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					imports := NewImports()
					kind := []byte{0, 0}
					body := []byte{0x10, 0, 0x0b}
					index := uint32(1)
					if table {
						handle, err := producer.ExportedTable("table")
						if err != nil {
							t.Fatal(err)
						}
						imports.Table("env", "target", handle)
						kind, body, index = []byte{1, 0x70, 1, 1, 1}, []byte{0x41, 0, 0x11, 0, 0, 0x0b}, 0
					} else {
						handle, err := producer.ExportedFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						imports.Function("env", "target", handle)
					}
					if tail {
						if table {
							body[2] = 0x13
						} else {
							body[0] = 0x12
						}
					}
					source := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
						wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), kind...))),
						wasmtest.Section(3, []byte{1, 0}),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, index))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					)
					c := rv64TableStageCompile(t, fmt.Sprintf("narrow-wide-internal-consumer-table%t-tail%t", table, tail), source, artifact)
					consumer, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 2; i++ {
						if out, err := consumer.Invoke("run"); err != nil || !slices.Equal(out, []uint64{42}) {
							t.Fatal("narrow entry lost internal transport capacity", out, err)
						}
					}
				})
			}
		}
	}
}
