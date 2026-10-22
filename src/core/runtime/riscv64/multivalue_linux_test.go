//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestMultivalueFrameLayoutAndTransactionalCopy(t *testing.T) {
	for _, pair := range [][2]int{{-1, 0}, {257, 1}, {0, -1}, {0, 257}} {
		if _, err := NewCallFrameLayout(pair[0], pair[1]); err == nil {
			t.Fatal("invalid capacities", pair)
		}
	}
	for _, params := range []int{0, 256} {
		ops := testMemory()
		f, err := newCallFrameLayout(params, 256, ops)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		calls := 0
		trapCode := uint32(0)
		var e *Engine
		e, err = newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
			calls++
			if a0 != pointer(f.mem[8:]) || a1 != pointer(f.mem[(params+1)*8:]) {
				panic("ranges")
			}
			if binary.LittleEndian.Uint64(f.mem) != 0xabcdef {
				panic("fixed context header")
			}
			result := f.mem[(params+1)*8:]
			for j := 0; j < 256; j++ {
				if binary.LittleEndian.Uint64(result[j*8:]) != 0 {
					panic("stale result")
				}
			}
			for j := 0; j < 256; j++ {
				binary.LittleEndian.PutUint64(result[j*8:], uint64(j)+0x100000000)
			}
			if trap != pointer(e.trap) {
				panic("trap pointer")
			}
			binary.LittleEndian.PutUint32(e.trap, trapCode)
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
		args := make([]uint64, params)
		for j := range args {
			args[j] = uint64(j) ^ 0xdeadbeef00000000
		}
		results := make([]uint64, 256)
		for range 3 {
			if err = f.CallIntoWithContext(e, p, 0, args, results, 16, 0xabcdef); err != nil {
				t.Fatal(err)
			}
			for j, v := range results {
				if v != uint64(j)+0x100000000 {
					t.Fatal(j, v)
				}
			}
		}
		if params == 256 {
			// The API allows one caller-owned slice to serve as arguments and
			// results: all inputs are staged before any outputs are copied.
			if err = f.CallIntoWithContext(e, p, 0, args, args, 16, 0xabcdef); err != nil {
				t.Fatal(err)
			}
			for j, v := range args {
				if v != uint64(j)+0x100000000 {
					t.Fatal("aliased arguments/results", j, v)
				}
			}
		}
		beforeCalls := calls
		before := append([]byte(nil), f.mem...)
		if err = f.CallIntoWithContext(e, p, 0, args, make([]uint64, 257), 16, 1); err == nil || !bytes.Equal(before, f.mem) || calls != beforeCalls {
			t.Fatal("result capacity admitted or mutated", err, calls)
		}
		if err = f.CallIntoWithContext(e, p, 0, make([]uint64, params+1), results, 16, 1); err == nil || !bytes.Equal(before, f.mem) || calls != beforeCalls {
			t.Fatal("argument capacity admitted or mutated", err, calls)
		}
		for j := range results {
			results[j] = 0x55555555
		}
		trapCode = 9
		if err = f.CallIntoWithContext(e, p, 0, args, results, 16, 0xabcdef); err == nil {
			t.Fatal("missing trap")
		}
		for _, v := range results {
			if v != 0x55555555 {
				t.Fatal("trap committed output")
			}
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
		if err = f.CallIntoWithContext(e, p, 0, args, results, 16, 0); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
}

func TestVoidFrameSentinelAndCleanupRetry(t *testing.T) {
	ops := testMemory()
	f, err := newCallFrameLayout(0, 0, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	e, err := newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
		if a0 != pointer(f.mem[8:]) || a1 != a0 || binary.LittleEndian.Uint64(f.mem) != 17 || binary.LittleEndian.Uint64(f.mem[8:]) != 0 {
			panic("void frame sentinel")
		}
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
	if err = f.CallIntoWithContext(e, p, 0, nil, nil, 16, 17); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected new-layout unmap failure")
	unmap := ops.unmap
	base := pointer(f.mem)
	ops.unmap = func([]byte) error { return failure }
	if err = f.Close(); !errors.Is(err, failure) || len(f.mem) == 0 {
		t.Fatal("cleanup ownership", err)
	}
	ops.unmap = unmap
	if err = f.CallIntoWithContext(e, p, 0, nil, nil, 16, 17); !errors.Is(err, ErrClosed) {
		t.Fatal("closed frame entered", err)
	}
	if err = f.Close(); err != nil || permissions(t, base) != "" {
		t.Fatal("cleanup retry", err)
	}
}
