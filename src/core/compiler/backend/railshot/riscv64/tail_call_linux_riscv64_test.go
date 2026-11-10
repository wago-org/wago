//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// These probes are test-only native prefixes. They record actual entry SP in a
// retained offheap cell, never change SP/RA/transport, and do not write guest
// memory or promise public profiling semantics. All relative intra-function
// branches remain unchanged; independently decoded same-image call/tail sites
// are rebased to the prefixed framed entries before publication.
func localTailSPProbe(pointer uintptr) []byte {
	var a enc.Asm
	a.Constant64(enc.X28, int64(pointer))
	a.Ld(enc.X29, enc.X28, 0)
	a.Branch(enc.GEU, enc.SP, enc.X29, 8)
	a.Sd(enc.SP, enc.X28, 0)
	a.Ld(enc.X29, enc.X28, 8)
	a.Branch(enc.GEU, enc.X29, enc.SP, 8)
	a.Sd(enc.SP, enc.X28, 8)
	for a.Len()%16 != 0 {
		a.Addi(enc.Zero, enc.Zero, 0)
	}
	if a.Err() != nil {
		panic(a.Err())
	}
	return a.B
}

func localTailNativeCode(t *testing.T, m *wasm.Module, vector bool) ([]byte, []FunctionInfo) {
	t.Helper()
	opts := localTailOptions()
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	var code []byte
	var info []FunctionInfo
	if vector {
		c, err := compileSWARDecodedModuleProfile(m, emissionMode{production: true, interruptible: true, localTailCalls: true}, swarModulePlain, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, indexedMemories: true})
		if err != nil {
			t.Fatal(err)
		}
		code, info = c.code, c.functions
	} else {
		opts.production, opts.publicStarts = true, true
		c, err := compileDecodedModule(m, scalarRegisters[:], opts)
		if err != nil {
			t.Fatal(err)
		}
		code, info = c.code, c.functions
	}
	if !bytes.Equal(image.Code, code) || len(image.Entry) != len(info) {
		t.Fatal("probe/source staging mismatch")
	}
	return code, info
}

func localTailInstrument(t *testing.T, code []byte, infos []FunctionInfo, probe []byte) ([]byte, []int) {
	t.Helper()
	entries := make([]int, len(infos))
	oldEntries := make(map[int64]int, len(infos))
	var out []byte
	for i, info := range infos {
		for len(out)%16 != 0 {
			out = append(out, 0x13, 0, 0, 0)
		}
		entries[i] = len(out)
		oldEntries[int64(info.Entry)] = i
		out = append(out, probe...)
		out = append(out, code[info.Entry:info.Entry+info.CodeBytes]...)
	}
	a := enc.Asm{B: out}
	for i, info := range infos {
		for off := info.Entry; off+8 <= info.Entry+info.CodeBytes; off += 4 {
			hi := binary.LittleEndian.Uint32(code[off:])
			lo := binary.LittleEndian.Uint32(code[off+4:])
			link := (lo >> 7) & 31
			if hi&0x7f != 0x17 || (hi>>7)&31 != 31 || lo&0x7f != 0x67 || (lo>>15)&31 != 31 || (link != 0 && link != 1) {
				continue
			}
			dest := int64(off) + int64(int32(hi&0xfffff000)) + int64(int32(lo)>>20)
			j, ok := oldEntries[dest]
			if !ok {
				continue
			}
			site := entries[i] + len(probe) + off - info.Entry
			if err := a.PatchLong(site, int64(entries[j])); err != nil {
				t.Fatal(err)
			}
		}
	}
	return a.B, entries
}

func localTailCounterFunction(target byte, extraLocals int) localTailFixture {
	// Base return is an explicit multivalue return. The tail discards the
	// previous operand prefix and forwards raw FP/integer cells to the target.
	return localTailFixture{
		params:  []wasm.ValType{wasm.I32, wasm.F64, wasm.F32, wasm.I64},
		results: []wasm.ValType{wasm.F64, wasm.F32, wasm.I64, wasm.I32},
		locals:  extraLocals,
		body:    []byte{0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 0, 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, 0x20, 3, 0x12, target, 0x0b},
	}
}

func TestLocalTailRV64DeepSelfMutualConstantSPAndRawTransport(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, mutual := range []bool{false, true} {
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			funcs := []localTailFixture{localTailCounterFunction(0, 0)}
			if mutual {
				funcs = []localTailFixture{localTailCounterFunction(1, 0), localTailCounterFunction(0, 60)}
			}
			code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(vector, funcs...)), vector)
			arena, err := coreruntime.NewArena(16)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := arena.Close(); err != nil {
					t.Error(err)
				}
			})
			witness := arena.Alloc(16)
			code, entries := localTailInstrument(t, code, info, localTailSPProbe(thunkPointerRV64(witness)))
			entry := thunkImageRV64(t, code) + uintptr(entries[0])
			var observed uint64
			invoke := func(depth uint32) {
				t.Helper()
				binary.LittleEndian.PutUint64(witness, ^uint64(0))
				binary.LittleEndian.PutUint64(witness[8:], 0)
				binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(depth))
				binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(args[16:], 0xdeadbeef80000000)
				binary.LittleEndian.PutUint64(args[24:], 0xfedcba9876543210)
				for i := range results {
					results[i] = 0xa5
				}
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					t.Fatal(vector, mutual, depth, err)
				}
				for i, want := range []uint64{0x7ff0000000000001, 0x80000000, 0xfedcba9876543210, 0} {
					if got := binary.LittleEndian.Uint64(results[i*8:]); got != want {
						t.Fatal("raw result", i, got, want)
					}
				}
				if !bytes.Equal(results[32:], bytes.Repeat([]byte{0xa5}, len(results)-32)) {
					t.Fatal("tail result overwrite")
				}
				lo, hi := binary.LittleEndian.Uint64(witness), binary.LittleEndian.Uint64(witness[8:])
				if lo == 0 || lo == ^uint64(0) || lo != hi {
					t.Fatal("tail grew native stack", depth, lo, hi)
				}
				if observed != 0 && lo != observed {
					t.Fatal("depth-dependent SP", depth, observed, lo)
				}
				observed = lo
			}
			for _, depth := range []uint32{0, 1, 33, 50000, 2} {
				invoke(depth)
			}
			// Reconstruct root arguments because the shared arena is intentionally
			// overwritten by every tail hop. No per-hop Go allocation is permitted.
			if n := testing.AllocsPerRun(25, func() {
				binary.LittleEndian.PutUint64(args, 41)
				binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(args[16:], 0xdeadbeef80000000)
				binary.LittleEndian.PutUint64(args[24:], 0xfedcba9876543210)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("warm tail allocations", n)
			}
		}
	}
}

func TestLocalTailRV64RestoresOriginalRAAfterOrdinaryCall(t *testing.T) {
	for _, vector := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		funcs := []localTailFixture{
			{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 0, 0x10, 1, 0x20, 0, 0x6a, 0x0b}},
			// The ordinary call overwrites live RA; the subsequent proper tail
			// must reload this function's original RA from header0.
			{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x10, 2, 0x1a, 0x20, 0, 0x12, 3, 0x0b}},
			{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 9, 0x0b}},
			{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}},
		}
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(vector, funcs...)), vector)
		entry := thunkImageRV64(t, code) + uintptr(info[0].Entry)
		binary.LittleEndian.PutUint64(args, 0xdeadbeef00000029)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 83 {
			t.Fatal("original continuation/local preservation", vector, err)
		}
	}
}

func TestLocalTailRV64FullArityAndNarrowEntryToWideInternal(t *testing.T) {
	for _, vector := range []bool{false, true} {
		arity := 256
		if vector {
			arity = 128
		}
		params := make([]wasm.ValType, arity)
		prefix, tail, leaf := []byte{}, []byte{}, []byte{}
		want := make([]uint64, arity)
		for i := range params {
			params[i] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[i%4]
			switch i % 4 {
			case 0:
				prefix = append(prefix, 0x41)
				prefix = append(prefix, wasmtest.SLEB32(int32(0x80000000+uint32(i)))...)
				want[i] = 0x80000000 + uint64(i)
			case 1:
				prefix = append(prefix, 0x42)
				prefix = append(prefix, wasmtest.SLEB64(-int64(i)-1)...)
				want[i] = uint64(-int64(i) - 1)
			case 2:
				prefix = append(prefix, 0x43)
				var raw [4]byte
				binary.LittleEndian.PutUint32(raw[:], 0x7f800001+uint32(i))
				prefix = append(prefix, raw[:]...)
				want[i] = 0x7f800001 + uint64(i)
			case 3:
				prefix = append(prefix, 0x44)
				var raw [8]byte
				binary.LittleEndian.PutUint64(raw[:], 0x7ff0000000000001+uint64(i))
				prefix = append(prefix, raw[:]...)
				want[i] = 0x7ff0000000000001 + uint64(i)
			}
			tail = append(tail, 0x20)
			tail = append(tail, wasmtest.ULEB(uint32(i))...)
			leaf = append(leaf, 0x20)
			leaf = append(leaf, wasmtest.ULEB(uint32(i))...)
		}
		prefix = append(prefix, 0x12, 1, 0x0b)
		tail = append(tail, 0x12, 2, 0x0b)
		leaf = append(leaf, 0x0b)
		funcs := []localTailFixture{{results: params, body: prefix}, {params: params, results: params, body: tail}, {params: params, results: params, body: leaf}}
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(vector, funcs...)), vector)
		entry := thunkImageRV64(t, code) + uintptr(info[0].Entry)
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		for i, v := range want {
			if got := binary.LittleEndian.Uint64(results[i*8:]); got != v {
				t.Fatal("full arity", vector, i, got, v)
			}
		}
		if !bytes.Equal(results[8*arity:], bytes.Repeat([]byte{0xa5}, len(results)-8*arity)) {
			t.Fatal("wide return tail")
		}
	}
}

func TestLocalTailRV64TargetTrapAndLargerFramePrewrite(t *testing.T) {
	for _, vector := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		funcs := []localTailFixture{{results: []wasm.ValType{wasm.I32}, body: []byte{0x12, 1, 0x0b}}, {results: []wasm.ValType{wasm.I32}, locals: 64, body: []byte{0x41, 41, 0x0b}}}
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(vector, funcs...)), vector)
		ar, err := coreruntime.NewArena(16)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := ar.Close(); err != nil {
				t.Error(err)
			}
		})
		witness := ar.Alloc(16)
		binary.LittleEndian.PutUint64(witness, ^uint64(0))
		code, entries := localTailInstrument(t, code, info, localTailSPProbe(thunkPointerRV64(witness)))
		base := thunkImageRV64(t, code)
		if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		sp := uintptr(binary.LittleEndian.Uint64(witness))
		if binary.LittleEndian.Uint64(witness[8:]) != uint64(sp) || info[1].FrameBytes <= info[0].FrameBytes {
			t.Fatal("probe/frame fixture")
		}
		// This entire owned stack range is below the smaller caller's writes.
		low := sp - uintptr(info[1].FrameBytes)
		n := info[1].FrameBytes - info[0].FrameBytes
		if low < e.StackLimit() || low+uintptr(n) > e.StackTop() || n <= 0 {
			t.Fatal("sentinel outside retained engine stack")
		}
		// Match the repository's explicit owned-mmap pointer conversion. No Go
		// pointer or arbitrary guest address is synthesized by this probe.
		pointer := *(*unsafe.Pointer)(unsafe.Pointer(&low))
		below := unsafe.Slice((*byte)(pointer), n)
		for i := range below {
			below[i] = 0xa5
		}
		j.SetStackFence(low + 16)
		for i := range results {
			results[i] = 0xa5
		}
		err = e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results)
		var te *coreruntime.TrapError
		if !errors.As(err, &te) || te.Code != coreruntime.TrapStackFenceBreached || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != uint32(localTailDecode(t, localTailSource(vector, funcs...)).Code[1].LocalDeclBytes) {
			t.Fatal("target guard provenance", vector, err)
		}
		if !bytes.Equal(below, bytes.Repeat([]byte{0xa5}, n)) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("tail target wrote before full frame guard")
		}
		j.SetStackFence(e.StackLimit())
		clear(trap)
		if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 41 {
			t.Fatal("guard reuse", err)
		}
		// A target body trap reports the target, never a fabricated return site.
		funcs[1].body = []byte{0x00, 0x0b}
		code, info = localTailNativeCode(t, localTailDecode(t, localTailSource(vector, funcs...)), vector)
		entry := thunkImageRV64(t, code) + uintptr(info[0].Entry)
		for i := range results {
			results[i] = 0xa5
		}
		err = e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		if !errors.As(err, &te) || te.Code != coreruntime.TrapUnreachable || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != 3 {
			t.Fatal("target body source", err)
		}
		if !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("cold tail published results")
		}
	}
}

func TestLocalTailRV64ActiveCycleInterruptAndReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	for _, vector := range []bool{false, true} {
		e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
		j, err := coreruntime.NewJobMemory(65536)
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
		funcs := []localTailFixture{
			{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x12, 0, 0x0b}},
			{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 41, 0x0b}},
		}
		m, decodeErr := wasm.DecodeModule(localTailSource(vector, funcs...))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		code, info := localTailNativeCode(t, m, vector)
		base := thunkImageRV64(t, code)
		for i := range results {
			results[i] = 0xa5
		}
		done := make(chan error, 1)
		go func() { done <- e.CallPrepared(base+uintptr(info[0].Entry), args, j.LinMemBase(), trap, results) }()
		word := (*uint32)(unsafe.Pointer(&j.CurrentBytes()[0]))
		deadline := time.Now().Add(2 * time.Second)
		started := false
		for time.Now().Before(deadline) {
			if atomic.LoadUint32(word) != 0 {
				started = true
				break
			}
			select {
			case err := <-done:
				t.Fatal("tail returned before interrupt", err)
			default:
			}
			time.Sleep(100 * time.Microsecond)
		}
		stop := coreruntime.RequestInterruptAsync(trap)
		err = <-done
		stop() // Always join the native activation before cleanup/clear.
		var te *coreruntime.TrapError
		if !started || !errors.As(err, &te) || te.Code != coreruntime.TrapInterrupted || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].ProgramCounter != 1 {
			t.Fatal("active tail entry poll", vector, err)
		}
		if !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("interrupted tail committed result")
		}
		clear(trap)
		if err := e.CallPrepared(base+uintptr(info[1].Entry), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 41 {
			t.Fatal("cycle reuse", err)
		}
	}
}

func TestLocalTailRV64ZeroArityVoidReturn(t *testing.T) {
	for _, vector := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		funcs := []localTailFixture{{body: []byte{0x12, 1, 0x0b}}, {body: []byte{0x0b}}}
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(vector, funcs...)), vector)
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(thunkImageRV64(t, code)+uintptr(info[0].Entry), args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("void tail wrote result arena")
		}
	}
}

func TestLocalTailRV64InsideRetainedForeignBridgeUsesFullArena(t *testing.T) {
	// This is an ordinary foreign activation containing a proper local tail;
	// it does not claim that an imported tail releases/reuses a foreign record.
	for _, shared := range []bool{false, true} {
		f := newNativeImportFixtureRV64(t, shared)
		params := make([]wasm.ValType, 256)
		body := []byte{}
		for i := range params {
			params[i] = wasm.I64
			body = append(body, 0x42)
			body = append(body, wasmtest.SLEB64(-int64(i)-1)...)
		}
		body = append(body, 0x12, 1, 0x0b)
		funcs := []localTailFixture{{results: []wasm.ValType{wasm.I32}, body: body}, {params: params, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 0xff, 1, 0x1a, 0x41, 17, 0x0b}}}
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(false, funcs...)), false)
		target := thunkImageRV64(t, code) + uintptr(info[0].Entry)
		entry := f.thunk(t, target, 0, 1)
		for i := range f.args {
			f.args[i] = 0x5a
		}
		for i := range f.results {
			f.results[i] = 0xa5
		}
		before := bytes.Clone(f.args)
		if err := f.e.CallPrepared(entry, nil, f.caller.LinMemBase(), f.trap, f.results[:8]); err != nil {
			t.Fatal(shared, err)
		}
		if binary.LittleEndian.Uint64(f.results) != 17 || !bytes.Equal(f.args, before) || !bytes.Equal(f.results[8:], bytes.Repeat([]byte{0xa5}, len(f.results)-8)) {
			t.Fatal("foreign original result transport")
		}
		if binary.LittleEndian.Uint64(f.targetArguments[255*8:]) != ^uint64(255) {
			t.Fatal("tail failed to use complete retained arena")
		}
		f.assertCaller(t)
	}
}
