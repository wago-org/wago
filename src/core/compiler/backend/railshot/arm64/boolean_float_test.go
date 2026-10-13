//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBooleanFloatSelectionAndFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, enabled := range []bool{false, true} {
		for _, hotLiteral := range []bool{false, true} {
			body := []byte{2, 1, 0x7f, 1, 0x7d, 0x03, 0x40, 0x41, 1, 0x20, 0, 0x41, 1, 0x71, 0x41, 1, 0x74, 0x6a, 0xb3, 0x21, 3}
			if hotLiteral {
				body = append(body, 0x43, 0, 0, 0, 63, 0x1a)
			}
			body = append(body, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x20, 3, 0xbc, 0x0b)
			m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body})
			options := CompileOptions{Optimizations: map[string]bool{"boolean-float-select": enabled}}
			var stats ModuleStats
			options.Stats = &stats
			c, err := CompileModuleWith(m, options)
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			want := 0
			if enabled && !hotLiteral {
				want = 1
			}
			if got := stats.Funcs[0].Peephole["boolean-float-select"]; got != want {
				t.Fatalf("enabled=%t hotliteral=%t selected=%d want=%d", enabled, hotLiteral, got, want)
			}
			for _, x := range []uint64{0, 1, 2, 3, 0xffffffff} {
				want := uint64(0x3f800000)
				if x&1 != 0 {
					want = 0x40400000
				}
				got, err := runArm64WrapperWithOptions(t, m, options, x, 17)
				if err != nil || got != want {
					t.Fatalf("x=%x got=%x err=%v want=%x", x, got, err, want)
				}
			}
		}
	}
}
