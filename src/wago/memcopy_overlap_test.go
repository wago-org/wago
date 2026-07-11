//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestMemoryCopyOverlapMatrix exercises dynamic memory.copy (memmove semantics)
// across every internal size tier (byte / 8 / 16 / 64-group) and overlap shape:
// dst>src overlapping (backward path), dst<src (forward), and disjoint. It guards
// the amd64 backward-SSE block copy that replaced `std; rep movsb; cld`.
func TestMemoryCopyOverlapMatrix(t *testing.T) {
	// (module (memory 2) (func (export "docopy") (param i32 i32 i32)
	//   (memory.copy (local.get 0) (local.get 1) (local.get 2))))
	body := []byte{
		0x20, 0x00, // local.get 0 (dst)
		0x20, 0x01, // local.get 1 (src)
		0x20, 0x02, // local.get 2 (n)
		0xfc, 0x0a, 0x00, 0x00, // memory.copy dst=0 src=0
		0x0b, // end
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x02})), // 1 memory, min 2 pages
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("docopy", 0, 0),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)

	sizes := []int{0, 1, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 71, 72, 95, 96, 97, 127, 128, 255, 256, 257, 512, 1024, 4095, 4096, 4097}
	// dst - src offset: positive = dst ahead (backward path when overlapping);
	// negative = src ahead (forward); large = disjoint.
	deltas := []int{1, 3, 7, 8, 15, 16, 17, 31, 63, 64, 128, 256, -1, -8, -16, -64, 65536}

	base := 8192 // keep both regions well inside the 2-page (131072 B) memory
	for _, n := range sizes {
		for _, dlt := range deltas {
			var dst, src int
			if dlt >= 0 {
				src, dst = base, base+dlt
			} else {
				dst, src = base, base-dlt
			}
			name := fmt.Sprintf("n=%d/delta=%d", n, dlt)
			t.Run(name, func(t *testing.T) {
				in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
				if err != nil {
					t.Fatalf("instantiate: %v", err)
				}
				defer in.Close()
				mem := in.Memory().Bytes()
				// Seed a position-dependent pattern so every byte is checkable.
				for i := range mem {
					mem[i] = byte((i*31 + 7) & 0xff)
				}
				want := append([]byte(nil), mem...)
				copy(want[dst:dst+n], want[src:src+n]) // Go copy = memmove reference

				if _, err := in.Invoke("docopy", uint64(uint32(dst)), uint64(uint32(src)), uint64(uint32(n))); err != nil {
					t.Fatalf("docopy(%d,%d,%d): %v", dst, src, n, err)
				}
				for i := range want {
					if mem[i] != want[i] {
						t.Fatalf("byte %d = %#x, want %#x (dst=%d src=%d n=%d)", i, mem[i], want[i], dst, src, n)
					}
				}
			})
		}
	}
}
