//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarMemoryExecutionFixture(t *testing.T) (*coreruntime.Engine, *coreruntime.JobMemory, []byte, []byte, []byte) {
	t.Helper()
	e, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	j, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	ar, err := coreruntime.NewArena(128)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	args, results, trap := ar.Alloc(32), ar.Alloc(16), ar.Alloc(coreruntime.TrapBufferBytes)
	j.SetStackFence(e.StackLimit())
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	return e, j, args, results, trap
}

func TestSWARRV64WholeMemoryBoundaryAndExactSource(t *testing.T) {
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	for _, store := range []bool{false, true} {
		for align := byte(0); align <= 4; align++ {
			for _, offset := range []uint32{0, 1, 0xffffffff} {
				t.Run(fmt.Sprintf("store%t/align%d/offset%x", store, align, offset), func(t *testing.T) {
					fixture := swarMemoryFixture(store, align, offset)
					// A large real frame exercises escaped spill addressing. The
					// nonzero local declaration also participates in the exact PC.
					fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
					fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
					mode := emissionMode{production: true, functionIndex: 17}
					code, info, err := compileSWARPairsWithMemory(fn, ft, memory, mode)
					if err != nil || info.FrameBytes <= 2048 {
						t.Fatal("large memory function frame", info, err)
					}
					entry := thunkImageRV64(t, code)
					opPC := uint32(fn.LocalDeclBytes + 2)
					if store {
						opPC += 2
					}
					for _, address := range []uint32{0, 1, 7, 15, 65519, 65520, 65521, 65535, 0xffffffff} {
						mem := j.CurrentBytes()
						for i := range mem {
							mem[i] = byte(i*17 + 3)
						}
						before := bytes.Clone(mem)
						binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(address))
						binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
						binary.LittleEndian.PutUint64(args[16:], 0x8000000000000000)
						for i := range results {
							results[i] = 0xa5
						}
						ea := uint64(address) + uint64(offset)
						good := ea <= uint64(len(mem))-16
						err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
						if !good {
							var bounds *coreruntime.TrapError
							if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds ||
								binary.LittleEndian.Uint64(trap[16:]) != uint64(opPC)<<32|18 ||
								!bytes.Equal(mem, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, 16)) {
								t.Fatal("exact whole-function trap/atomic access", address, err, trap, results)
							}
							continue
						}
						if err != nil {
							t.Fatal(address, err)
						}
						if store {
							want := bytes.Clone(before)
							copy(want[ea:ea+16], args[8:24])
							if !bytes.Equal(mem, want) || !bytes.Equal(results, args[8:24]) {
								t.Fatal("store preserves vector local/raw payload", address)
							}
						} else if !bytes.Equal(mem, before) || !bytes.Equal(results, mem[ea:ea+16]) {
							t.Fatal("whole-function load", address)
						}
					}
					// Reuse after the final bounds trap.
					if offset < 2 {
						binary.LittleEndian.PutUint64(args, 0xdeadbeef00000000)
						if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
							t.Fatal("memory trap poisoned reuse", err)
						}
					}
				})
			}
		}
	}
}

func TestSWARRV64WholeMemorySequentialTrapsAndZeroAllocation(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	fixture := fixtureFunc{params: []byte{0x7f, 0x7f, 0x7b}, result: 0x7b,
		body: []byte{0x20, 0, 0x20, 2, 0xfd, 11, 0, 0, 0x20, 1, 0xfd, 0, 0, 0, 0x0b}}
	fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
	code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, functionIndex: 31})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, code)
	binary.LittleEndian.PutUint64(args[16:], 0x8877665544332211)
	binary.LittleEndian.PutUint64(args[24:], 0xffeeddccbbaa0099)
	for _, addresses := range [][2]uint32{{65521, 0}, {0, 65521}, {1, 1}} {
		mem := j.CurrentBytes()
		clear(mem)
		binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(addresses[0]))
		binary.LittleEndian.PutUint64(args[8:], 0xdeadbeef00000000|uint64(addresses[1]))
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		want := make([]byte, len(mem))
		if addresses[0] == 65521 || addresses[1] == 65521 {
			pc := uint32(fn.LocalDeclBytes + 4)
			if addresses[0] != 65521 {
				pc = uint32(fn.LocalDeclBytes + 10)
				copy(want[:16], args[16:32]) // The earlier successful store remains.
			}
			var bounds *coreruntime.TrapError
			if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds ||
				binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|32 ||
				!bytes.Equal(results, bytes.Repeat([]byte{0xa5}, 16)) || !bytes.Equal(mem, want) {
				t.Fatal("per-instruction source/write ordering", addresses, err, trap)
			}
		} else {
			copy(want[1:17], args[16:32])
			if err != nil || !bytes.Equal(mem, want) || !bytes.Equal(results, args[16:32]) {
				t.Fatal("store/load reuse", err)
			}
		}
	}
	call := func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}
	if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
		t.Fatal("whole memory native allocations", allocs)
	}
}
