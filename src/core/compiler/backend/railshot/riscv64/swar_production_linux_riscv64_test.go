//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarProductionEntryRV64(t *testing.T, source []byte, heap bool) uintptr {
	t.Helper()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := compileSWARProductionModule(m, CompileOptions{DeferCodeMapping: heap, Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	var mapping []byte
	var base uintptr
	if heap {
		mapping, base, err = coreruntime.MapCode(image.Code)
	} else {
		t.Cleanup(func() {
			if err := image.CodeImage.Close(); err != nil {
				t.Error(err)
			}
		})
		mapping, base, err = image.CodeImage.Take()
	}
	t.Cleanup(func() {
		if err := coreruntime.Unmap(mapping); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !heap {
		if err := coreruntime.SealCode(mapping); err != nil {
			t.Fatal(err)
		}
	}
	return base + uintptr(image.Entry[0])
}

func TestSWARProductionRV64SharedImageMixedCallsAndRawResults(t *testing.T) {
	for _, heap := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		entry := swarProductionEntryRV64(t, binaryModule(swarCallFixtures()...), heap)
		for _, pair := range dirtyInputs() {
			input := [8]uint64{pair[0], 0x7ff0000000000001, pair[1], pair[1], pair[0], ^pair[1], ^pair[0], 0x8000000000000000}
			write := func() {
				for i, v := range input {
					binary.LittleEndian.PutUint64(args[i*8:], v)
				}
			}
			write()
			for i := range results {
				results[i] = 0xa5
			}
			want := [10]uint64{input[5], input[6], input[1] ^ input[5], input[2] ^ input[6], uint64(uint32(input[0])), input[7], input[5], input[6], uint64(uint32(input[3])), input[4]}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal("shared image mixed call", heap, err)
			}
			for i, v := range want {
				if binary.LittleEndian.Uint64(results[i*8:]) != v {
					t.Fatal("shared image raw/narrow/live result", heap, i)
				}
			}
			if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) {
				t.Fatal("result tail")
			}
			if n := testing.AllocsPerRun(50, func() {
				write()
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("warm allocation", n)
			}
		}
	}
}

func TestSWARProductionRV64SharedImageStackExhaustionSourceAndReuse(t *testing.T) {
	for _, heap := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		entry := swarProductionEntryRV64(t, binaryModule(swarRecursiveCallFixture()), heap)
		vector := []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80}
		for _, depth := range []uint32{17, ^uint32(0), 1} {
			copy(args, vector)
			binary.LittleEndian.PutUint64(args[16:], dirty(depth))
			for i := range results {
				results[i] = 0xa5
			}
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			if depth == ^uint32(0) {
				var tr *coreruntime.TrapError
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(1)<<32|1 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("stack/source/prewrite", heap, err)
				}
				clear(trap)
				continue
			}
			if err != nil || !bytes.Equal(results[:16], vector) || !bytes.Equal(results[16:], bytes.Repeat([]byte{0xa5}, len(results)-16)) {
				t.Fatal("raw recursive result/reuse", heap, depth, err)
			}
		}
	}
}

func TestSWARProductionRV64SharedImageWholeCalleeFrameFenceBeforeWrites(t *testing.T) {
	caller := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 1, 0x0b}}
	leaf := fixtureFunc{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 127, 0x7b}, body: []byte{0x20, 0, 0x0b}}
	source := binaryModule(caller, leaf)
	reference, err := compileSWARModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, heap := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		entry := swarProductionEntryRV64(t, source, heap)
		var probe enc.Asm
		probe.Ld(enc.Scratch1, enc.SP, 8)
		probe.Sd(enc.SP, enc.Scratch1, 0)
		code, info, err := frameFunction(probe.B, nil, 0, 0, false, -1, FunctionInfo{mode: emissionMode{production: true}})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.CallPrepared(thunkImageRV64(t, code), args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		bridgeSP := uintptr(binary.LittleEndian.Uint64(results)) + uintptr(info.FrameBytes)
		calleeSP := bridgeSP - uintptr(reference.functions[0].FrameBytes)
		frameStart := calleeSP - uintptr(reference.functions[1].FrameBytes)
		if frameStart < e.StackLimit() || frameStart >= calleeSP || calleeSP > e.StackTop() {
			t.Fatal("sentinel outside owned mmap stack")
		}
		// Only the known engine-owned mmap address is converted, never Go storage.
		pointer := *(*unsafe.Pointer)(unsafe.Pointer(&frameStart))
		frame := unsafe.Slice((*byte)(pointer), reference.functions[1].FrameBytes)
		for i := range frame {
			frame[i] = 0x6b
		}
		before := bytes.Clone(frame)
		j.SetStackFence(frameStart + 16)
		for i := range results {
			results[i] = 0xa5
		}
		var tr *coreruntime.TrapError
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(3)<<32|2 || !bytes.Equal(frame, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("shared image callee frame changed before fence", heap, err)
		}
		j.SetStackFence(e.StackLimit())
		clear(trap)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal("shared image fenced reuse", heap, err)
		}
	}
}
