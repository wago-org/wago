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
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func vectorTailCounter(target byte, locals int) localTailFixture {
	return localTailFixture{
		params:  []wasm.ValType{wasm.I32, wasm.V128, wasm.F32},
		results: []wasm.ValType{wasm.V128, wasm.F32, wasm.I32}, locals: locals,
		body: []byte{0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, 0x20, 2, 0x20, 0, 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, 0x12, target, 0x0b},
	}
}

func TestVectorLocalTailRV64DeepMixedSelfMutualMeasuredSP(t *testing.T) {
	for _, mutual := range []bool{false, true} {
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		funcs := []localTailFixture{vectorTailCounter(0, 0)}
		if mutual {
			funcs = []localTailFixture{vectorTailCounter(1, 0), vectorTailCounter(0, 60)}
		}
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
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
		code, entries := localTailInstrument(t, code, info, localTailSPProbe(thunkPointerRV64(witness)))
		entry := thunkImageRV64(t, code) + uintptr(entries[0])
		var observed uint64
		write := func(depth uint32) {
			binary.LittleEndian.PutUint64(args, 0xfeedface00000000|uint64(depth))
			binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
			binary.LittleEndian.PutUint64(args[16:], 0x8000000000000000)
			binary.LittleEndian.PutUint64(args[24:], 0xdeadbeef7f800001)
		}
		for _, depth := range []uint32{0, 1, 31, 50000, 2} {
			write(depth)
			binary.LittleEndian.PutUint64(witness, ^uint64(0))
			binary.LittleEndian.PutUint64(witness[8:], 0)
			for i := range results {
				results[i] = 0xa5
			}
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				t.Fatal(mutual, depth, err)
			}
			for i, want := range []uint64{0x7ff0000000000001, 0x8000000000000000, 0x7f800001, 0} {
				if binary.LittleEndian.Uint64(results[i*8:]) != want {
					t.Fatal("mixed raw tail result", mutual, depth, i)
				}
			}
			if !bytes.Equal(results[32:], bytes.Repeat([]byte{0xa5}, len(results)-32)) {
				t.Fatal("result tail")
			}
			lo, hi := binary.LittleEndian.Uint64(witness), binary.LittleEndian.Uint64(witness[8:])
			if lo == 0 || lo == ^uint64(0) || lo != hi || observed != 0 && lo != observed {
				t.Fatal("vector tail grew native stack", depth, lo, hi, observed)
			}
			observed = lo
		}
		if n := testing.AllocsPerRun(25, func() {
			write(41)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); n != 0 {
			t.Fatal("warm vector tail allocation", n)
		}
	}
}

func TestVectorLocalTailRV64Mixed128CompactPackingBothDirections(t *testing.T) {
	params := make([]wasm.ValType, 128)
	targetParams := make([]wasm.ValType, 128)
	for i := range params {
		params[i] = []wasm.ValType{wasm.I32, wasm.V128, wasm.F32, wasm.I64, wasm.F64}[i%5]
	}
	root, leaf := []byte{}, []byte{}
	for i := 127; i >= 0; i-- {
		targetParams[127-i] = params[i]
		root = append(root, 0x20)
		root = append(root, wasmtest.ULEB(uint32(i))...)
		leaf = append(leaf, 0x20)
		leaf = append(leaf, wasmtest.ULEB(uint32(i))...)
	}
	root = append(root, 0x12, 1, 0x0b)
	leaf = append(leaf, 0x0b)
	funcs := []localTailFixture{{params: params, results: params, body: root}, {params: targetParams, results: params, body: leaf}}
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
	entry := thunkImageRV64(t, code) + uintptr(info[0].Entry)
	var input, want []uint64
	for i, typ := range params {
		v := uint64(0xfedcba9876543210) ^ uint64(i)
		switch i % 5 {
		case 0:
			v = 0xfeedface80000000 + uint64(i)
		case 2:
			v = 0xdeadbeef7f800001 + uint64(i)
		case 4:
			v = 0x7ff0000000000001 + uint64(i)
		}
		input = append(input, v)
		if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.F32) {
			want = append(want, uint64(uint32(v)))
		} else {
			want = append(want, v)
		}
		if wasm.EqualValType(typ, wasm.V128) {
			input = append(input, 0x8000000000000000^uint64(i))
			want = append(want, 0x8000000000000000^uint64(i))
		}
	}
	// Compute the compact target argument order independently of the emitted
	// marshaller: vectors retain low/high order while logical values reverse.
	var targetWant []uint64
	ends := make([]int, 128)
	at := 0
	for i, typ := range params {
		ends[i] = at
		at++
		if wasm.EqualValType(typ, wasm.V128) {
			at++
		}
	}
	for i := 127; i >= 0; i-- {
		targetWant = append(targetWant, want[ends[i]])
		if wasm.EqualValType(params[i], wasm.V128) {
			targetWant = append(targetWant, want[ends[i]+1])
		}
	}
	write := func() {
		for i := range args {
			args[i] = 0x5a
		}
		for i, v := range input {
			binary.LittleEndian.PutUint64(args[i*8:], v)
		}
	}
	write()
	for i := range results {
		results[i] = 0xa5
	}
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal(err)
	}
	for i, v := range want {
		if binary.LittleEndian.Uint64(results[i*8:]) != v {
			t.Fatal("mixed compact return", i)
		}
	}
	for i, v := range targetWant {
		if binary.LittleEndian.Uint64(args[i*8:]) != v {
			t.Fatal("mixed compact argument", i)
		}
	}
	if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) || !bytes.Equal(args[len(input)*8:], bytes.Repeat([]byte{0x5a}, len(args)-len(input)*8)) {
		t.Fatal("mixed compact arena tail")
	}
	if n := testing.AllocsPerRun(25, func() {
		write()
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("mixed warm allocation", n)
	}
}

func TestVectorLocalTailRV64Full128PairsFromParameterlessEntry(t *testing.T) {
	v := make([]wasm.ValType, 128)
	root, middle, leaf := []byte{}, []byte{}, []byte{}
	for i := range v {
		v[i] = wasm.V128
		root = append(root, swarLiteral(0x7ff0000000000001+uint64(i), 0x8000000000000000^uint64(i))...)
		middle = append(middle, 0x20)
		middle = append(middle, wasmtest.ULEB(uint32(i))...)
		leaf = append(leaf, 0x20)
		leaf = append(leaf, wasmtest.ULEB(uint32(127-i))...)
	}
	root = append(root, 0x12, 1, 0x0b)
	middle = append(middle, 0x12, 2, 0x0b)
	leaf = append(leaf, 0x0b)
	funcs := []localTailFixture{{results: v, body: root}, {params: v, results: v, body: middle}, {params: v, results: v, body: leaf}}
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
	entry := thunkImageRV64(t, code) + uintptr(info[0].Entry)
	for i := range results {
		results[i] = 0xa5
	}
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if binary.LittleEndian.Uint64(results[16*i:]) != 0x7ff0000000000001+uint64(127-i) || binary.LittleEndian.Uint64(results[16*i+8:]) != 0x8000000000000000^uint64(127-i) {
			t.Fatal("full raw pair", i)
		}
	}
	if binary.LittleEndian.Uint64(args[255*8:]) != 0x8000000000000000^127 {
		t.Fatal("highest input cell not reached")
	}
	if n := testing.AllocsPerRun(25, func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("full pair warm allocation", n)
	}
}

func TestVectorLocalTailRV64LivePrefixOriginalRAAndResults(t *testing.T) {
	p, r := []wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128, wasm.I32}
	mask := swarLiteral(0x8000000000000000, 0x7ff0000000000001)
	leaf := append([]byte{0x20, 0}, mask...)
	leaf = append(leaf, 0xfd, 81, 0x20, 1, 0x0b)
	funcs := []localTailFixture{
		{params: p, results: r, body: []byte{0x20, 0, 0x20, 0, 0x20, 1, 0x10, 1, 0x21, 1, 0xfd, 81, 0x20, 1, 0x0b}},
		{params: p, results: r, body: []byte{0x10, 2, 0x1a, 0x20, 0, 0x20, 1, 0x12, 3, 0x0b}},
		{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 9, 0x0b}},
		{params: p, results: r, body: leaf},
	}
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
	binary.LittleEndian.PutUint64(args, 0x123456789abcdef0)
	binary.LittleEndian.PutUint64(args[8:], 0xfedcba9876543210)
	binary.LittleEndian.PutUint64(args[16:], 0xdeadbeef80000001)
	for i := range results {
		results[i] = 0xa5
	}
	if err := e.CallPrepared(thunkImageRV64(t, code)+uintptr(info[0].Entry), args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{0x8000000000000000, 0x7ff0000000000001, 0x80000001} {
		if binary.LittleEndian.Uint64(results[i*8:]) != want {
			t.Fatal("ordinary ancestor/RA/raw result", i)
		}
	}
	if !bytes.Equal(results[24:], bytes.Repeat([]byte{0xa5}, len(results)-24)) {
		t.Fatal("ordinary ancestor result tail")
	}
}

func TestVectorLocalTailRV64InsideNarrowRetainedForeignBridge(t *testing.T) {
	for _, shared := range []bool{false, true} {
		f := newNativeImportFixtureRV64(t, shared)
		v := make([]wasm.ValType, 128)
		body := []byte{}
		for i := range v {
			v[i] = wasm.V128
			body = append(body, swarLiteral(uint64(i), ^uint64(i))...)
		}
		body = append(body, 0x12, 1, 0x0b)
		funcs := []localTailFixture{{results: []wasm.ValType{wasm.I32}, body: body}, {params: v, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 127, 0x1a, 0x41, 17, 0x0b}}}
		code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
		entry := f.thunk(t, thunkImageRV64(t, code)+uintptr(info[0].Entry), 0, 1)
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
			t.Fatal("foreign surrounding transport")
		}
		if binary.LittleEndian.Uint64(f.targetArguments[254*8:]) != 127 || binary.LittleEndian.Uint64(f.targetArguments[255*8:]) != ^uint64(127) {
			t.Fatal("foreign full pair arena")
		}
		f.assertCaller(t)
	}
}

func TestVectorLocalTailRV64LargerTargetGuardAndRawResultReuse(t *testing.T) {
	v := []wasm.ValType{wasm.V128}
	funcs := []localTailFixture{{params: v, results: v, body: []byte{0x20, 0, 0x12, 1, 0x0b}}, {params: v, results: v, locals: 64, body: []byte{0x20, 0, 0x0b}}}
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	code, info := localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
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
	write := func() {
		binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
		binary.LittleEndian.PutUint64(args[8:], 0x8000000000000000)
	}
	write()
	if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal(err)
	}
	sp := uintptr(binary.LittleEndian.Uint64(witness))
	low := sp - uintptr(info[1].FrameBytes)
	n := info[1].FrameBytes - info[0].FrameBytes
	if n <= 0 || low < e.StackLimit() || low+uintptr(n) > e.StackTop() {
		t.Fatal("sentinel outside retained stack")
	}
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&low))
	below := unsafe.Slice((*byte)(pointer), n)
	for i := range below {
		below[i] = 0xa5
	}
	for i := range results {
		results[i] = 0xa5
	}
	j.SetStackFence(low + 16)
	write()
	err = e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results)
	var te *coreruntime.TrapError
	if !errors.As(err, &te) || te.Code != coreruntime.TrapStackFenceBreached || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != 3 {
		t.Fatal("vector target entry guard", err)
	}
	if !bytes.Equal(below, bytes.Repeat([]byte{0xa5}, n)) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("vector target prewrite")
	}
	clear(trap)
	j.SetStackFence(e.StackLimit())
	write()
	if err := e.CallPrepared(base+uintptr(entries[0]), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 0x7ff0000000000001 || binary.LittleEndian.Uint64(results[8:]) != 0x8000000000000000 {
		t.Fatal("vector guard reuse", err)
	}
	funcs[1].body = []byte{0x00, 0x0b}
	code, info = localTailNativeCode(t, localTailDecode(t, localTailSource(true, funcs...)), true)
	write()
	for i := range results {
		results[i] = 0xa5
	}
	err = e.CallPrepared(thunkImageRV64(t, code)+uintptr(info[0].Entry), args, j.LinMemBase(), trap, results)
	if !errors.As(err, &te) || te.Code != coreruntime.TrapUnreachable || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 1 || te.Frames[0].ProgramCounter != 3 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("vector body cold source/result", err)
	}
}

func TestVectorLocalTailRV64ActiveCycleInterruptAndReuse(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
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
	v := []wasm.ValType{wasm.V128}
	funcs := []localTailFixture{{params: v, results: v, body: []byte{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x20, 0, 0x12, 0, 0x0b}}, {params: v, results: v, body: []byte{0x20, 0, 0x0b}}}
	m, err := wasm.DecodeModule(localTailSource(true, funcs...))
	if err != nil {
		t.Fatal(err)
	}
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	code, info := localTailNativeCode(t, m, true)
	base := thunkImageRV64(t, code)
	write := func() {
		binary.LittleEndian.PutUint64(args, 0x7ff0000000000001)
		binary.LittleEndian.PutUint64(args[8:], 0x8000000000000000)
	}
	write()
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
			t.Fatal("vector cycle returned", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(trap)
	err = <-done
	stop()
	var te *coreruntime.TrapError
	if !started || !errors.As(err, &te) || te.Code != coreruntime.TrapInterrupted || len(te.Frames) != 1 || te.Frames[0].FunctionIndex != 0 || te.Frames[0].ProgramCounter != 1 {
		t.Fatal("active vector tail entry poll", err)
	}
	if !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("vector interrupt committed result")
	}
	clear(trap)
	write()
	if err := e.CallPrepared(base+uintptr(info[1].Entry), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 0x7ff0000000000001 || binary.LittleEndian.Uint64(results[8:]) != 0x8000000000000000 {
		t.Fatal("vector interrupt reuse", err)
	}
}
