//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func alignedStoreFixture(t testing.TB, mode string) *wasm.Module {
	body := []byte{1, 1, 0x7f, 0x20, 0}
	switch mode {
	case "mask", "conditional", "later-write":
		body = append(body, 0x41, 0x78, 0x71)
	case "shift", "add", "or":
		body = append(body, 0x41, 3, 0x74)
	case "mul":
		body = append(body, 0x41, 8, 0x6c)
	case "shift32":
		body = append(body, 0x41, 32, 0x74)
	}
	if mode == "add" {
		body = append(body, 0x41, 8, 0x6a)
	}
	if mode == "or" {
		body = append(body, 0x41, 8, 0x72)
	}
	body = append(body, 0x21, 1)
	if mode == "conditional" {
		body = append(body, 0x20, 0, 0x41, 1, 0x71, 0x04, 0x40, 0x20, 0, 0x21, 1, 0x0b)
	}
	body = append(body, 0x20, 1, 0x41)
	body = append(body, wasmtest.SLEB32(0x11223344)...)
	body = append(body, 0x36, 2, 0)
	if mode == "later-write" {
		body = append(body, 0x20, 0, 0x21, 1)
	}
	body = append(body, 0x20, 1, 0x41)
	body = append(body, wasmtest.SLEB32(0x55667788)...)
	body = append(body, 0x36, 2, 4)
	if mode == "later-aligned-write" {
		body = append(body, 0x20, 0, 0x41, 0x78, 0x71, 0x21, 1)
	}
	body = append(body, 0x41, 0, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func runAlignedStoreMemory(t *testing.T, m *wasm.Module, opts CompileOptions, arg uint64) ([]byte, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	clear(jm.CurrentBytes())
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	args, results, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
	binary.LittleEndian.PutUint64(args, arg)
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	return append([]byte(nil), jm.CurrentBytes()...), err
}

func TestAlignedBoundsCertificateStoresAndTrapOrderArm64(t *testing.T) {
	for _, mode := range []string{"mask", "shift", "mul", "add", "or", "raw", "shift32", "conditional", "later-write", "later-aligned-write"} {
		m := alignedStoreFixture(t, mode)
		for _, enabled := range []bool{false, true} {
			for _, arg := range []uint64{0, 7, 8, 8191, 8192, 65528, 65531, 65532, 65535, 65536, 0x887766550000fffC, 0xffffffff} {
				p := uint32(arg)
				base := p
				switch mode {
				case "mask", "conditional", "later-write":
					base = p &^ 7
				case "shift", "mul":
					base = p << 3
				case "add":
					base = (p << 3) + 8
				case "or":
					base = (p << 3) | 8
				}
				if mode == "conditional" && p&1 != 0 {
					base = p
				}
				want := make([]byte, 65536)
				traps := uint64(base)+4 > uint64(len(want))
				if !traps {
					binary.LittleEndian.PutUint32(want[int(base):], 0x11223344)
					next := base
					if mode == "later-write" {
						next = p
					}
					traps = uint64(next)+8 > uint64(len(want))
					if !traps {
						binary.LittleEndian.PutUint32(want[int(next)+4:], 0x55667788)
					}
				}
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"memory-min-bounds": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runAlignedStoreMemory(t, m, opts, arg)
				if (err != nil) != traps || !bytes.Equal(got, want) {
					t.Fatalf("mode=%s enabled=%v arg=%x: trap=%v want=%v memoryEqual=%v", mode, enabled, arg, err, traps, bytes.Equal(got, want))
				}
				if diagnosticsEnabled {
					aligned := mode == "mask" || mode == "shift" || mode == "mul" || mode == "add" || mode == "or"
					hits := stats.Funcs[0].Peephole["aligned-bounds-certificate"]
					if (hits != 0) != (aligned && enabled) {
						t.Fatalf("mode=%s enabled=%v: selections=%d", mode, enabled, hits)
					}
				}
			}
		}
	}
}

func TestAlignedBoundsExtentOverflowAndSource(t *testing.T) {
	f := fn{localAddressAligned8: 2}
	for _, tc := range []struct {
		kind         uint8
		index        uint32
		extent, want int32
	}{{1, 1, 4, 8}, {1, 1, 9, 16}, {1, 1, 0x7ffffff8, 0x7ffffff8}, {1, 1, 0x7ffffff9, 0x7ffffff9}, {1, 1, 0, 0}, {2, 1, 4, 4}, {1, 0, 4, 4}, {1, 64, 4, 4}} {
		if got := f.alignedAddressExtent(tc.kind, tc.index, tc.extent); got != tc.want {
			t.Fatalf("%+v: got %d", tc, got)
		}
	}
}
