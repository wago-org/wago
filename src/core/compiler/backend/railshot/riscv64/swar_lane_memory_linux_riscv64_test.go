//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestSWARRV64SplatAndLaneMemoryAllLanesAndFootprints(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	for i := range mem {
		mem[i] = byte(i*17 + 3)
	}
	before := bytes.Clone(mem)
	wantMem := bytes.Clone(mem)
	sentinel := bytes.Repeat([]byte{0xa5}, 16)
	// Trusted descriptor probe keeps all pages mapped; restore before cleanup.
	sizeCell := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&mem[0]), -int(abi.ActualLinMemByteSize64Offset))), 8)
	original := binary.LittleEndian.Uint64(sizeCell)
	defer binary.LittleEndian.PutUint64(sizeCell, original)
	kinds := []uint32{7, 8, 9, 10, 84, 85, 86, 87, 88, 89, 90, 91}
	for _, kind := range kinds {
		align, _ := swarMemoryOpcode(kind)
		footprint := uint64(1) << align
		laneOp := kind >= 84
		store := kind >= 88
		laneCount := byte(1)
		if laneOp {
			laneCount = byte(16 / footprint)
		}
		for lane := byte(0); lane < laneCount; lane++ {
			for _, offset := range []uint32{0, 1, ^uint32(0)} {
				fixture := swarPartialLoadFixture(kind, align, offset)
				if laneOp {
					fixture = swarLaneMemoryFixture(kind, align, offset, lane)
				}
				fixture.locals = []byte{1, byte(128 - len(fixture.params)), 0x7b}
				fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
				code, info, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, functionIndex: 53})
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("large lane/splat memory frame", kind, lane, info, err)
				}
				entry := thunkImageRV64(t, code)
				pc := uint32(fn.LocalDeclBytes + 2)
				if laneOp {
					pc += 2
				}
				sizes := []uint64{original}
				if offset == 0 {
					sizes = []uint64{0, 1, 2, 3, 4, 7, 8, 15, original}
				}
				for _, size := range sizes {
					binary.LittleEndian.PutUint64(sizeCell, size)
					addresses := []uint32{0, 1, 2, 3, 4, 5, 6, 7, ^uint32(0)}
					if size >= footprint {
						addresses = append(addresses, uint32(size-footprint), uint32(size-footprint+1))
					}
					for _, address := range addresses {
						binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(address))
						binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
						binary.LittleEndian.PutUint64(args[16:], 0x8000000000000000)
						var argBefore [24]byte
						copy(argBefore[:], args[:24])
						copy(results, sentinel)
						ea := uint64(address) + uint64(offset)
						good := size >= footprint && ea <= size-footprint
						err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
						if !good {
							var bounds *coreruntime.TrapError
							if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|54 || !bytes.Equal(results, sentinel) || !bytes.Equal(mem, before) {
								t.Fatal("lane/splat preflight/source/sentinels", kind, lane, offset, size, address, err, trap, results)
							}
						} else {
							var want [16]byte
							if laneOp {
								copy(want[:], argBefore[8:])
								start := uint64(lane) * footprint
								if store {
									copy(wantMem[ea:ea+footprint], argBefore[8+start:8+start+footprint])
								} else {
									copy(want[start:start+footprint], before[ea:ea+footprint])
								}
							} else {
								for start := uint64(0); start < 16; start += footprint {
									copy(want[start:start+footprint], before[ea:ea+footprint])
								}
							}
							if err != nil || !bytes.Equal(results, want[:]) || !bytes.Equal(mem, wantMem) {
								t.Fatal("lane/splat payload/memory/canonical local", kind, lane, offset, size, address, results, want, err)
							}
							if store {
								copy(mem[ea:ea+footprint], before[ea:ea+footprint])
								copy(wantMem[ea:ea+footprint], before[ea:ea+footprint])
							}
						}
						if !bytes.Equal(args[:24], argBefore[:]) {
							t.Fatal("lane memory changed raw arguments", kind, lane, address)
						}
					}
				}
				binary.LittleEndian.PutUint64(sizeCell, original)
				if offset < 2 {
					binary.LittleEndian.PutUint64(args, 0xdeadbeef00000000)
					call := func() {
						if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
							panic(err)
						}
					}
					call() // Reuse after the final failed access.
					if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
						t.Fatal("lane/splat memory allocations", kind, lane, allocs)
					}
					copy(mem, before)
				}
			}
		}
	}
}
