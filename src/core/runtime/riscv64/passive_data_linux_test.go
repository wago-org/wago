//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGuestContextPassiveDataOwnershipAndLayout(t *testing.T) {
	source := []byte{3, 5, 7, 11}
	globals := make([]uint64, 256)
	globals[255] = 91
	data := make([][]byte, MaxDataSegments)
	data[1], data[255] = source, []byte{17, 19}
	c, err := NewGuestContextWithData(0, globals, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	clear(source)
	if got, err := c.Global(255); err != nil || got != 91 {
		t.Fatal("global layout changed", got, err)
	}
	if perm := permissions(t, pointer(c.mem)); !strings.HasPrefix(perm, "rw-") {
		t.Fatal("context permissions", perm)
	}
	head := 257*8 + MaxDataSegments*16
	if len(c.mem) < head+6 || !bytes.Equal(c.mem[head:head+6], []byte{3, 5, 7, 11, 17, 19}) {
		t.Fatal("passive bytes borrowed or reordered")
	}
	for j := 0; j < MaxDataSegments; j++ {
		desc := 257*8 + j*16
		base, size := binary.LittleEndian.Uint64(c.mem[desc:]), binary.LittleEndian.Uint64(c.mem[desc+8:])
		wantBase, wantSize := uint64(0), uint64(0)
		if j == 1 {
			wantBase, wantSize = uint64(pointer(c.mem[head:])), 4
		} else if j == 255 {
			wantBase, wantSize = uint64(pointer(c.mem[head+4:])), 2
		}
		if base != wantBase || size != wantSize {
			t.Fatal("data index layout", j, base, size, wantBase, wantSize)
		}
	}
}

func TestGuestContextPassiveDataBoundsAndCleanup(t *testing.T) {
	ops := testMemory()
	maps := 0
	mapRW := ops.mapRW
	ops.mapRW = func(n int) ([]byte, error) { maps++; return mapRW(n) }
	for _, input := range []struct {
		globals []uint64
		data    [][]byte
	}{{make([]uint64, 257), nil}, {nil, make([][]byte, 257)}, {nil, [][]byte{make([]byte, MaxGuestDataBytes), {1}}}} {
		if c, err := newGuestContextWithData(0, input.globals, input.data, ops); c != nil || err == nil || maps != 0 {
			t.Fatal("bounds checked after allocation", c, err, maps)
		}
	}
	c, err := newGuestContextWithData(0, nil, [][]byte{make([]byte, MaxGuestDataBytes)}, ops)
	if err != nil || maps != 1 {
		t.Fatal("exact data quota", err, maps)
	}
	fail := errors.New("injected data arena retirement failure")
	unmap := ops.unmap
	ops.unmap = func([]byte) error { return fail }
	if err := c.Close(); !errors.Is(err, fail) || len(c.mem) == 0 {
		t.Fatal("failed cleanup lost ownership", err)
	}
	if _, err := c.Call(nil, nil, nil, nil, 0, nil, 16); !errors.Is(err, ErrClosed) {
		t.Fatal("closed arena callable", err)
	}
	ops.unmap = unmap
	if err := c.Close(); err != nil || len(c.mem) != 0 {
		t.Fatal("cleanup retry", err)
	}
	ops.mapRW = func(int) ([]byte, error) { return nil, fail }
	if c, err := newGuestContextWithData(0, nil, [][]byte{{1}}, ops); c != nil || !errors.Is(err, fail) {
		t.Fatal("mapping failure", c, err)
	}
}

func TestGuestContextPassiveCallQuiescentClose(t *testing.T) {
	ops := testMemory()
	c, err := newGuestContextWithData(0, nil, [][]byte{{1, 2, 3}}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := newCallFrame(0, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	e, err := newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, n, trap uintptr) (uint64, uint64) {
		close(entered)
		<-release
		// Simulate data.drop while the context still owns a callable arena.
		binary.LittleEndian.PutUint64(c.mem[16:], 0)
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
	if err = b.Append([]byte{0x13, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	p, err := publishImage(b, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	called := make(chan error, 1)
	go func() { _, err := c.Call(nil, f, e, p, 0, nil, 16); called <- err }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case err := <-closed:
		t.Fatal("data arena retired during guest call", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-called; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}
