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

func swarPartialLoadFootprint(kind uint32) uint64 {
	if kind == 92 {
		return 4
	}
	return 8
}

func swarPartialLoadOracle(input []byte, kind uint32) [16]byte {
	var out [16]byte
	copy(out[:], input)
	if kind <= 6 {
		return swarWidenOracle(out[:], 1<<((kind-1)/2), kind%2 == 1, false, false)
	}
	return out
}

func TestSWARRV64PartialLoadsExactFootprintAndSource(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	for i := range mem {
		mem[i] = byte(i*17 + 3)
	}
	binary.LittleEndian.PutUint64(mem, 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(mem[8:], 0x8000000000000000)
	before := bytes.Clone(mem)
	sentinel := bytes.Repeat([]byte{0xa5}, 16)
	for _, kind := range swarPartialLoadKinds {
		maxAlign, _ := swarMemoryOpcode(kind)
		footprint := swarPartialLoadFootprint(kind)
		for align := uint32(0); align <= maxAlign; align++ {
			for _, offset := range []uint32{0, 1, ^uint32(0)} {
				fixture := swarPartialLoadFixture(kind, align, offset)
				fixture.locals = []byte{1, 127, 0x7b}
				fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
				code, info, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, functionIndex: 23})
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("large partial load frame", kind, align, info, err)
				}
				entry := thunkImageRV64(t, code)
				pc := uint32(fn.LocalDeclBytes + 2)
				for _, address := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 15, 65527, 65528, 65529, 65531, 65532, 65533, 65535, ^uint32(0)} {
					binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(address))
					argBefore := binary.LittleEndian.Uint64(args)
					copy(results, sentinel)
					ea := uint64(address) + uint64(offset)
					good := ea <= uint64(len(mem))-footprint
					err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
					if !good {
						var bounds *coreruntime.TrapError
						if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|24 || !bytes.Equal(results, sentinel) {
							t.Fatal("partial exact bounds/source/result", kind, address, offset, err, trap, results)
						}
					} else {
						want := swarPartialLoadOracle(mem[ea:ea+footprint], kind)
						if err != nil || !bytes.Equal(results, want[:]) {
							t.Fatal("partial exact footprint/extension/zero", kind, address, offset, results, want, err)
						}
					}
					if !bytes.Equal(mem, before) || binary.LittleEndian.Uint64(args) != argBefore {
						t.Fatal("partial load mutated memory/argument", kind, address)
					}
				}
				if offset < 2 {
					binary.LittleEndian.PutUint64(args, 0xdeadbeef00000000)
					call := func() {
						if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
							panic(err)
						}
					}
					call() // Reuse after the final bounds trap.
					if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
						t.Fatal("partial load allocations", kind, allocs)
					}
				}
			}
		}
	}
}

func TestSWARRV64PartialLoadsShortLogicalMemory(t *testing.T) {
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	for i := range mem {
		mem[i] = byte(i*13 + 7)
	}
	before := bytes.Clone(mem)
	// Trusted descriptor probe retains the actual mapping and varies only its
	// authoritative byte-size cache. Ordinary Wasm memories still use pages.
	sizeCell := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&mem[0]), -int(abi.ActualLinMemByteSize64Offset))), 8)
	original := binary.LittleEndian.Uint64(sizeCell)
	defer binary.LittleEndian.PutUint64(sizeCell, original)
	sentinel := bytes.Repeat([]byte{0xa5}, 16)
	for _, kind := range swarPartialLoadKinds {
		fn, ft, memory := swarValidatedMemoryFunction(t, swarPartialLoadFixture(kind, 0, 0))
		code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true, functionIndex: 41})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		footprint := swarPartialLoadFootprint(kind)
		for _, size := range []uint64{0, 3, 4, 7, 8, 15} {
			for _, address := range []uint64{0, 1, 3, 4, 7, 8} {
				binary.LittleEndian.PutUint64(sizeCell, size)
				binary.LittleEndian.PutUint64(args, 0xdeadbeef00000000|address)
				copy(results, sentinel)
				good := size >= footprint && address <= size-footprint
				err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
				if !good {
					var bounds *coreruntime.TrapError
					pc := uint32(fn.LocalDeclBytes + 2)
					if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(pc)<<32|42 || !bytes.Equal(results, sentinel) {
						t.Fatal("short partial bounds/source", kind, size, address, err, trap, results)
					}
				} else {
					want := swarPartialLoadOracle(mem[address:address+footprint], kind)
					if err != nil || !bytes.Equal(results, want[:]) {
						t.Fatal("short exact footprint", kind, size, address, results, want, err)
					}
				}
				if !bytes.Equal(mem, before) {
					t.Fatal("short partial load modified memory", kind, size, address)
				}
			}
		}
	}
}

func TestSWARRV64ExtendedByteLoadsEveryValueAtEveryLane(t *testing.T) {
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	mem := j.CurrentBytes()
	for i := range mem {
		mem[i] = byte(i*17 + 3)
	}
	for _, kind := range []uint32{1, 2} {
		fn, ft, memory := swarValidatedMemoryFunction(t, swarPartialLoadFixture(kind, 0, 0))
		code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, code)
		for address := uint64(0); address < 256; address++ {
			binary.LittleEndian.PutUint64(args, address|0xdeadbeef00000000)
			want := swarPartialLoadOracle(mem[address:address+8], kind)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results, want[:]) {
				t.Fatal("every byte extending load", kind, address, results, want, err)
			}
		}
	}
}
