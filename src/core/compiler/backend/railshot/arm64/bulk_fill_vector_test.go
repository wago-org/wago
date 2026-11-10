//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBulkFillVectorSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for n := 15; n <= 65; n++ {
		for _, enabled := range []bool{false, true} {
			body := []byte{0, 0x20, 0, 0x20, 1, 0x41}
			if n < 64 {
				body = append(body, byte(n))
			} else {
				body = append(body, byte(n)|0x80, 0)
			}
			body = append(body, 0xfc, 11, 0, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, body)
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"bulk-fill-vector": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			want := 0
			if enabled && n >= 16 && n <= 64 {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["memfill-vector"]; got != want {
				t.Fatalf("n=%d enabled=%t got=%d want=%d", n, enabled, got, want)
			}
		}
	}
}
