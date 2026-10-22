//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"bytes"
	"encoding/binary"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestRV64InvocationViewActualEntryBuffersAndReservation(t *testing.T) {
	e, j, arena := productionFixtureRV64(t, linMemBytes)
	physicalTop := e.StackTop()
	pointer := e.RV64InvocationViewPointer()
	if pointer != physicalTop-abi.RV64InvocationViewBytes || e.StackTop() != physicalTop {
		t.Fatal("view reservation changed physical extent")
	}
	var a enc.Asm
	a.Ld(enc.X5, enc.X10, 0)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ret()
	entry := productionImageRV64(t, &a)
	trap := arena.Alloc(TrapBufferBytes)
	if err := j.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	pairs := [][2][]byte{{arena.Alloc(16), arena.Alloc(24)}, {arena.Alloc(8), arena.Alloc(40)}}
	for n, pair := range pairs {
		binary.LittleEndian.PutUint64(pair[0], uint64(49+n))
		var err error
		if n == 0 {
			err = e.Call(entry, pair[0], j.LinearMemory(), trap, pair[1])
		} else {
			err = e.CallPrepared(entry, pair[0], j.LinMemBase(), trap, pair[1])
		}
		if err != nil || binary.LittleEndian.Uint64(pair[1]) != uint64(49+n) {
			t.Fatal("entry result", err)
		}
		view := e.stack[len(e.stack)-abi.RV64InvocationViewBytes:]
		want := []uint64{abi.RV64InvocationMagic, uint64(e.StackLimit()), uint64(pointer), uint64(slicePtr(pair[0])), uint64(len(pair[0])), uint64(slicePtr(pair[1])), uint64(len(pair[1])), 0}
		for word, value := range want {
			if binary.LittleEndian.Uint64(view[word*8:]) != value {
				t.Fatalf("entry%d word%d", n, word)
			}
		}
	}
	var empty enc.Asm
	empty.Ret()
	if err := e.CallPreparedBounded(productionImageRV64(t, &empty), nil, j.LinMemBase(), trap, nil); err != nil {
		t.Fatal(err)
	}
	view := e.stack[len(e.stack)-abi.RV64InvocationViewBytes:]
	for _, offset := range []int{24, 32, 40, 48} {
		if binary.LittleEndian.Uint64(view[offset:]) != 0 {
			t.Fatal("zero direction retained an old buffer")
		}
	}
	before := bytes.Clone(view)
	if err := e.CallPrepared(entry, pairs[0][0], j.LinMemBase(), trap[:2], pairs[0][1]); err == nil {
		t.Fatal("short trap admitted")
	}
	if !bytes.Equal(view, before) {
		t.Fatal("rejected entry published new invocation")
	}
}

func TestRV64InvocationViewClearedBeforeEngineReuse(t *testing.T) {
	e, err := AcquireEngineWithStackBytes(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	pointer := e.RV64InvocationViewPointer()
	e.prepareNativeInvocation([]byte{1}, []byte{2})
	if err := ReleaseEngine(e); err != nil {
		t.Fatal(err)
	}
	reused, err := AcquireEngineWithStackBytes(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer reused.Close()
	if reused != e || reused.stackTop != reused.StackTop() || pointer != reused.StackTop()-abi.RV64InvocationViewBytes {
		t.Fatal("cache did not restore idle entry layout")
	}
	for _, value := range reused.stack[len(reused.stack)-abi.RV64InvocationViewBytes:] {
		if value != 0 {
			t.Fatal("pooled Engine retained invocation pointers")
		}
	}
}
