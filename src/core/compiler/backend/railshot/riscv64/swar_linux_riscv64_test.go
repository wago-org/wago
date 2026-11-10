//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"math/rand"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarFramedProbe(t *testing.T, f *integerEmitter, params, slots int) []byte {
	t.Helper()
	if f.err != nil || f.a.Err() != nil {
		t.Fatal(f.err, f.a.Err())
	}
	end := f.Jump()
	for _, tr := range f.traps {
		if err := f.Patch(tr.site, f.Position()); err != nil {
			t.Fatal(err)
		}
		f.emitProductionTrap(productionTrapCode(tr.code), tr.sourcePC)
	}
	if err := f.Patch(end, f.Position()); err != nil {
		t.Fatal(err)
	}
	widths := make([]bool, params)
	for i := range widths {
		widths[i] = true
	}
	code, _, err := frameFunction(f.a.B, widths, params, slots, false, -1, FunctionInfo{mode: f.mode})
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestSWARRV64PairBitsAliasesAndLargeHomes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 6)
	pairs := []swarPair{{enc.X5, enc.X6}, {enc.X6, enc.X5}, {enc.X7, enc.X9}, {enc.X9, enc.X7}, {enc.X10, enc.X11}, {enc.X11, enc.X10}, {enc.X5, enc.X9}, {enc.X10, enc.X6}}
	ops := []wasm.InstrKind{wasm.InstrV128Not, wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor, wasm.InstrV128Bitselect}
	rng := rand.New(rand.NewSource(640128))
	for _, op := range ops {
		for _, dst := range pairs {
			f := integerEmitter{target: target{mode: emissionMode{production: true}}}
			f.swarLoad(16, swarPair{enc.X5, enc.X6})
			f.swarLoad(32, swarPair{enc.X7, enc.X9})
			f.swarLoad(48, swarPair{enc.X10, enc.X11})
			f.swarBits(op, dst, swarPair{enc.X5, enc.X6}, swarPair{enc.X7, enc.X9}, swarPair{enc.X10, enc.X11})
			f.swarStore(4096, dst)
			f.swarLoad(4096, swarPair{enc.X5, enc.X6})
			f.a.Ld(enc.Scratch1, enc.SP, 8)
			f.a.Sd(enc.X5, enc.Scratch1, 0)
			f.a.Sd(enc.X6, enc.Scratch1, 8)
			entry := thunkImageRV64(t, swarFramedProbe(t, &f, 6, 506))
			for n := 0; n < 32; n++ {
				var input [6]uint64
				for i := range input {
					input[i] = rng.Uint64()
					binary.LittleEndian.PutUint64(args[8*i:], input[i])
				}
				if n == 0 {
					for i := range input {
						input[i] = 0
						binary.LittleEndian.PutUint64(args[8*i:], 0)
					}
				}
				if n == 1 {
					for i := range input {
						input[i] = ^uint64(0)
						binary.LittleEndian.PutUint64(args[8*i:], input[i])
					}
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					t.Fatal(op, dst, err)
				}
				for half := 0; half < 2; half++ {
					a, b, m := input[half], input[2+half], input[4+half]
					var want uint64
					switch op {
					case wasm.InstrV128Not:
						want = ^a
					case wasm.InstrV128And:
						want = a & b
					case wasm.InstrV128Andnot:
						want = a &^ b
					case wasm.InstrV128Or:
						want = a | b
					case wasm.InstrV128Xor:
						want = a ^ b
					case wasm.InstrV128Bitselect:
						want = a&m | b&^m
					}
					if got := binary.LittleEndian.Uint64(results[8*half:]); got != want {
						t.Fatalf("op%d dst%v half%d got%x want%x", op, dst, half, got, want)
					}
				}
			}
		}
	}
	for _, dst := range pairs {
		f := integerEmitter{target: target{mode: emissionMode{production: true}}}
		f.swarLoad(16, swarPair{enc.X5, enc.X6})
		f.swarMove(dst, swarPair{enc.X5, enc.X6})
		f.swarStore(2040, dst)
		f.swarLoad(2040, swarPair{enc.X5, enc.X6})
		f.a.Ld(enc.Scratch1, enc.SP, 8)
		f.a.Sd(enc.X5, enc.Scratch1, 0)
		f.a.Sd(enc.X6, enc.Scratch1, 8)
		entry := thunkImageRV64(t, swarFramedProbe(t, &f, 2, 254))
		binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
		binary.LittleEndian.PutUint64(args[8:], 0x8000000000000000)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(args[:16], results[:16]) {
			t.Fatal("pair move/boundary raw bits", dst, err)
		}
	}
}

func TestSWARRV64FullWidthMemoryPreflight(t *testing.T) {
	e, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	j, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	j.SetStackFence(e.StackLimit())
	ar, err := coreruntime.NewArena(128)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	args, results, trap := ar.Alloc(24), ar.Alloc(16), ar.Alloc(coreruntime.TrapBufferBytes)
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	for _, store := range []bool{false, true} {
		for _, offset := range []uint64{0, 1, 0xffffffff} {
			t.Run(fmt.Sprintf("store%t/offset%x", store, offset), func(t *testing.T) {
				f := integerEmitter{target: target{mode: emissionMode{production: true, functionIndex: 7}, sourcePC: 91}}
				f.Load(5, 16, true)
				f.Load(6, 24, true)
				f.Load(7, 32, true)
				f.swarMemory(offset, store)
				f.a.Ld(enc.Scratch1, enc.SP, 8)
				f.a.Sd(enc.X6, enc.Scratch1, 0)
				f.a.Sd(enc.X7, enc.Scratch1, 8)
				entry := thunkImageRV64(t, swarFramedProbe(t, &f, 3, 0))
				for _, address := range []uint32{0, 1, 7, 15, 65519, 65520, 65521, 65535, 0xffffffff} {
					memory := j.CurrentBytes()
					for i := range memory {
						memory[i] = byte(i*17 + 3)
					}
					before := bytes.Clone(memory)
					binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(address))
					binary.LittleEndian.PutUint64(args[8:], 0x8877665544332211)
					binary.LittleEndian.PutUint64(args[16:], 0xffeeddccbbaa0099)
					for i := range results {
						results[i] = 0xa5
					}
					ea := uint64(address) + offset
					good := ea <= uint64(len(memory))-16
					err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
					if !good {
						var bounds *coreruntime.TrapError
						if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds {
							t.Fatal("exact pair memory trap", err)
						}
						if err == nil || !bytes.Equal(memory, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, 16)) {
							t.Fatal("split access changed state", address, err)
						}
						if binary.LittleEndian.Uint64(trap[16:]) != uint64(91)<<32|8 {
							t.Fatal("source trap", trap)
						}
						continue
					}
					if err != nil {
						t.Fatal(address, err)
					}
					if store {
						want := bytes.Clone(before)
						copy(want[ea:ea+16], args[8:24])
						if !bytes.Equal(memory, want) {
							t.Fatal("full pair store", address)
						}
					} else if !bytes.Equal(results, memory[ea:ea+16]) || !bytes.Equal(memory, before) {
						t.Fatal("full pair load", address)
					}
				}
			})
		}
	}
}

func TestSWARRV64ShortLogicalMemoryRejectsBeforeBytes(t *testing.T) {
	e, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	j, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	j.SetStackFence(e.StackLimit())
	ar, err := coreruntime.NewArena(128)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	args, results, trap := ar.Alloc(24), ar.Alloc(16), ar.Alloc(coreruntime.TrapBufferBytes)
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	memory := j.CurrentBytes()
	// Trusted descriptor probe: ordinary Wasm memories still grow by pages.
	// Retain the full mapping while varying only the authoritative size cache.
	sizeCell := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&memory[0]), -int(abi.ActualLinMemByteSize64Offset))), 8)
	original := binary.LittleEndian.Uint64(sizeCell)
	defer binary.LittleEndian.PutUint64(sizeCell, original)
	for _, store := range []bool{false, true} {
		f := integerEmitter{target: target{mode: emissionMode{production: true}}}
		f.Load(5, 16, true)
		f.Load(6, 24, true)
		f.Load(7, 32, true)
		f.swarMemory(0, store)
		f.a.Ld(enc.Scratch1, enc.SP, 8)
		f.a.Sd(enc.X6, enc.Scratch1, 0)
		f.a.Sd(enc.X7, enc.Scratch1, 8)
		entry := thunkImageRV64(t, swarFramedProbe(t, &f, 3, 0))
		for _, size := range []uint64{0, 15, 16, 31} {
			for _, addr := range []uint64{0, 1, 15, 16} {
				for i := range memory {
					memory[i] = byte(i*13 + 7)
				}
				before := bytes.Clone(memory)
				binary.LittleEndian.PutUint64(sizeCell, size)
				binary.LittleEndian.PutUint64(args, addr|0xdeadbeef00000000)
				binary.LittleEndian.PutUint64(args[8:], 0x8877665544332211)
				binary.LittleEndian.PutUint64(args[16:], 0xffeeddccbbaa0099)
				for i := range results {
					results[i] = 0xa5
				}
				err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
				good := size >= 16 && addr <= size-16
				if !good {
					var bounds *coreruntime.TrapError
					if !errors.As(err, &bounds) || bounds.Code != coreruntime.TrapLinMemOutOfBounds {
						t.Fatal("exact pair memory trap", err)
					}
					if err == nil || !bytes.Equal(memory, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, 16)) {
						t.Fatal("short logical memory partially accessed", store, size, addr, err)
					}
				} else if err != nil {
					t.Fatal("exact full-width logical memory", store, size, addr, err)
				} else if store {
					want := bytes.Clone(before)
					copy(want[addr:addr+16], args[8:24])
					if !bytes.Equal(memory, want) {
						t.Fatal("short memory store", size, addr)
					}
				} else if !bytes.Equal(results, memory[addr:addr+16]) || !bytes.Equal(memory, before) {
					t.Fatal("short memory load", size, addr)
				}
			}
		}
	}
}
