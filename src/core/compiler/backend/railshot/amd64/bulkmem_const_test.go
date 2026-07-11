//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Constant-size memory.copy / memory.fill now unroll through 64 bytes (33..64 use
// the SSE 16-byte path for copy). These tests check exact-byte coverage: the whole
// destination is written and nothing one byte before or after it is disturbed,
// across sizes that exercise the overlapping 16-byte tail.

func copyConstModule(t *testing.T, n int) *wasm.Module {
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01} // dst, src
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(int32(n))...) // i32.const n
	body = append(body, 0xfc, 0x0a, 0x00, 0x00, 0x0b) // memory.copy; end
	return modMem(t, 1, []wasm.ValType{i32, i32}, nil, body)
}

func fillConstModule(t *testing.T, n int) *wasm.Module {
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01} // dst, val
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(int32(n))...) // i32.const n
	body = append(body, 0xfc, 0x0b, 0x00, 0x0b)       // memory.fill; end
	return modMem(t, 1, []wasm.ValType{i32, i32}, nil, body)
}

func TestBulkCopyConst33To64(t *testing.T) {
	const bg = 0xBB
	for _, n := range []int{32, 33, 40, 48, 63, 64} {
		const src, dst = 100, 4096
		_, mem, err := runMemAmd64(t, copyConstModule(t, n), func(m []byte) {
			for i := range m {
				m[i] = bg
			}
			for i := 0; i < n; i++ {
				m[src+i] = byte(i + 1) // distinct source pattern
			}
		}, dst, src)
		if err != nil {
			t.Fatalf("n=%d copy: %v", n, err)
		}
		want := make([]byte, n)
		for i := range want {
			want[i] = byte(i + 1)
		}
		if !bytes.Equal(mem[dst:dst+n], want) {
			t.Errorf("n=%d: dst region = % x, want % x", n, mem[dst:dst+n], want)
		}
		if mem[dst-1] != bg || mem[dst+n] != bg {
			t.Errorf("n=%d: copy disturbed a neighbouring byte (before=%#x after=%#x)", n, mem[dst-1], mem[dst+n])
		}
	}
}

func TestBulkFillConst33To64(t *testing.T) {
	const bg, val = 0xBB, 0x5A
	for _, n := range []int{32, 33, 40, 48, 63, 64} {
		const dst = 4096
		_, mem, err := runMemAmd64(t, fillConstModule(t, n), func(m []byte) {
			for i := range m {
				m[i] = bg
			}
		}, dst, val)
		if err != nil {
			t.Fatalf("n=%d fill: %v", n, err)
		}
		for i := 0; i < n; i++ {
			if mem[dst+i] != val {
				t.Fatalf("n=%d: fill byte %d = %#x, want %#x", n, i, mem[dst+i], val)
			}
		}
		if mem[dst-1] != bg || mem[dst+n] != bg {
			t.Errorf("n=%d: fill disturbed a neighbouring byte (before=%#x after=%#x)", n, mem[dst-1], mem[dst+n])
		}
	}
}

// TestBulkCopyConstOverlap checks memmove semantics for a forward-overlapping
// 33..64-byte copy (load-all precedes store-all, so the source is fully read
// before any destination byte is written).
func TestBulkCopyConstOverlap(t *testing.T) {
	for _, n := range []int{33, 48, 64} {
		const src = 200
		dst := src + 8 // overlaps
		_, mem, err := runMemAmd64(t, copyConstModule(t, n), func(m []byte) {
			for i := 0; i < n; i++ {
				m[src+i] = byte(i + 1)
			}
		}, uint64(dst), src)
		if err != nil {
			t.Fatalf("n=%d overlap copy: %v", n, err)
		}
		for i := 0; i < n; i++ {
			if mem[dst+i] != byte(i+1) {
				t.Fatalf("n=%d overlap: dst[%d] = %#x, want %#x", n, i, mem[dst+i], byte(i+1))
			}
		}
	}
}
