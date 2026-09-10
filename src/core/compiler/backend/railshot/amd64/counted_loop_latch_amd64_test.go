//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func countedLoopLatchModuleAMD64(t testing.TB, exactExit bool) *wasm.Module {
	t.Helper()
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 accumulator local
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // if counter == 0, exit block
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, // accumulator++
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // counter--
		0x0c, 0x00, // backedge
		0x0b, // end loop
	}
	if !exactExit {
		body = append(body, 0x01)
	}
	body = append(body, 0x0b, 0x20, 0x01, 0x0b) // end block; accumulator result
	return mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestCountedLoopLatchAMD64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, n := range []uint64{0, 1, 7, 100} {
			var stats ModuleStats
			cm, err := CompileModuleWith(countedLoopLatchModuleAMD64(t, true), CompileOptions{
				Stats:         &stats,
				Optimizations: map[string]bool{"counted-loop-latch": enabled},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := runCompiledAmd64u(t, cm, n)
			cm.CodeImage.Close()
			if got != n {
				t.Fatalf("enabled=%t n=%d: got %d", enabled, n, got)
			}
			want := 0
			if enabled {
				want = 1
			}
			if hits := stats.Funcs[0].Peephole["counted-loop-latch"]; hits != want {
				t.Fatalf("enabled=%t hits=%d, want %d", enabled, hits, want)
			}
		}
	}
}

func TestCountedLoopLatchAMD64RejectsUnsafeShapes(t *testing.T) {
	for _, test := range []struct {
		name          string
		exactExit     bool
		interruptible bool
	}{
		{name: "non-exact exit"},
		{name: "interruptible", exactExit: true, interruptible: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(countedLoopLatchModuleAMD64(t, test.exactExit), CompileOptions{
				Interruptible: test.interruptible,
				Stats:         &stats,
				Optimizations: map[string]bool{"counted-loop-latch": true},
			})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if hits := stats.Funcs[0].Peephole["counted-loop-latch"]; hits != 0 {
				t.Fatalf("unsafe shape fused %d latch(es)", hits)
			}
		})
	}
}
