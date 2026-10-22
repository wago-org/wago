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

func TestSWARRV64ActiveDataInitializationVectorUseAndDroppedIndices(t *testing.T) {
	segments := make([]wasm.Data, 256)
	for i := range segments {
		payload := []byte{byte(i), byte(i >> 1), byte(i ^ 0xa5)}
		if i%3 == 0 {
			segments[i] = swarActiveSegment(uint32(i*2), payload)
		} else {
			segments[i] = wasm.Data{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: payload}
		}
	}
	segments[255] = swarActiveSegment(0, []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80})
	segments[127] = swarActiveSegment(65536, nil)
	load := fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 0, 0x20, 1, 0xfd, 0, 0, 0, 0xfd, 81, 0x0b}}
	fixtures := []fixtureFunc{load, swarDataFixture(255, false), swarDataFixture(255, true), swarDataFixture(254, false)}
	source := swarMixedDataSource(fixtures, segments)
	image, err := compileSWARModule(source, emissionMode{production: true, interruptible: true})
	if err != nil || image.functions[0].FrameBytes <= 2048 {
		t.Fatal("active vector module", err)
	}
	clear(source)
	entry := thunkImageRV64(t, image.code)
	for pass := range 2 {
		e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
		mem := j.CurrentBytes()
		for i := range mem {
			mem[i] = 0xa5
		}
		want := bytes.Clone(mem)
		for _, segment := range segments {
			if segment.Mode.Kind == wasm.DataActive {
				off, err := plainScalarConst(segment.Mode.Offset, wasm.I32, false)
				if err != nil {
					t.Fatal(err)
				}
				copy(want[int(off):], segment.Init)
			}
		}
		owner, err := image.instantiateData(j)
		if err != nil || !bytes.Equal(mem, want) {
			t.Fatal("actual full initialization/source overlap order", pass, err)
		}
		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
		dir := swarOwnedDataDirectory(owner, 256)
		for i, segment := range segments {
			desc := dir[i*16 : (i+1)*16]
			if segment.Mode.Kind == wasm.DataActive {
				if !bytes.Equal(desc, make([]byte, 16)) {
					t.Fatal("active segment descriptor not dropped", pass, i)
				}
			} else if !bytes.Equal(swarOwnedDataPayload(desc), segment.Init) {
				t.Fatal("passive index/payload changed", pass, i)
			}
		}
		for _, pair := range dirtyInputs() {
			binary.LittleEndian.PutUint64(args, pair[0])
			binary.LittleEndian.PutUint64(args[8:], pair[1])
			binary.LittleEndian.PutUint64(args[16:], dirty(0))
			for i := range results {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != pair[0]^binary.LittleEndian.Uint64(want) || binary.LittleEndian.Uint64(results[8:]) != pair[1]^binary.LittleEndian.Uint64(want[8:]) || !bytes.Equal(results[16:], bytes.Repeat([]byte{0xa5}, len(results)-16)) {
				t.Fatal("active rawbytes vector load/XOR/tail", pass, pair, err)
			}
		}
		dirBefore := bytes.Clone(dir)
		for _, ranges := range [][3]uint32{{0, 0, 0}, {65536, 0, 0}, {0, 0, 1}, {0, 1, 0}} {
			before := bytes.Clone(mem)
			binary.LittleEndian.PutUint64(args[16:], dirty(ranges[0]))
			binary.LittleEndian.PutUint64(args[24:], dirty(ranges[1]))
			binary.LittleEndian.PutUint64(args[32:], dirty(ranges[2]))
			for i := range results {
				results[i] = 0xa5
			}
			err := e.CallPrepared(entry+uintptr(image.functions[1].Entry), args, j.LinMemBase(), trap, results)
			valid := ranges[1] == 0 && ranges[2] == 0
			if valid {
				if err != nil {
					t.Fatal("active dropped zero-count init", ranges, err)
				}
			} else {
				var tr *coreruntime.TrapError
				if !errors.As(err, &tr) || tr.Code != coreruntime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint64(trap[16:]) != uint64(len(fixtures[1].locals)+8)<<32|2 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
					t.Fatal("active initialized segment remained usable", ranges, err)
				}
				clear(trap)
			}
			if !bytes.Equal(mem, before) || !bytes.Equal(dir, dirBefore) {
				t.Fatal("dropped active init changed memory/descriptor", ranges)
			}
		}
		if err := e.CallPrepared(entry+uintptr(image.functions[2].Entry), args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(dir, dirBefore) {
			t.Fatal("drop active already-dropped segment", err)
		}
		// A passive index immediately before the final active index remains
		// usable through the same whole-module function directory.
		binary.LittleEndian.PutUint64(args[16:], dirty(1024))
		binary.LittleEndian.PutUint64(args[24:], dirty(0))
		binary.LittleEndian.PutUint64(args[32:], dirty(3))
		if err := e.CallPrepared(entry+uintptr(image.functions[3].Entry), args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(mem[1024:1027], segments[254].Init) {
			t.Fatal("original passive index after active segments", err)
		}
		binary.LittleEndian.PutUint64(args[16:], dirty(0))
		if allocs := testing.AllocsPerRun(50, func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("active initialization vector invocation warm allocations", allocs)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		clear(mem)
		fresh, err := image.instantiateData(j)
		if err != nil || !bytes.Equal(mem[:16], segments[255].Init) || !bytes.Equal(mem[1024:1027], []byte{0, 0, 0}) {
			t.Fatal("reinstantiation reapplies source active segments only", err)
		}
		if err := fresh.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSWARRV64ActiveDataLateBoundsFailureDoesNotWriteEarlierSegments(t *testing.T) {
	e, j, args, results, trap := swarScalarMemoryExecutionFixture(t)
	load := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}
	good := swarActiveSegment(0, []byte{1, 2, 3, 4})
	for _, bad := range []wasm.Data{swarActiveSegment(65535, []byte{5, 6}), swarActiveSegment(65537, nil), swarActiveSegment(0xffffffff, nil)} {
		image, err := compileSWARModule(swarMixedDataSource([]fixtureFunc{load}, []wasm.Data{good, bad}), emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		mem := j.CurrentBytes()
		for i := range mem {
			mem[i] = 0xa5
		}
		before := bytes.Clone(mem)
		if owner, err := image.instantiateData(j); owner != nil || err == nil || !bytes.Equal(mem, before) || j.CaptureInstanceContext().PassiveDataPtr != 0 {
			t.Fatal("failed active instantiate wrote/bound", err)
		}
		// Failure did not poison the unrelated execution bridge or bindings.
		entry := thunkImageRV64(t, image.code)
		binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
		binary.LittleEndian.PutUint64(args[8:], 0x80000000deadbeef)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(args[:16], results[:16]) {
			t.Fatal("active initialization failure poisoned bridge reuse", err)
		}
	}
}
