//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func swarScalarMemoryExecutionFixture(t *testing.T) (*coreruntime.Engine, *coreruntime.JobMemory, []byte, []byte, []byte) {
	t.Helper()
	e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
	j, err := coreruntime.NewJobMemoryGrowable(65536, 131072)
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
	return e, j, args, results, trap
}

func swarScalarMemoryExpected(c swarScalarMemoryCase, data []byte) uint64 {
	var value uint64
	for i := 0; i < int(c.size); i++ {
		value |= uint64(data[i]) << (8 * i)
	}
	if c.signed {
		shift := 64 - 8*c.size
		value = uint64(int64(value<<shift) >> shift)
	}
	if c.class == 0x7f || c.class == 0x7d {
		value = uint64(uint32(value))
	}
	return value
}

func TestSWARRV64ScalarMemoryWithLiveVectorsBoundsAndRawFP(t *testing.T) {
	e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	for i := range mem {
		mem[i] = byte(i*17 + 0x83)
	}
	before := make([]byte, len(mem)) // One bounded reusable full-memory oracle.
	for _, c := range swarScalarMemoryCases() {
		for align := byte(0); align <= swarMemoryAlignment(int(c.size)); align++ {
			for _, offset := range []uint32{0, 1, 0xffffffff} {
				fn, ft, memory := swarValidatedMemoryFunction(t, swarScalarMemoryFixture(c, align, offset))
				code, info, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, functionIndex: 21})
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("large scalar-memory frame", c, info, err)
				}
				entry := thunkImageRV64(t, code)
				addresses := []uint32{0, 1, 2, 3, 4, 5, 6, 7, 65536 - uint32(c.size), 65537 - uint32(c.size), 65535, 65536, 0xffffffff}
				for _, address := range addresses {
					copy(before, mem)
					input := [4]uint64{dirty(address), 0x7ff0000000000001, 0x80000000deadbeef, 0xfff00000fedc80ff}
					for i, value := range input {
						binary.LittleEndian.PutUint64(args[i*8:], value)
					}
					for i := range results {
						results[i] = 0xa5
					}
					end := uint64(address) + uint64(offset) + uint64(c.size)
					err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
					if end > uint64(len(mem)) {
						var tr *coreruntime.TrapError
						pc := uint32(fn.LocalDeclBytes + 4)
						if c.store {
							pc += 2
						}
						if !errors.As(err, &tr) || tr.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|22 || !bytes.Equal(mem, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
							t.Fatal("scalar bounds/source/prewrite", c, align, offset, address, err, trap[16:24])
						}
						clear(trap)
						continue
					}
					start := uint64(address) + uint64(offset)
					value := input[3]
					if c.store {
						for i := uint64(0); i < uint64(c.size); i++ {
							before[start+i] = byte(value >> (8 * i))
						}
						if c.class == 0x7f || c.class == 0x7d {
							value = uint64(uint32(value))
						}
					} else {
						value = swarScalarMemoryExpected(c, before[start:])
					}
					if err != nil || binary.LittleEndian.Uint64(results) != input[1] || binary.LittleEndian.Uint64(results[8:]) != input[2] || binary.LittleEndian.Uint64(results[16:]) != value || !bytes.Equal(mem, before) || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
						t.Fatal("scalar access/raw/live vector/narrow/tail", c, align, offset, address, value, results[:24], err)
					}
					for i, value := range input {
						if binary.LittleEndian.Uint64(args[i*8:]) != value {
							t.Fatal("scalar access mutated argument arena", c, i)
						}
					}
				}
				if offset != 0xffffffff {
					binary.LittleEndian.PutUint64(args, dirty(8))
					if allocs := testing.AllocsPerRun(50, func() {
						if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
							panic(err)
						}
					}); allocs != 0 {
						t.Fatal("scalar-memory warm allocations", c, allocs)
					}
				}
			}
		}
	}
}

func TestSWARRV64ScalarMemorySizeGrowAndVectorReuse(t *testing.T) {
	e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
	var entries [2]uintptr
	for i, grow := range []bool{false, true} {
		fn, ft, memory := swarValidatedMemoryFunction(t, swarScalarMemorySizeFixture(grow))
		// The owner supplies the actual growable maximum, two pages here.
		memory.Limits.Max = 2
		code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entries[i] = thunkImageRV64(t, code)
	}
	vector := [2]uint64{0x7ff0000000000001, 0x80000000deadbeef}
	for _, test := range []struct {
		grow                 bool
		delta, result, pages uint32
	}{{false, 0, 1, 1}, {true, 0, 1, 1}, {true, 0xffffffff, 0xffffffff, 1}, {true, 1, 1, 2}, {false, 0, 2, 2}, {true, 1, 0xffffffff, 2}, {true, 0, 2, 2}} {
		binary.LittleEndian.PutUint64(args, vector[0])
		binary.LittleEndian.PutUint64(args[8:], vector[1])
		binary.LittleEndian.PutUint64(args[16:], dirty(test.delta))
		for i := range results {
			results[i] = 0xa5
		}
		index := 0
		if test.grow {
			index = 1
		}
		if err := e.CallPrepared(entries[index], args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != vector[0] || binary.LittleEndian.Uint64(results[8:]) != vector[1] || binary.LittleEndian.Uint64(results[16:]) != uint64(test.result) || len(j.CurrentBytes()) != int(test.pages)*65536 || !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
			t.Fatal("scalar size/grow/vector/normalization", test, err, results[:24], len(j.CurrentBytes()))
		}
	}
}

func TestSWARRV64ScalarMemoryUsesLogicalSizeAndNestedCallFrame(t *testing.T) {
	e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
	fixture := swarScalarMemoryFixture(swarScalarMemoryCase{0x39, 0x7c, 8, false, true}, 0, 0)
	caller := fixtureFunc{params: fixture.params, results: fixture.results, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}
	data := modifySection(binaryModule(caller, fixture), 7, 5, []byte{1, 1, 1, 2}, false)
	image, err := compileSWARModule(data, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
	mem := j.CurrentBytes()
	// This cache is authoritative, independently of mapped reservation or pages.
	sizeCell := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&mem[0]), -int(abi.ActualLinMemByteSize64Offset))), 8)
	binary.LittleEndian.PutUint64(sizeCell, 7)
	before := bytes.Clone(mem)
	binary.LittleEndian.PutUint64(args, dirty(0))
	binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[16:], 0x80000000deadbeef)
	binary.LittleEndian.PutUint64(args[24:], 0xfff0000000000001)
	for i := range results {
		results[i] = 0xa5
	}
	var tr *coreruntime.TrapError
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); !errors.As(err, &tr) || tr.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(9)<<32|2 || !bytes.Equal(mem, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("nested scalar store logical-size/prewrite/source", err, trap[16:24])
	}
	binary.LittleEndian.PutUint64(sizeCell, 65536)
	clear(trap)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(mem) != 0xfff0000000000001 || binary.LittleEndian.Uint64(results[16:]) != 0xfff0000000000001 {
		t.Fatal("nested scalar raw-FP store after trap reuse", err)
	}
}
