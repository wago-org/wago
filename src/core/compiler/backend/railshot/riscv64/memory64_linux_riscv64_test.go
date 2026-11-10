//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func memory64NativeJob(t *testing.T, e *coreruntime.Engine, trap []byte) (*coreruntime.JobMemory, []byte) {
	t.Helper()
	j, err := coreruntime.NewJobMemoryGrowable(65536, 2*65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	j.SetStackFence(e.StackLimit())
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	arena, err := coreruntime.NewArena(MaxProductionMemories * abi.MemoryDirEntryBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		j.SetMemoryDirPtr(0)
		if err := arena.Close(); err != nil {
			t.Error(err)
		}
	})
	dir := arena.Alloc(MaxProductionMemories * abi.MemoryDirEntryBytes)
	for i := 0; i < MaxProductionMemories; i++ {
		row := dir[i*abi.MemoryDirEntryBytes:]
		binary.LittleEndian.PutUint64(row, uint64(j.LinMemBase()))
		binary.LittleEndian.PutUint64(row[8:], 0xfedcba9876543210)
		binary.LittleEndian.PutUint32(row[16:], 0xffffffff)
	}
	j.SetMemoryDirPtr(thunkPointerRV64(dir))
	return j, dir
}

func TestProductionMemory64CarryBeforeAnyScalarOrVectorStore(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, index := range []byte{0, 99} {
			e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
			j, _ := memory64NativeJob(t, e, trap)
			value, footprint := wasm.I64, 8
			if vector {
				value, footprint = wasm.V128, 16
			}
			for _, offset := range []uint64{1, ^uint64(0)} {
				body := []byte{0x20, 0, 0x20, 1}
				if vector {
					body = append(body, 0xfd, 11)
				} else {
					body = append(body, 0x37)
				}
				body = append(body, 64, index)
				body = append(body, memory64ULEB(offset)...)
				body = append(body, 0x42, 17, 0x0b)
				m := memory64BackendModule(t, []int{int(index)}, []wasm.ValType{wasm.I64, value}, []wasm.ValType{wasm.I64}, body, false)
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
				if err != nil {
					t.Fatal(err)
				}
				entry := thunkImageRV64(t, image.Code) + uintptr(image.Entry[0])
				invoke := func(address uint64, fail bool) {
					t.Helper()
					memory := j.CurrentBytes()
					for i := range memory {
						memory[i] = 0x5a
					}
					before := bytes.Clone(memory)
					for i := range results {
						results[i] = 0xa5
					}
					binary.LittleEndian.PutUint64(args, address)
					binary.LittleEndian.PutUint64(args[8:], 0x8877665544332211)
					binary.LittleEndian.PutUint64(args[16:], 0xffeeddccbbaa0099)
					argBefore := bytes.Clone(args)
					err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
					if !bytes.Equal(args, argBefore) {
						t.Fatal("argument payload changed")
					}
					if fail {
						var te *coreruntime.TrapError
						if !errors.As(err, &te) || te.Code != coreruntime.TrapLinMemOutOfBounds || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].ProgramCounter != 5 || !te.Frames[0].HasProgramCounter {
							t.Fatal("carry/bounds exact source", vector, index, offset, address, err)
						}
						if !bytes.Equal(memory, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
							t.Fatal("failed split store published a byte/result")
						}
					} else {
						if err != nil || binary.LittleEndian.Uint64(results) != 17 {
							t.Fatal("valid store/reuse", err)
						}
						copy(before[int(address+offset):], argBefore[8:8+footprint])
						if !bytes.Equal(memory, before) || !bytes.Equal(results[8:], bytes.Repeat([]byte{0xa5}, len(results)-8)) {
							t.Fatal("full payload/tail mismatch", vector, index)
						}
					}
				}
				for _, address := range []uint64{^uint64(0), ^uint64(0) - 1, 1 << 32, 1 << 63} {
					invoke(address, true)
				}
				if offset == 1 {
					invoke(uint64(65536-footprint), true)
					invoke(uint64(65536-footprint-1), false)
					invoke(2, false)
				} else {
					invoke(1, true)
				} // wraps to zero without the explicit carry guard
			}
		}
	}
}

func TestProductionMemory64GrowFullDeltaAndAliasQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
		j, dir := memory64NativeJob(t, e, trap)
		body := []byte{0x20, 0, 0x40, 99, 0x3f, 0, 0x3f, 99, 0x0b}
		m := memory64BackendModule(t, []int{0, 99}, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, body, vector)
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, image.Code) + uintptr(image.Entry[0])
		row := dir[99*abi.MemoryDirEntryBytes:]
		binary.LittleEndian.PutUint32(row[20:], 1)
		before := bytes.Clone(dir)
		invoke := func(delta uint64, want [3]uint64) {
			t.Helper()
			binary.LittleEndian.PutUint64(args, delta)
			for i := range results {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal(err)
			}
			for i, v := range want {
				if got := binary.LittleEndian.Uint64(results[i*8:]); got != v {
					t.Fatal("full64 grow/alias", vector, delta, i, got, v)
				}
			}
			if !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
				t.Fatal("grow result tail")
			}
		}
		for _, delta := range []uint64{1, 1 << 32, 1 << 63, ^uint64(0)} {
			invoke(delta, [3]uint64{^uint64(0), 1, 1})
			if !bytes.Equal(dir, before) || j.CurrentPages() != 1 || len(j.CurrentBytes()) != 65536 {
				t.Fatal("failed grow published cache/length")
			}
		}
		binary.LittleEndian.PutUint32(row[20:], 2)
		invoke(1, [3]uint64{1, 2, 2})
		if binary.LittleEndian.Uint32(row[16:]) != 2 || binary.LittleEndian.Uint64(row[8:]) != 2*65536 || binary.LittleEndian.Uint32(dir[16:]) != 0xffffffff {
			t.Fatal("selected vs aliased authoritative cache")
		}
		binary.LittleEndian.PutUint32(row[20:], 1)
		before = bytes.Clone(dir)
		invoke(0, [3]uint64{2, 2, 2})
		invoke(^uint64(0), [3]uint64{^uint64(0), 2, 2})
		if !bytes.Equal(dir, before) || len(j.CurrentBytes()) != 2*65536 || !bytes.Equal(j.CurrentBytes()[65536:], make([]byte, 65536)) {
			t.Fatal("zero/failed grow changed cache or zero reserve")
		}
		if allocs := testing.AllocsPerRun(50, func() {
			binary.LittleEndian.PutUint64(args, 0)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("warm memory64 growth allocations", allocs)
		}
	}
}
