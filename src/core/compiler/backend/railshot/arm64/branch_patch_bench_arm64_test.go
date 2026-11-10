//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Small, valid branch-heavy functions measure compilation only. No generated
// code is executed, and no branch approaches an instruction range boundary.
func BenchmarkCompileBranchPatchesARM64(b *testing.B) {
	for _, count := range []int{1, 128} {
		name := "small"
		if count != 1 {
			name = "many"
		}
		b.Run(name, func(b *testing.B) {
			var body []byte
			for i := 0; i < count; i++ {
				body = append(body, 0x20, 0x00, 0x04, 0x7f, 0x20, 0x00, 0x05, 0x41, 0x07, 0x0b, 0x1a)
			}
			body = append(body, 0x0b)
			m, err := wasm.DecodeModule(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
		})
	}
}
