//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64CombinedStateOwnersMutationCallsDropAndIndependentClose(t *testing.T) {
	source, fixtures, initial, payload := swarCombinedStateSource()
	image, code := swarTableStateImageRV64(t, source)
	entry := func(i int) uintptr { return code.Base() + uintptr(image.functions[i].Entry) }
	for pass := range 2 {
		e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
		if uint64(j.CurrentPages()) != image.memory.Limits.Min || uint64(j.MaxPages()) != image.memory.Limits.Max || !image.memory.Limits.HasMax {
			t.Fatal("combined declared memory/current allocation ceiling")
		}
		globals, err := image.instantiateGlobals(j)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := globals.Close(); err != nil {
				t.Error(err)
			}
		})
		data, err := image.instantiateData(j)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := data.Close(); err != nil {
				t.Error(err)
			}
		})
		tables := swarTableStateOwnerRV64(t, image, code, j)
		ctx := j.CaptureInstanceContext()
		if ctx.GlobalsPtr != globals.directory || ctx.PassiveDataPtr != data.directory || ctx.TablePtr != tables.table || ctx.FuncRefDescPtr != tables.functions || ctx.PassiveElemPtr != tables.elements || !bytes.Equal(j.CurrentBytes()[7:9], []byte{0xa5, 0x5a}) {
			t.Fatal("combined native bindings/active data", pass)
		}
		input := [2]uint64{0xfff0000000000001, 0x7fa1234500000001}
		run := func(drop uint32, old [2]uint64) {
			binary.LittleEndian.PutUint64(args, input[0])
			binary.LittleEndian.PutUint64(args[8:], input[1])
			binary.LittleEndian.PutUint64(args[16:], dirty(101)) // byte-unaligned vector access
			binary.LittleEndian.PutUint64(args[24:], dirty(drop))
			for i := range results {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry(0), args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal("combined call", pass, err)
			}
			for half := range 2 {
				if binary.LittleEndian.Uint64(results[half*8:]) != old[half] || binary.LittleEndian.Uint64(results[16+half*8:]) != old[half]^binary.LittleEndian.Uint64(payload[half*8:]) || binary.LittleEndian.Uint64(results[32+half*8:]) != old[half]^input[half] {
					t.Fatal("combined old live global/memory/indirect/direct/new reload", pass, half, results[:48])
				}
			}
			if binary.LittleEndian.Uint64(results[48:]) != 2 || !bytes.Equal(results[56:], bytes.Repeat([]byte{0xa5}, len(results)-56)) || !bytes.Equal(j.CurrentBytes()[101:117], payload) {
				t.Fatal("combined length/tail/memory", pass)
			}
			table := swarTableOwnedBytes(tables.table, 8+4*32)
			dir := swarTableOwnedBytes(tables.functions, 4*40)
			if !bytes.Equal(table[8:40], dir[120:152]) || !bytes.Equal(table[40:], make([]byte, 3*32)) {
				t.Fatal("combined element installed actual backward-call canonical row")
			}
		}
		run(0, initial)
		updated := [2]uint64{initial[0] ^ input[0], initial[1] ^ input[1]}
		// An unrelated owner closes/rebinds without disturbing the other native
		// pointers. The old global result and fresh reload must track new cells.
		if err := globals.Close(); err != nil {
			t.Fatal(err)
		}
		after := j.CaptureInstanceContext()
		if after.GlobalsPtr != 0 || after.TablePtr != ctx.TablePtr || after.FuncRefDescPtr != ctx.FuncRefDescPtr || after.PassiveElemPtr != ctx.PassiveElemPtr || after.PassiveDataPtr != ctx.PassiveDataPtr {
			t.Fatal("global close damaged other state")
		}
		globals, err = image.instantiateGlobals(j)
		if err != nil {
			t.Fatal(err)
		}
		run(1, initial)
		dataDir := swarOwnedDataDirectory(data, 2)
		elemDir := swarTableOwnedBytes(tables.elements, 3*16)
		if binary.LittleEndian.Uint32(dataDir[24:]) != 0 || binary.LittleEndian.Uint64(dataDir[16:]) == 0 || binary.LittleEndian.Uint32(dataDir[28:]) != 0 || binary.LittleEndian.Uint32(elemDir[24:]) != 0 || binary.LittleEndian.Uint64(elemDir[16:]) == 0 || binary.LittleEndian.Uint32(elemDir[28:]) != 0 {
			t.Fatal("combined drop changed pointers/pads instead of lengths")
		}
		rows := bytes.Clone(swarTableOwnedBytes(tables.table, 8+4*32))
		oldData, oldElem := bytes.Clone(dataDir), bytes.Clone(elemDir)
		binary.LittleEndian.PutUint64(args[24:], dirty(0))
		for i := range results {
			results[i] = 0xa5
		}
		err = e.CallPrepared(entry(0), args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		pc := uint32(len(fixtures[0].locals) + bytes.Index(fixtures[0].body, []byte{0xfc, 8}))
		if !errors.As(err, &tr) || tr.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|1 || !bytes.Equal(swarTableOwnedBytes(tables.table, len(rows)), rows) || !bytes.Equal(dataDir, oldData) || !bytes.Equal(elemDir, oldElem) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("combined dropped-data cold source blocked subsequent element/call/results", pass, err)
		}
		clear(trap)
		// The preceding global.set executes before this trap: invocation failure
		// does not roll back prior guest writes. Its new value is initial again.
		cell := swarOwnedGlobalCell(globals, 0, 16)
		if binary.LittleEndian.Uint64(cell) != initial[0] || binary.LittleEndian.Uint64(cell[8:]) != initial[1] {
			t.Fatal("combined failure lost earlier ordered mutation")
		}
		if err := data.Close(); err != nil {
			t.Fatal(err)
		}
		after = j.CaptureInstanceContext()
		if after.PassiveDataPtr != 0 || after.GlobalsPtr != globals.directory || after.TablePtr != tables.table || after.FuncRefDescPtr != tables.functions || after.PassiveElemPtr != tables.elements {
			t.Fatal("data close damaged other state")
		}
		data, err = image.instantiateData(j)
		if err != nil {
			t.Fatal(err)
		}
		// Reset just table/element owner; global/data identities are preserved.
		if err := tables.Close(); err != nil {
			t.Fatal(err)
		}
		after = j.CaptureInstanceContext()
		if after.TablePtr != 0 || after.FuncRefDescPtr != 0 || after.PassiveElemPtr != 0 || after.GlobalsPtr != globals.directory || after.PassiveDataPtr != data.directory {
			t.Fatal("table close damaged other state")
		}
		tables = swarTableStateOwnerRV64(t, image, code, j)
		run(0, initial)
		if n := testing.AllocsPerRun(50, func() {
			if err := e.CallPrepared(entry(0), args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); n != 0 {
			t.Fatal("combined positive memory/table mutation/call warm allocations", n)
		}
		// Actual leaf entry still reads the currently bound global cells after all
		// independently rebound states. It also proves final code stayed callable.
		binary.LittleEndian.PutUint64(cell, updated[0]) // old cell is live: globals was not closed again
		binary.LittleEndian.PutUint64(cell[8:], updated[1])
		if err := e.CallPrepared(entry(1), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != input[0]^binary.LittleEndian.Uint64(payload)^updated[0] || binary.LittleEndian.Uint64(results[8:]) != input[1]^binary.LittleEndian.Uint64(payload[8:])^updated[1] {
			t.Fatal("combined rebound direct leaf", err)
		}
	}
}

func TestSWARRV64TableStateMaximumCompactVectorArenas(t *testing.T) {
	params := bytes.Repeat([]byte{0x7b}, 128)
	caller, leaf := fixtureFunc{params: params, results: params}, fixtureFunc{params: params, results: params, unexported: true}
	for i := 0; i < 128; i++ {
		caller.body = append(caller.body, 0x20, byte(i))
		leaf.body = append(leaf.body, 0x20, byte(127-i))
	}
	caller.body = append(caller.body, 0x41, 0, 0x11, 1, 0, 0x0b)
	leaf.body = append(leaf.body, 0x0b)
	image, code := swarTableStateImageRV64(t, swarTableStateSource([]fixtureFunc{caller, leaf}, 1, 1, []wasm.Elem{swarStateElement(wasm.ElemActive, 0, 1)}))
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	swarTableStateOwnerRV64(t, image, code, j)
	if image.functions[0].Params != 256 || image.functions[0].Results != 256 {
		t.Fatal("owner maximum compact slot metadata")
	}
	for i := 0; i < 256; i++ {
		binary.LittleEndian.PutUint64(args[i*8:], 0xfff00000deadbeef^uint64(i)*0x101010101)
	}
	input := bytes.Clone(args)
	entry := code.Base() + uintptr(image.functions[0].Entry)
	run := func() {
		copy(args, input)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}
	run()
	for i := 0; i < 128; i++ {
		if !bytes.Equal(results[i*16:(i+1)*16], input[(127-i)*16:(128-i)*16]) {
			t.Fatal("sealed owned maximum indirect vectors", i)
		}
	}
	if n := testing.AllocsPerRun(50, run); n != 0 {
		t.Fatal("sealed maximum indirect warm allocations", n)
	}
}
