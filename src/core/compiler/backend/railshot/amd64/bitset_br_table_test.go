//go:build linux && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBitsetBrTable(t *testing.T) {
	for _, n := range []int{8, 31, 32, 63, 64} {
		labels := make([]uint32, n)
		for i := range labels {
			if i%3 == 1 || i == n-1 {
				labels[i] = 1
			}
		}
		m := brTableLabelsInRAX(t, labels, 0)
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["br-table-bitset"] != 1 {
			t.Fatalf("n=%d missing bitset: %v", n, stats.Funcs[0].Peephole)
		}
		for index := uint64(0); index < uint64(n+2); index++ {
			want := uint64(1000)
			if index < uint64(n) && labels[index] == 1 {
				want++
			}
			if got := runCompiledAmd64u(t, cm, index, 1); got != want {
				t.Fatalf("n=%d index=%d got=%d want=%d", n, index, got, want)
			}
		}
		for _, index := range []uint64{127, 0x80000000, 0xffffffff} {
			if got := runCompiledAmd64u(t, cm, index, 1); got != 1000 {
				t.Fatalf("index=%x wraps into mask: got=%d", index, got)
			}
		}
	}
}

func TestBitsetBrTableTypedMerge(t *testing.T) {
	body := []byte{0, 0x02, 0x7e, 0x02, 0x7e, 0x20, 1, 0x20, 0, 0x0e, 8, 0, 1, 0, 1, 0, 1, 0, 1, 0, 0x0b, 0x42, 7, 0x7c, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
	for _, compact := range []bool{false, true} {
		cm, e := CompileModuleWith(m, CompileOptions{CompactNative: compact})
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		for _, value := range []uint64{0, 1, 0x8000000000000000, ^uint64(0)} {
			for _, index := range []uint64{0, 1, 7, 8, 63, 64, 0xffffffff} {
				want := value + 7
				if index < 8 && index%2 == 1 {
					want = value
				}
				if got := runCompiledAmd64u(t, cm, index, value); got != want {
					t.Fatalf("compact=%v index=%x value=%x got=%x want=%x", compact, index, value, got, want)
				}
			}
		}
	}
}
