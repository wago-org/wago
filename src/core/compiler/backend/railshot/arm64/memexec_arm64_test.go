//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// TestMemExec compiles a store+load function in guard-page mode (no bounds
// checks), sets linMemReg to a linear-memory buffer via the trampoline, executes it
// under qemu, and verifies the value round-trips through linear memory — proving
// the ported memory codegen (base+index+disp address fold) is correct, not just
// compiling.
// TestMemExecConstStoreLargeDisp is a regression test for a StoreImmIdx bug: a
// constant-value store whose memarg offset exceeds the scaled-immediate range
// (so the displacement is folded into X16 via addDispX16, which uses X17 as
// scratch) parked the store value in X17 *before* that fold — so the fold
// clobbered the value and the store wrote the displacement instead. Loads and
// register-value stores were unaffected; only const-value stores at large
// offsets, which is exactly dlmalloc's mparams init — hence every real Rust host-importing
// program aborted at its first heap allocation.
func TestMemExecConstStoreLargeDisp(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// offset 32768 (0x8000) is past the word-scaled imm12 range (0xFFF*4 = 16380),
	// forcing the large-displacement path. f(addr): mem[addr+32768] = 12345;
	// return mem[addr+32768].
	body := []byte{
		0x00,
		0x20, 0x00, 0x41, 0xb9, 0xe0, 0x00, 0x36, 0x02, 0x80, 0x80, 0x02, // local.get0; i32.const 12345; i32.store offset=32768
		0x20, 0x00, 0x28, 0x02, 0x80, 0x80, 0x02, // local.get0; i32.load offset=32768
		0x0b,
	}
	m := modMem(t, 1, i32, i32, body)
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map code: %v", err)
	}
	const head = 4096
	buf, err := arm64spike.MapRW(head + 65536)
	if err != nil {
		t.Fatalf("map mem: %v", err)
	}
	linMem := uintptr(unsafe.Pointer(&buf[head]))
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	const addr = 16
	got := arm64spike.Call3(entry, addr, 0, linMem)
	if uint32(got) != 12345 {
		t.Fatalf("returned %d, want 12345 (const store wrote the displacement, not the value)", uint32(got))
	}
	if inMem := binary.LittleEndian.Uint32(buf[head+addr+32768:]); inMem != 12345 {
		t.Fatalf("linear memory at [%d] = %d, want 12345", addr+32768, inMem)
	}
}

func TestMemExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// f(addr): mem[addr+4] = 12345; return mem[addr+4]
	body := []byte{
		0x00,
		0x20, 0x00, 0x41, 0xb9, 0xe0, 0x00, 0x36, 0x02, 0x04, // local.get0; i32.const 12345 (LEB b9 e0 00); i32.store offset=4
		0x20, 0x00, 0x28, 0x02, 0x04, // local.get0; i32.load offset=4
		0x0b,
	}
	m := modMem(t, 1, i32, i32, body)
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map code: %v", err)
	}
	// linMem buffer: 4 KiB basedata headroom (negative offsets) + 64 KiB memory.
	const head = 4096
	buf, err := arm64spike.MapRW(head + 65536)
	if err != nil {
		t.Fatalf("map mem: %v", err)
	}
	linMem := uintptr(unsafe.Pointer(&buf[head]))
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))

	const addr = 16
	got := arm64spike.Call3(entry, addr, 0, linMem)
	if uint32(got) != 12345 {
		t.Fatalf("returned %d, want 12345", uint32(got))
	}
	// mem[addr+4] in the buffer should hold 12345 (little-endian).
	inMem := binary.LittleEndian.Uint32(buf[head+addr+4:])
	if inMem != 12345 {
		t.Fatalf("linear memory at [%d] = %d, want 12345", addr+4, inMem)
	}
}

func TestBulkMemoryExecLargeArm64(t *testing.T) {
	const (
		head = 4096
		size = 65536
		n    = 137
	)
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	cases := []struct {
		name string
		body []byte
		a0   uintptr
		a1   uintptr
		init func([]byte)
		want func([]byte) []byte
		got  func([]byte) []byte
	}{
		{
			name: "copy-forward-disjoint",
			body: []byte{
				0x00,
				0x20, 0x00, // dst
				0x20, 0x01, // src
				0x41, 0x89, 0x01, // i32.const 137
				0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
				0x0b,
			},
			a0: 32, a1: 300,
			init: func(mem []byte) {
				for i := 0; i < n; i++ {
					mem[300+i] = byte(1 + i)
				}
			},
			want: func(mem []byte) []byte { return append([]byte(nil), mem[300:300+n]...) },
			got:  func(mem []byte) []byte { return mem[32 : 32+n] },
		},
		{
			name: "copy-backward-overlap",
			body: []byte{
				0x00,
				0x20, 0x00, // dst
				0x20, 0x01, // src
				0x41, 0x89, 0x01, // i32.const 137
				0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
				0x0b,
			},
			a0: 107, a1: 100,
			init: func(mem []byte) {
				for i := 0; i < n+16; i++ {
					mem[100+i] = byte(0xa0 + i)
				}
			},
			want: func(mem []byte) []byte {
				cp := append([]byte(nil), mem...)
				copy(cp[107:107+n], cp[100:100+n])
				return cp[107 : 107+n]
			},
			got: func(mem []byte) []byte { return mem[107 : 107+n] },
		},
		{
			name: "fill-large-tail",
			body: []byte{
				0x00,
				0x20, 0x00, // dst
				0x20, 0x01, // fill byte
				0x41, 0x89, 0x01, // i32.const 137
				0xfc, 0x0b, 0x00, // memory.fill 0
				0x0b,
			},
			a0: 512, a1: 0xab,
			init: func(mem []byte) {
				for i := range mem[512 : 512+n] {
					mem[512+i] = 0x11
				}
			},
			want: func(mem []byte) []byte { return bytes.Repeat([]byte{0xab}, n) },
			got:  func(mem []byte) []byte { return mem[512 : 512+n] },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modMem(t, 1, i32x2, nil, tc.body)
			cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true})
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			code, err := arm64spike.MapExec(cm.Code)
			if err != nil {
				t.Fatalf("map code: %v", err)
			}
			buf, err := arm64spike.MapRW(head + size)
			if err != nil {
				t.Fatalf("map mem: %v", err)
			}
			mem := buf[head:]
			binary.LittleEndian.PutUint32(buf[head-bdCurBytes:], size)
			tc.init(mem)
			want := tc.want(mem)

			entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
			arm64spike.Call3(entry, tc.a0, tc.a1, uintptr(unsafe.Pointer(&buf[head])))
			if got := tc.got(mem); !bytes.Equal(got, want) {
				t.Fatalf("memory mismatch\n got % x\nwant % x", got, want)
			}
		})
	}
}

func TestBulkMemoryExecConstArm64(t *testing.T) {
	const (
		head = 4096
		size = 65536
	)
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	constLEB := map[int][]byte{33: {0x21}, 63: {0x3f}, 64: {0xc0, 0x00}}
	for _, n := range []int{33, 63, 64} {
		for _, op := range []string{"copy-forward", "copy-backward", "fill"} {
			t.Run(fmt.Sprintf("%s/%d", op, n), func(t *testing.T) {
				body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41}
				body = append(body, constLEB[n]...)
				if op == "fill" {
					body = append(body, 0xfc, 0x0b, 0x00, 0x0b)
				} else {
					body = append(body, 0xfc, 0x0a, 0x00, 0x00, 0x0b)
				}
				m := modMem(t, 1, i32x2, nil, body)
				cm, err := CompileModuleWith(m, CompileOptions{})
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				code, err := arm64spike.MapExec(cm.Code)
				if err != nil {
					t.Fatalf("map code: %v", err)
				}
				buf, err := arm64spike.MapRW(head + size)
				if err != nil {
					t.Fatalf("map mem: %v", err)
				}
				mem := buf[head:]
				binary.LittleEndian.PutUint32(buf[head-bdCurBytes:], size)
				for i := range mem[:512] {
					mem[i] = byte(i*37 + 11)
				}
				want := append([]byte(nil), mem...)
				var dst, src uintptr
				switch op {
				case "copy-forward":
					dst, src = 16, 256
					copy(want[dst:dst+uintptr(n)], want[src:src+uintptr(n)])
				case "copy-backward":
					dst, src = 107, 100
					copy(want[dst:dst+uintptr(n)], want[src:src+uintptr(n)])
				case "fill":
					dst, src = 300, 0xab
					copy(want[dst:dst+uintptr(n)], bytes.Repeat([]byte{byte(src)}, n))
				}
				entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
				arm64spike.Call3(entry, dst, src, uintptr(unsafe.Pointer(&buf[head])))
				if !bytes.Equal(mem, want) {
					t.Fatal("constant bulk-memory result mismatch")
				}
			})
		}
	}
}
