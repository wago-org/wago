//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestSerializedFrameCapacityReuseAndCleanup(t *testing.T) {
	for _, n := range []int{-1, 257} {
		if _, err := NewCallFrame(n); err == nil {
			t.Fatal("invalid capacity", n)
		}
	}
	if _, err := new(CallFrame).Call(nil, nil, 0, nil, 16); !errors.Is(err, ErrClosed) {
		t.Fatal("zero frame", err)
	}
	ops := testMemory()
	f, err := newCallFrame(2, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	e, err := newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
		if a0 != pointer(f.mem) || a1 != pointer(f.mem[f.capacity*8:]) {
			panic("serialized pointers")
		}
		args := f.mem[:16]
		result := f.mem[f.capacity*8:]
		binary.LittleEndian.PutUint64(result, binary.LittleEndian.Uint64(args)+binary.LittleEndian.Uint64(args[8:]))
		return 0, 0
	}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	b, err := newCodeBuffer(4, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	b.Append([]byte{0x13, 0, 0, 0})
	p, err := publishImage(b, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, args := range [][]uint64{{^uint64(0), 0}, {1, 2}, {0xffffffff, 0}} {
		got, err := f.Call(e, p, 0, args, 16)
		if err != nil || got != args[0]+args[1] {
			t.Fatal(got, err)
		}
	}
	before := append([]byte(nil), f.mem...)
	if _, err = f.Call(e, p, 0, []uint64{1, 2, 3}, 16); err == nil || !bytes.Equal(before, f.mem) {
		t.Fatal("capacity rejection changed cells", err)
	}
	failure := errors.New("injected frame unmap failure")
	unmap := ops.unmap
	ops.unmap = func([]byte) error { return failure }
	base := pointer(f.mem)
	if err = f.Close(); !errors.Is(err, failure) || len(f.mem) == 0 {
		t.Fatal("cleanup ownership", err)
	}
	if _, err = f.Call(e, p, 0, []uint64{1}, 16); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	ops.unmap = unmap
	if err = f.Close(); err != nil || permissions(t, base) != "" {
		t.Fatal("cleanup retry", err)
	}
}

func TestSerializedFrameCloseWaitsForInvocation(t *testing.T) {
	ops := testMemory()
	f, err := newCallFrame(1, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	e, err := newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
		close(entered)
		<-release
		return 0, 0
	}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	b, err := newCodeBuffer(4, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	b.Append([]byte{0x13, 0, 0, 0})
	p, err := publishImage(b, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	called, closed := make(chan error, 1), make(chan error, 1)
	go func() { _, err := f.Call(e, p, 0, []uint64{1}, 16); called <- err }()
	<-entered
	base := pointer(f.mem)
	go func() { closed <- f.Close() }()
	select {
	case err := <-closed:
		t.Fatal("retired active serialized frame", err)
	case <-time.After(10 * time.Millisecond):
	}
	if permissions(t, base) == "" {
		t.Fatal("active cells unmapped")
	}
	close(release)
	if err := <-called; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if permissions(t, base) != "" {
		t.Fatal("frame not retired")
	}
}
