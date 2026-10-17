//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MutationTableImport() []byte {
	return wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 4)))
}

func TestRV64PublicForeignReferenceMutationCancellation(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32}
				if vector {
					types = append(types, wasm.V128)
				}
				key := fmt.Sprintf("foreign-mutation-cancel-types%d", len(types))
				importsSection := wasmtest.Section(2, wasmtest.Vec(append(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2), wasmtest.ULEB(1024)...)))
				pSource := rv64ReplaceTableStageSection(t, rv64ImportedTableModule(types, 9, false, true, true, 2), 2, importsSection)
				// The spin function saves the sole nonnull reference, clears its row,
				// then fills repeatedly. Cancellation must leave complete rows whose
				// retired owner survives through cold unwinding and later reuse.
				spin := []byte{1, 1, 0x70, 0x41, 0, 0x25, 0, 0x21, 0,
					0x41, 0, 0xd0, 0x70, 0x26, 0, 0x03, 0x40, 0x41, 1, 0x20, 0, 0x41}
				spin = append(spin, wasmtest.SLEB32(1023)...)
				spin = append(spin, 0xfc, 17, 0, 0x0c, 0, 0x0b, 0x0b)
				spinCode := append(wasmtest.ULEB(uint32(len(spin))), spin...)
				callerTypes := append(slices.Clone(types), wasm.I32)
				caller := []byte{}
				for index := range types {
					caller = append(caller, 0x20, byte(index))
				}
				caller = append(caller, 0x20, byte(len(types)), 0x11, 1, 0, 0x23, 0, 0x0b)
				// An unused v128 identity selects the complete SWAR module path.
				identity := []byte{}
				for index := range types {
					identity = append(identity, 0x20, byte(index))
				}
				identity = append(identity, 0x0b)
				cSource := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(callerTypes, callerTypes), wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
					importsSection, wasmtest.Section(3, []byte{4, 0, 2, 3, 1}),
					wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 7, 0x0b}))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("spin", 0, 1), wasmtest.ExportEntry("clear", 0, 2))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), spinCode, wasmtest.Code([]byte{0x41, 0, 0xd0, 0x70, 0x41, 0x80, 0x08, 0xfc, 17, 0, 0xfc, 16, 0, 0x0b}), wasmtest.Code(identity))),
				)
				p := rv64TableStageCompile(t, key+"-producer", pSource, artifact)
				c := rv64TableStageCompile(t, key+"-mutator", cSource, artifact)
				table, err := NewTable(1024, 1024)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				producer, err := Instantiate(p, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				mutator, err := Instantiate(c, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer mutator.Close()
				canonical := bytes.Clone(table.desc[8:40])
				_ = producer.Close()
				_ = p.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := mutator.InvokeContext(ctx, "spin"); done <- err }()
				witness := (*uint64)(unsafe.Pointer(&table.desc[8+2*coreruntime.TableEntryBytes]))
				deadline := time.Now().Add(5 * time.Second)
				for atomic.LoadUint64(witness) == 0 && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				entered := atomic.LoadUint64(witness) != 0
				cancel()
				select {
				case callErr := <-done:
					if !entered || !errors.Is(callErr, context.Canceled) {
						t.Fatal("foreign bulk progress/cancellation", entered, callErr)
					}
				case <-time.After(5 * time.Second):
					stop := coreruntime.RequestInterruptAsync(mutator.trap)
					<-done
					stop()
					t.Fatal("foreign bulk cancellation stalled")
				}
				var zero [coreruntime.TableEntryBytes]byte
				for index := 0; index < 1024; index++ {
					row := table.desc[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
					if !bytes.Equal(row, zero[:]) && !bytes.Equal(row, canonical) {
						t.Fatal("partial foreign descriptor after join", index)
					}
				}
				input, want := rv64NativeVectorInputs(types)
				want[0] = uint64(uint32(want[0] + 9))
				want = append(want, 7)
				got, err := mutator.Invoke("call", append(input, 2)...)
				if err != nil || !slices.Equal(got, want) || producer.resourcesClosed {
					t.Fatal("retired foreign producer after cancellation", got, err)
				}
				got, err = mutator.Invoke("clear")
				if err != nil || !slices.Equal(got, []uint64{1024}) || !producer.resourcesClosed || p.codeCache.mem != nil {
					t.Fatal("foreign cancellation final cleanup", got, err)
				}
			})
		}
	}
}

// References originate from a genuine producer's active segment and travel
// through table.get, full-width locals, typed select and block-result joins.
func rv64ForeignMutationModule(types []wasm.ValType) []byte {
	callerTypes := append(slices.Clone(types), wasm.I32)
	caller := []byte{}
	for index := range types {
		caller = append(caller, 0x20)
		caller = append(caller, wasmtest.ULEB(uint32(index))...)
	}
	caller = append(caller, 0x20)
	caller = append(caller, wasmtest.ULEB(uint32(len(types)))...)
	caller = append(caller, 0x11, 1, 0, 0x23, 0, 0x0b)
	// Save a foreign reference, clear its original row, then use a control join
	// before writing the saved carrier back. One nullable funcref local follows
	// the two i32 parameters; the reference must never use i32 normalization.
	joinBody := []byte{1, 1, 0x70, 0x20, 1, 0x25, 0, 0x21, 2,
		0x20, 1, 0xd0, 0x70, 0x26, 0, 0x20, 0,
		0x02, 0x70, 0x20, 2, 0x0c, 0, 0x0b, 0x26, 0, 0x0b}
	selectBody := []byte{1, 1, 0x70, 0x20, 1, 0x25, 0, 0x21, 2,
		0x20, 1, 0xd0, 0x70, 0x26, 0, 0x20, 0,
		0x20, 2, 0xd0, 0x70, 0x41, 1, 0x1c, 1, 0x70, 0x26, 0, 0x0b}
	codeWithLocals := func(body []byte) []byte { return append(wasmtest.ULEB(uint32(len(body))), body...) }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(callerTypes, callerTypes), wasmtest.FuncType(types, types),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		rv64MutationTableImport(), wasmtest.Section(3, []byte{9, 0, 2, 3, 4, 3, 2, 2, 2, 5}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 7, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("set", 0, 1),
			wasmtest.ExportEntry("fill", 0, 2), wasmtest.ExportEntry("grow", 0, 3), wasmtest.ExportEntry("copy", 0, 4),
			wasmtest.ExportEntry("clear", 0, 5), wasmtest.ExportEntry("join", 0, 6), wasmtest.ExportEntry("select", 0, 7),
			wasmtest.ExportEntry("size", 0, 8), wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x25, 0, 0x26, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x25, 0, 0x20, 2, 0xfc, 17, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0x20, 1, 0xfc, 15, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 17, 0, 0x0b}),
			codeWithLocals(joinBody), codeWithLocals(selectBody), wasmtest.Code([]byte{0xfc, 16, 0, 0x0b}))),
	)
}

func TestRV64PublicForeignReferenceMutation(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
				if vector {
					types = append(types, wasm.V128)
				}
				key := fmt.Sprintf("foreign-mutation-types%d", len(types))
				producerSource := rv64ImportedTableModule(types, 9, false, true, true, 2)
				producerSource = rv64ReplaceTableStageSection(t, producerSource, 2, rv64MutationTableImport())
				producerSource = rv64ReplaceTableStageSection(t, producerSource, 9, ownedTableElements(0, 1, 2))
				p := rv64TableStageCompile(t, key+"-producer", producerSource, artifact)
				c := rv64TableStageCompile(t, key+"-mutator", rv64ForeignMutationModule(types), artifact)
				table, err := NewTable(2, 4)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				producer, err := Instantiate(p, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				mutator, err := Instantiate(c, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer mutator.Close()
				input, want := rv64NativeVectorInputs(types)
				want[0] = uint64(uint32(want[0] + 9))
				want = append(want, 7)
				dirty := func(v uint32) uint64 { return 0xfeedface00000000 | uint64(v) }
				call := func(index uint32) {
					t.Helper()
					got, err := mutator.Invoke("call", append(slices.Clone(input), dirty(index))...)
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("foreign carrier/context/bit preservation", index, got, want, err)
					}
				}
				invoke := func(name string, args ...uint64) []uint64 {
					t.Helper()
					got, err := mutator.Invoke(name, args...)
					if err != nil {
						t.Fatal(name, got, err)
					}
					return got
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := p.Close(); err != nil {
					t.Fatal(err)
				}
				runtime.GC()
				call(0)
				// Storage accepts a descriptor with a different signature; only the
				// indirect call checks the complete structural signature key.
				invoke("set", dirty(0), dirty(1))
				_, err = mutator.Invoke("call", append(slices.Clone(input), dirty(0))...)
				ownedTableTrap(t, err, coreruntime.TrapIndirectWrongSig)
				// Restore a genuine typed descriptor by a second public producer.
				p2 := rv64TableStageCompile(t, key+"-restore", producerSource, artifact)
				restored, err := Instantiate(p2, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Close()
				if !producer.resourcesClosed || p.codeCache.mem != nil {
					t.Fatal("overwritten retired producer retained")
				}
				invoke("set", dirty(1), dirty(0))
				if err := restored.Close(); err != nil {
					t.Fatal(err)
				}
				if err := p2.Close(); err != nil {
					t.Fatal(err)
				}
				invoke("join", dirty(1), dirty(0))
				call(1)
				invoke("select", dirty(0), dirty(1))
				call(0)
				if got := invoke("grow", dirty(0), dirty(2)); !slices.Equal(got, []uint64{2}) || table.Size() != 4 {
					t.Fatal("foreign fill during grow", got, table.Size())
				}
				call(2)
				call(3)
				invoke("fill", dirty(1), dirty(0), dirty(3))
				invoke("clear", dirty(0), dirty(1))
				invoke("copy", dirty(0), dirty(1), dirty(3)) // overlap forwards
				call(0)
				call(1)
				invoke("copy", dirty(1), dirty(0), dirty(3)) // overlap backwards
				call(3)
				before := bytes.Clone(table.desc)
				for _, invalid := range []struct {
					name string
					args []uint64
				}{
					{"set", []uint64{dirty(4), dirty(0)}}, {"set", []uint64{dirty(0), dirty(4)}},
					{"fill", []uint64{dirty(3), dirty(0), dirty(2)}}, {"fill", []uint64{dirty(0), dirty(4), dirty(0)}},
					{"copy", []uint64{dirty(3), dirty(0), dirty(2)}}, {"copy", []uint64{dirty(0), dirty(3), dirty(2)}},
					{"copy", []uint64{dirty(^uint32(0)), dirty(0), dirty(1)}},
				} {
					_, err := mutator.Invoke(invalid.name, invalid.args...)
					ownedTableTrap(t, err, coreruntime.TrapTableOutOfBounds)
					if !bytes.Equal(before, table.desc) {
						t.Fatal("failed mutation changed rows/length", invalid.name)
					}
				}
				if got := invoke("grow", dirty(0), dirty(^uint32(0))); !slices.Equal(got, []uint64{0xffffffff}) || !bytes.Equal(before, table.desc) {
					t.Fatal("failed foreign grow changed storage", got)
				}
				invoke("copy", dirty(4), dirty(4), dirty(0))
				invoke("fill", dirty(4), dirty(0), dirty(0))
				if !bytes.Equal(before, table.desc) || restored.resourcesClosed {
					t.Fatal("zero range or live foreign row lifetime")
				}
				invoke("clear", dirty(0), dirty(4))
				if !restored.resourcesClosed || p2.codeCache.mem != nil || p2.codeCache.refs != 0 {
					t.Fatal("last copied foreign reference retained producer/code")
				}
			})
		}
	}
}
