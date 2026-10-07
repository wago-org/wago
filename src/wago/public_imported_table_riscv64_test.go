//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64TableStageCompile(t *testing.T, name string, source []byte, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE")
	if mode == "load" {
		if !artifact {
			t.Skip("precompiled artifact path only")
		}
		blob, err := os.ReadFile(filepath.Join(dir, name+".wago"))
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if c != nil {
			t.Cleanup(func() { _ = c.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := rv64PublicStartCompile(t, source, artifact)
	if mode == "prepare" {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".wago"), blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

// Imported tables contain canonical producer descriptors from genuine public
// construction. No fixture writes raw rows or calls a private runtime helper.
func rv64ImportedTableModule(types []wasm.ValType, seed byte, shared, initialize, hasMax bool, minimum uint32, installLocal ...bool) []byte {
	postCallWrite := len(installLocal) > 1 && installLocal[1]
	callerTypes := append(slices.Clone(types), wasm.I32)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType(callerTypes, callerTypes), wasmtest.FuncType(types, types),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	))}
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70)
	if hasMax {
		tableImport = append(tableImport, 1)
	} else {
		tableImport = append(tableImport, 0)
	}
	tableImport = append(tableImport, wasmtest.ULEB(minimum)...)
	if hasMax {
		tableImport = append(tableImport, 2)
	}
	imports := [][]byte{tableImport}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	caller, target := []byte{}, []byte{}
	for index := range types {
		caller = append(caller, 0x20)
		caller = append(caller, wasmtest.ULEB(uint32(index))...)
		target = append(target, 0x20)
		target = append(target, wasmtest.ULEB(uint32(index))...)
		if index == 0 {
			target = append(target, 0x23, 0, 0x6a)
		}
	}
	caller = append(caller, 0x20)
	caller = append(caller, wasmtest.ULEB(uint32(len(types)))...)
	caller = append(caller, 0x11, 1, 0)
	if postCallWrite {
		caller = append(caller, 0x41, 1, 0xd2, 1, 0x26, 0)
	}
	caller = append(caller, 0x23, 0, 0x0b)
	target = append(target, 0x0b)
	sections = append(sections,
		wasmtest.Section(3, []byte{6, 0, 1, 2, 3, 3, 2}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, seed, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("safe", 0, 2),
			wasmtest.ExportEntry("clear", 0, 3), wasmtest.ExportEntry("grow", 0, 4), wasmtest.ExportEntry("size", 0, 5),
			wasmtest.ExportEntry("table", 1, 0), wasmtest.ExportEntry("alias", 1, 0))))
	if initialize {
		sections = append(sections, ownedTableElements(0, 1))
	} else if postCallWrite || len(installLocal) != 0 && installLocal[0] {
		sections = append(sections, wasmtest.Section(9, []byte{1, 3, 0, 1, 1}))
	}
	clearBody := []byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b}
	if len(installLocal) != 0 && installLocal[0] {
		clearBody = []byte{0x20, 0, 0xd2, 1, 0x26, 0, 0xfc, 16, 0, 0x0b}
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(target),
		wasmtest.Code([]byte{0x23, 0, 0x0b}),
		wasmtest.Code(clearBody),
		wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 15, 0, 0x0b}),
		wasmtest.Code([]byte{0xfc, 16, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestRV64PublicImportedTableCallsAndRetiredOwner(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-shared%t-artifact%t", vector, shared, artifact), func(t *testing.T) {
					types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
					if vector {
						types = append(types, wasm.V128)
					}
					key := fmt.Sprintf("imported-types%d-shared%t", len(types), shared)
					p := rv64TableStageCompile(t, key+"-producer", rv64ForeignTableCallModule(types, 9, shared, false), artifact)
					c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, shared, false, true, 1), artifact)
					imports := NewImports()
					consumerImports := NewImports()
					if shared {
						memory, err := NewSharedMemory(1, 4)
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
						imports.Memory("env", "memory", memory)
						consumerImports.Memory("env", "memory", memory)
					}
					producer, err := Instantiate(p, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					table, err := producer.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					consumer, err := Instantiate(c, consumerImports.Table("env", "table", table))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					alias, err := consumer.ExportedTable("alias")
					if err != nil || alias != table {
						t.Fatal("reexport did not preserve storage identity", err)
					}
					if (producer.jm == consumer.jm) != shared || producer.nativeContext == consumer.nativeContext {
						t.Fatal("native context/home identity")
					}
					input, want := rv64NativeVectorInputs(types)
					want[0] = uint64(uint32(want[0] + 9))
					want = append(want, 7)
					input = append(input, 0xfeedface00000000)
					prepared, err := consumer.WasmFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := p.Close(); err != nil {
						t.Fatal(err)
					}
					runtime.GC()
					for range 3 {
						got, err := consumer.Invoke("call", input...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("public imported foreign call", got, err)
						}
						out, err := prepared.Invoke(input...)
						if err != nil || !slices.Equal(out, want) {
							t.Fatal("prepared imported foreign call", out, err)
						}
					}
					if got, err := consumer.Invoke("grow", 0xfeedface00000001); err != nil || !slices.Equal(got, []uint64{1}) || table.Size() != 2 {
						t.Fatal("borrowed descriptor growth", got, err)
					}
					if null, err := alias.EntryIsNull(1); err != nil || !null {
						t.Fatal("grown null row", null, err)
					}
					_, err = consumer.Invoke("call", append(slices.Clone(input[:len(input)-1]), 1)...)
					ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
					if got, err := consumer.Invoke("clear", 0xfeedface00000000); err != nil || !slices.Equal(got, []uint64{2}) {
						t.Fatal("borrowed clear", got, err)
					}
					if producer.resourcesClosed {
						t.Fatal("imported table storage lost its owner before detach")
					}
					if err := consumer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					if p.codeCache.refs != 0 || p.codeCache.mem != nil || c.codeCache.refs != 0 || c.codeCache.mem != nil {
						t.Fatal("last imported owner leaked code")
					}
				})
			}
		}
	}
}

func TestRV64PublicImportedHostTableInitializerAndNoMaxGrowth(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32}
				if vector {
					types = append(types, wasm.V128)
				}
				table, err := NewTable(600, 601)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				key := fmt.Sprintf("host-imported-types%d", len(types))
				p := rv64TableStageCompile(t, key+"-writer", rv64ImportedTableModule(types, 9, false, true, false, 600), artifact)
				c := rv64TableStageCompile(t, key+"-reader", rv64ImportedTableModule(types, 7, false, false, false, 600), artifact)
				writer, err := Instantiate(p, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
				reader, err := Instantiate(c, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := p.Close(); err != nil {
					t.Fatal(err)
				}
				runtime.GC()
				input, want := rv64NativeVectorInputs(types)
				want[0] = uint64(uint32(want[0] + 9))
				want = append(want, 7)
				input = append(input, 0)
				if got, err := reader.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("retired active-element writer", got, err)
				}
				if got, err := reader.Invoke("grow", 0xfeedface00000001); err != nil || !slices.Equal(got, []uint64{600}) || table.Size() != 601 {
					t.Fatal("no-max imported actual reserve", got, err)
				}
				if got, err := reader.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{0xffffffff}) || table.Size() != 601 {
					t.Fatal("borrowed growth failure", got, err)
				}
				if _, err := reader.Invoke("clear", 0); err != nil {
					t.Fatal(err)
				}
				if !writer.resourcesClosed {
					t.Fatal("last host row left retired writer rooted")
				}
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRV64PublicImportedTableRejectsInvalidStorageBeforeMapping(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source-dependent rejection")
	}
	for _, kind := range []string{"oversize", "table64", "min", "max", "closed", "forged"} {
		t.Run(kind, func(t *testing.T) {
			table, err := NewTable(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			switch kind {
			case "oversize":
				_ = table.Close()
				table, err = NewTable(1, 1025)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
			case "table64":
				_ = table.Close()
				table, err = NewTable64(1, 2)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
			case "min":
				_ = table.Close()
				table, err = NewTable(0, 2)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
			case "max":
				table.owner.declaredHasMax = false
			case "closed":
				_ = table.Close()
			case "forged":
				table.desc[8] = 1
			}
			before := slices.Clone(table.desc)
			c, err := Compile(nil, rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, true, true, 1))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, NewImports().Table("env", "table", table))
			if in != nil {
				_ = in.Close()
			}
			if err == nil {
				t.Fatal("invalid table attachment admitted")
			}
			if !bytes.Equal(before, table.desc) {
				t.Fatal("rejection changed table bytes")
			}
			if cache := c.loadCodeCache(); cache != nil && (cache.base != 0 || cache.sealed) {
				t.Fatal("invalid imported table reached executable mapping")
			}
			if table.owner.importers != 0 {
				t.Fatal("failed preflight leaked importer lease")
			}
		})
	}
}

func TestRV64ImportedTablePreflightCloseRace(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source-dependent lifetime race")
	}
	c := rv64PublicStartCompile(t, rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, false, true, 1), false)
	for range 40 {
		table, err := NewTable(1, 2)
		if err != nil {
			t.Fatal(err)
		}
		imports, err := NewImports().Table("env", "table", table).snapshot()
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = railshotValidateResolvedImports(c, imports, nil) }()
		go func() { defer wg.Done(); _ = table.Close() }()
		wg.Wait()
		if table.owner.importers != 0 {
			t.Fatal("temporary storage lease leaked")
		}
		_ = table.Close()
	}
}

func TestRV64ActualImportedTablePrecompiledCompilerGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled runtime only")
	}
	if c, err := Compile(nil, rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, false, true, 1)); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("precompiled witness used source compilation", err)
	}
}

func TestRV64ImportedTableBoundedDeclaration(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source admission only")
	}
	if c, err := Compile(nil, rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, false, false, 1025)); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("unbounded imported declaration admitted", err)
	}
}

func rv64ReplaceTableStageSection(t *testing.T, source []byte, replacementID byte, replacement []byte) []byte {
	t.Helper()
	out := slices.Clone(source[:8])
	reader := wasm.ReaderFrom(source[8:])
	for reader.HasNext() {
		start := reader.Offset()
		id, err := reader.Byte()
		if err != nil {
			t.Fatal(err)
		}
		size, err := reader.U32()
		if err != nil {
			t.Fatal(err)
		}
		end := reader.Offset() + int(size)
		if id == replacementID {
			out = append(out, replacement...)
		} else {
			out = append(out, source[8+start:8+end]...)
		}
		if err := reader.JumpTo(end); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestRV64ImportedTableReplacesRetiredWriterAndPreservesEarlierInitializer(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32}
				if vector {
					types = append(types, wasm.V128)
				}
				key := fmt.Sprintf("imported-replace-types%d", len(types))
				table, err := NewTable(1, 2)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				first := rv64TableStageCompile(t, key+"-first", rv64ImportedTableModule(types, 9, false, true, true, 1), artifact)
				old, err := Instantiate(first, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer old.Close()
				_ = old.Close()
				_ = first.Close()
				if old.resourcesClosed {
					t.Fatal("installed retired writer lost storage")
				}
				next := rv64TableStageCompile(t, key+"-next", rv64ImportedTableModule(types, 11, false, true, true, 1), artifact)
				replacement, err := Instantiate(next, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer replacement.Close()
				if !old.resourcesClosed || first.codeCache.mem != nil {
					t.Fatal("overwritten retired writer was not finalized")
				}
				_ = replacement.Close()
				_ = next.Close()
				failedSource := rv64ReplaceTableStageSection(t, rv64ImportedTableModule(types, 13, false, true, true, 1), 9,
					wasmtest.Section(9, []byte{2, 0, 0x41, 0, 0x0b, 1, 1, 0, 0x41, 1, 0x0b, 1, 1}))
				failed := rv64TableStageCompile(t, key+"-failed", failedSource, artifact)
				partial, err := Instantiate(failed, NewImports().Table("env", "table", table))
				if partial != nil {
					_ = partial.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "active element") {
					t.Fatal("late element OOB was not reported", err)
				}
				if !replacement.resourcesClosed {
					t.Fatal("failed constructor left overwritten writer rooted")
				}
				_ = failed.Close()
				readerCode := rv64TableStageCompile(t, key+"-reader", rv64ImportedTableModule(types, 7, false, false, true, 1), artifact)
				reader, err := Instantiate(readerCode, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				input, want := rv64NativeVectorInputs(types)
				want[0] = uint64(uint32(want[0] + 13))
				want = append(want, 7)
				input = append(input, 0)
				if got, err := reader.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("earlier active segment/failed owner lifetime", got, err)
				}
				if _, err := reader.Invoke("clear", 0); err != nil {
					t.Fatal(err)
				}
				if failed.codeCache.refs != 0 || failed.codeCache.mem != nil {
					t.Fatal("cleared failed constructor left code rooted")
				}
			})
		}
	}
}

func TestRV64PublicImportedTableColdProducerAndCallerReuse(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-artifact%t", shared, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32}
				key := fmt.Sprintf("imported-cold-shared%t", shared)
				p := rv64TableStageCompile(t, key+"-producer", rv64ForeignTableCallModule(types, 9, shared, true), artifact)
				c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, shared, false, true, 1), artifact)
				producerImports, consumerImports := NewImports(), NewImports()
				if shared {
					memory, err := NewSharedMemory(1, 4)
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					producerImports.Memory("env", "memory", memory)
					consumerImports.Memory("env", "memory", memory)
				}
				producer, err := Instantiate(p, producerImports)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				table, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				reader, err := Instantiate(c, consumerImports.Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				_ = producer.Close()
				_ = p.Close()
				_ = c.Close()
				runtime.GC()
				for range 3 {
					_, err := reader.InvokeContext(context.Background(), "call", 0, 0xfeedface00000000)
					trap := ownedTableTrap(t, err, coreruntime.TrapUnreachable)
					if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].FunctionName != "target9" || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 6 {
						t.Fatal("imported target cold provenance", trap.Frames)
					}
					if got, err := reader.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{7}) {
						t.Fatal("caller identity after cold return", got, err)
					}
					if got, err := reader.Invoke("call", 5, 0); err != nil || !slices.Equal(got, []uint64{14, 7}) {
						t.Fatal("imported reuse after cold unwind", got, err)
					}
				}
				if _, err := reader.Invoke("clear", 0); err != nil {
					t.Fatal(err)
				}
				_ = reader.Close()
				if producer.hasPhysicalResources() || p.codeCache.mem != nil || c.codeCache.mem != nil {
					t.Fatal("cold imported owner cleanup")
				}
			})
		}
	}
}

func TestRV64PublicImportedTableRuntimeWriterMembership(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32}
				if vector {
					types = append(types, wasm.V128)
				}
				key := fmt.Sprintf("imported-runtime-writer-types%d", len(types))
				p := rv64TableStageCompile(t, key+"-writer", rv64ImportedTableModule(types, 9, false, false, true, 1, true), artifact)
				c := rv64TableStageCompile(t, key+"-reader", rv64ImportedTableModule(types, 7, false, false, true, 1), artifact)
				table, err := NewTable(1, 2)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				writer, err := Instantiate(p, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
				reader, err := Instantiate(c, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				if _, err := writer.Invoke("clear", 0); err != nil {
					t.Fatal("install local canonical ref at runtime", err)
				}
				input, want := rv64NativeVectorInputs(types)
				want[0] = uint64(uint32(want[0] + 9))
				want = append(want, 7)
				input = append(input, 0)
				if got, err := reader.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("reader could not authenticate still-open writer", got, err)
				}
				liveAllocations := testing.AllocsPerRun(20, func() {
					got, err := reader.Invoke("call", input...)
					if err != nil || !slices.Equal(got, want) {
						panic("live imported writer call")
					}
				})
				_ = writer.Close()
				_ = p.Close()
				runtime.GC()
				if got, err := reader.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("retired runtime writer", got, err)
				}
				retiredAllocations := testing.AllocsPerRun(20, func() {
					got, err := reader.Invoke("call", input...)
					if err != nil || !slices.Equal(got, want) {
						panic("retired imported writer call")
					}
				})
				t.Logf("imported-table allocations per call: live=%g retired=%g", liveAllocations, retiredAllocations)
				if liveAllocations > 16 || retiredAllocations > 64 {
					t.Fatal("imported-table allocation bound", liveAllocations, retiredAllocations)
				}
				if _, err := reader.Invoke("clear", 0); err != nil {
					t.Fatal(err)
				}
				if !writer.resourcesClosed || p.codeCache.mem != nil {
					t.Fatal("runtime-installed producer root leaked")
				}
				if _, err := reader.Invoke("safe"); err != nil {
					t.Fatal(err)
				}
				table.mu.Lock()
				state := table.retained[reader]
				table.mu.Unlock()
				if state != nil {
					t.Fatal("read-only activation kept transient membership")
				}
			})
		}
	}
}

func TestRV64ImportedTableNestedWriterCountersAndFailedRefresh(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source-dependent scope qualification")
	}
	c := rv64PublicStartCompile(t, rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 9, false, false, true, 1, true), false)
	table, err := NewTable(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	writer, err := Instantiate(c, NewImports().Table("env", "table", table))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	before := writer.resourceRefs
	var first, second rv64NativeTableTargets
	nativeExecutionMu.Lock()
	err = first.refresh(writer)
	if err == nil {
		err = second.refresh(writer)
	}
	nativeExecutionMu.Unlock()
	if err != nil {
		first.release()
		second.release()
		t.Fatal(err)
	}
	table.pruneRetainedInstances()
	table.mu.Lock()
	state := table.retained[writer]
	active := state.activeWriters
	table.mu.Unlock()
	if active != 2 {
		t.Fatal("nested writer counter", active)
	}
	first.release()
	table.mu.Lock()
	active = table.retained[writer].activeWriters
	table.mu.Unlock()
	if active != 1 {
		t.Fatal("outer scope dropped nested writer promise", active)
	}
	second.release()
	if writer.resourceRefs != before || len(table.retained) != 0 {
		t.Fatal("no-write nested scope leaked membership")
	}
	nativeExecutionMu.Lock()
	table.desc[8] = 1
	err = first.refresh(writer)
	table.desc[8] = 0
	nativeExecutionMu.Unlock()
	if err == nil {
		first.release()
		t.Fatal("forged row did not fail refresh")
	}
	first.release()
	if writer.resourceRefs != before || len(table.retained) != 0 {
		t.Fatal("partial refresh leaked writer roots")
	}
	if _, err := writer.Invoke("clear", 0); err != nil {
		t.Fatal(err)
	}
	nativeExecutionMu.Lock()
	err = first.refresh(writer)
	nativeExecutionMu.Unlock()
	if err != nil {
		first.release()
		t.Fatal(err)
	}
	_ = writer.Close()
	if err := table.Close(); err == nil {
		first.release()
		t.Fatal("retired suspended scope lost host table storage")
	}
	// The parked scope still promises that this retired writer can resume with
	// local references. Clearing its current row must preserve that membership.
	nativeExecutionMu.Lock()
	clear(table.desc[8 : 8+coreruntime.TableEntryBytes])
	nativeExecutionMu.Unlock()
	table.pruneRetainedInstances()
	if writer.resourcesClosed || table.retained[writer] == nil || table.retained[writer].activeWriters != 1 {
		first.release()
		t.Fatal("clearing rows retired a suspended writer")
	}
	first.release()
	if !writer.resourcesClosed {
		t.Fatal("last suspended scope left writer alive")
	}
	if err := table.Close(); err != nil {
		t.Fatal("released native scope kept storage leased", err)
	}
}
