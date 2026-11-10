//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestSWARRV64DirectCallsMixedCompactCellsAndLiveHomes(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	image, err := compileSWARModule(binaryModule(swarCallFixtures()...), emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
	for _, pair := range dirtyInputs() {
		input := [8]uint64{pair[0], 0x7ff0000000000001, pair[1], pair[1], pair[0], ^pair[1], ^pair[0], 0x8000000000000000}
		write := func() {
			for i, value := range input {
				binary.LittleEndian.PutUint64(args[i*8:], value)
			}
		}
		write()
		for i := range results {
			results[i] = 0xa5
		}
		// Input slots: I32, V128(2), F32, I64, V128(2), F64.
		want := [10]uint64{input[5], input[6], input[1] ^ input[5], input[2] ^ input[6], uint64(uint32(input[0])), input[7], input[5], input[6], uint64(uint32(input[3])), input[4]}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal("nested mixed call", pair, err)
		}
		for i, value := range want {
			if got := binary.LittleEndian.Uint64(results[i*8:]); got != value {
				t.Fatal("raw/narrow/live call cell", pair, i, got, value)
			}
		}
		if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-len(want)*8)) {
			t.Fatal("call result tail overwritten")
		}
		if allocs := testing.AllocsPerRun(50, func() {
			write() // Shared argument arenas are overwritten by nested calls.
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("nested call warm allocations", allocs)
		}
	}
}

func TestSWARRV64DirectCallsMaximumVectorArenas(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	params := bytes.Repeat([]byte{0x7b}, 128)
	caller := fixtureFunc{params: params, results: params}
	leaf := fixtureFunc{params: params, results: params}
	for i := 0; i < 128; i++ {
		caller.body = append(caller.body, 0x20, byte(i))
		leaf.body = append(leaf.body, 0x20, byte(127-i))
	}
	caller.body = append(caller.body, 0x10, 1, 0x0b)
	leaf.body = append(leaf.body, 0x0b)
	image, err := compileSWARModule(binaryModule(caller, leaf), emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
	for i := 0; i < 256; i++ {
		binary.LittleEndian.PutUint64(args[i*8:], 0xfff00000deadbeef^uint64(i)*0x101010101)
	}
	input := bytes.Clone(args)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if !bytes.Equal(results[i*16:i*16+16], input[(127-i)*16:(128-i)*16]) {
			t.Fatal("maximum compact vector call", i)
		}
	}
}

func TestSWARRV64DirectRecursiveCallsStackTrapAndReuse(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	fixture := swarRecursiveCallFixture()
	image, err := compileSWARModule(binaryModule(fixture), emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
	vector := []byte{1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0x80}
	for _, depth := range []uint32{0, 1, 2, 17, 31, 0xffffffff, 1} {
		copy(args, vector)
		binary.LittleEndian.PutUint64(args[16:], dirty(depth))
		for i := range results {
			results[i] = 0xa5
		}
		err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		if depth == 0xffffffff {
			var tr *coreruntime.TrapError
			// The first failed recursive entry reports its local-declaration PC
			// and function 0 before any result cell can be serialized.
			if !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(1)<<32|1 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("recursive stack bounds/source/prewrite", err, trap[16:24])
			}
			clear(trap)
			continue
		}
		if err != nil || !bytes.Equal(results[:16], vector) || !bytes.Equal(results[16:], bytes.Repeat([]byte{0xa5}, len(results)-16)) {
			t.Fatal("recursive dirty count/raw vector/reuse", depth, err, results[:16])
		}
	}
}

func TestSWARRV64DirectCalleeTrapSourceAndNoResult(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	for _, body := range [][]byte{
		{0, 0x0b},
		{0x41, 1, 0x41, 0, 0x6d, 0x1a, 0x20, 0, 0x0b},
	} {
		caller := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 1, 0x0b}}
		leaf := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: body}
		image, err := compileSWARModule(binaryModule(caller, leaf), emissionMode{production: true})
		if err != nil {
			t.Fatal(err)
		}
		entry := thunkImageRV64(t, image.code)
		for i := range results {
			results[i] = 0xa5
		}
		err = e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
		var tr *coreruntime.TrapError
		wantCode, wantPC := coreruntime.TrapUnreachable, uint32(1)
		if body[0] != 0 {
			wantCode, wantPC = coreruntime.TrapDivZero, 5
		}
		if !errors.As(err, &tr) || tr.Code != wantCode || binary.LittleEndian.Uint64(trap[16:]) != uint64(wantPC)<<32|2 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
			t.Fatal("callee trap/source/prewrite", err, wantCode, trap[16:24])
		}
		clear(trap)
	}
}

func TestSWARRV64DirectCalleeLoopActiveInterruptAndReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap := swarMemoryExecutionFixture(t)
	leaf := swarIndexedCases()[3].function
	marker := []byte{0x41, 0, 0x20, 0, 0xfd, 11, 0, 0}
	leaf.body = append(append(append([]byte(nil), leaf.body[:8]...), marker...), leaf.body[8:]...)
	caller := fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0x10, 1, 0x41, 16, 0x20, 0, 0xfd, 11, 0, 0, 0x0b}}
	data := modifySection(binaryModule(caller, leaf), 7, 5, []byte{1, 0, 1}, false)
	image, err := compileSWARModule(data, emissionMode{production: true, interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
	binary.LittleEndian.PutUint64(args, 1)
	binary.LittleEndian.PutUint64(args[8:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(args[16:], dirty(0xffffffff))
	vector := bytes.Clone(args[:16])
	for i := 16; i < 32; i++ {
		j.CurrentBytes()[i] = 0xa5
	}
	for i := range results {
		results[i] = 0xa5
	}
	done := make(chan error, 1)
	go func() { done <- e.CallPrepared(entry, args, j.LinMemBase(), trap, results) }()
	word := (*uint32)(unsafe.Pointer(&j.CurrentBytes()[0]))
	deadline, started := time.Now().Add(2*time.Second), false
	for time.Now().Before(deadline) {
		if atomic.LoadUint32(word) != 0 {
			started = true
			break
		}
		select {
		case err := <-done:
			t.Fatal("callee loop returned before interrupt", err)
		default:
		}
		time.Sleep(100 * time.Microsecond)
	}
	stop := coreruntime.RequestInterruptAsync(trap)
	defer stop()
	err = <-done // Join before any code, memory, trap, or call arena cleanup.
	stop()
	var tr *coreruntime.TrapError
	// Leaf local declarations occupy three bytes; its loop starts at body 4.
	if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || binary.LittleEndian.Uint64(trap[16:]) != uint64(7)<<32|2 || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) || !bytes.Equal(j.CurrentBytes()[16:32], bytes.Repeat([]byte{0xa5}, 16)) {
		t.Fatal("nested callee interruption/source/prewrite", started, err, trap[16:24])
	}
	clear(trap)
	copy(args, vector)
	binary.LittleEndian.PutUint64(args[16:], dirty(0))
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results, vector) || !bytes.Equal(j.CurrentBytes()[16:32], vector) {
		t.Fatal("callee reuse after interrupt", err, results)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		copy(args, vector)
		binary.LittleEndian.PutUint64(args[16:], dirty(0))
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("callee interruptible warm allocations", allocs)
	}
}

func TestSWARRV64DirectFloatCalleePreservesLiveRawCarriers(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	caller := fixtureFunc{params: []byte{0x7b, 0x7c, 0x7d}, results: []byte{0x7b, 0x7c, 0x7d}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}
	leaf := fixtureFunc{params: []byte{0x7d}, result: 0x7d, body: []byte{0x20, 0, 0x20, 0, 0x92, 0x0b}}
	image, err := compileSWARModule(binaryModule(caller, leaf), emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
	for _, bits := range []uint32{0, 0x80000000, 1, 0x80000001, 0x3fc00000, 0x7f7fffff, 0x7f800000, 0xff800000, 0x7f800001, 0xffc12345} {
		input := [4]uint64{0x7ff0000000000001, 0x80000000deadbeef, 0xfff0000000000001, dirty(bits)}
		write := func() {
			for i, value := range input {
				binary.LittleEndian.PutUint64(args[i*8:], value)
			}
		}
		write()
		for i := range results {
			results[i] = 0xa5
		}
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if binary.LittleEndian.Uint64(results[i*8:]) != input[i] {
				t.Fatal("FP callee clobbered live raw vector/F64", bits, i)
			}
		}
		value := math.Float32frombits(bits)
		want := math.Float32bits(value + value)
		got := binary.LittleEndian.Uint64(results[24:])
		if math.IsNaN(float64(value)) {
			if got>>32 != 0 || uint32(got)&0x7fc00000 != 0x7fc00000 {
				t.Fatal("FP callee quiet NaN/narrow normalization", bits, got)
			}
		} else if got != uint64(want) {
			t.Fatal("FP callee RNE result/narrow normalization", bits, got, want)
		}
		if !bytes.Equal(results[32:], bytes.Repeat([]byte{0xa5}, len(results)-32)) {
			t.Fatal("FP call result tail")
		}
		if allocs := testing.AllocsPerRun(50, func() {
			write()
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}); allocs != 0 {
			t.Fatal("FP callee warm allocations", allocs)
		}
	}
}

func TestSWARRV64DirectCalleeFrameFenceBeforeWrites(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
	caller := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x10, 1, 0x0b}}
	leaf := fixtureFunc{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 127, 0x7b}, body: []byte{0x20, 0, 0x0b}}
	image, err := compileSWARModule(binaryModule(caller, leaf), emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, image.code)
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
	calleeSP := bridgeSP - uintptr(image.functions[0].FrameBytes)
	frameStart := calleeSP - uintptr(image.functions[1].FrameBytes)
	if frameStart < e.StackLimit() || frameStart >= calleeSP || calleeSP > e.StackTop() {
		t.Fatal("sentinel region outside engine's mmap stack")
	}
	// This known mmap address is off the Go heap. Use the runtime's established
	// pointer-sized-local conversion rather than a moving Go pointer roundtrip.
	framePointer := *(*unsafe.Pointer)(unsafe.Pointer(&frameStart))
	frame := unsafe.Slice((*byte)(framePointer), image.functions[1].FrameBytes)
	for i := range frame {
		frame[i] = 0x6b
	}
	before := bytes.Clone(frame)
	// The caller fits; the callee has sixteen fewer bytes than its full frame.
	j.SetStackFence(frameStart + 16)
	for i := range results {
		results[i] = 0xa5
	}
	var tr *coreruntime.TrapError
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); !errors.As(err, &tr) || tr.Code != coreruntime.TrapStackFenceBreached || binary.LittleEndian.Uint64(trap[16:]) != uint64(3)<<32|2 || !bytes.Equal(frame, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
		t.Fatal("callee full-frame guard wrote before fence", err, trap[16:24])
	}
	j.SetStackFence(e.StackLimit())
	clear(trap)
	if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
		t.Fatal("fenced callee reuse", err)
	}
}
