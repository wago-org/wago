//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCI31ReductionMetadata(t *testing.T) {
	body := []byte{1, 2, 0x7f, 0x03, 0x40, 0x20, 2, 0x20, 1, 0xfb, 28, 0xfb, 30, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b}
	var bodies, exports, types [][]byte
	for i := 0; i < 4; i++ {
		b := append([]byte(nil), body...)
		if i%2 != 0 {
			b = append(append(append([]byte(nil), body[:3]...), 1), body[3:]...)
		}
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(b))), b...))
		types = append(types, []byte{0})
		exports = append(exports, wasmtest.ExportEntry(string(rune('a'+i)), 0, uint32(i)))
	}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(types...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	for _, workers := range []int{1, 2} {
		for _, interruptible := range []bool{false, true} {
			for _, source := range []bool{false, true} {
				if source && !profileEnabled {
					continue
				}
				m, err := wasm.DecodeModule(data)
				if err != nil {
					t.Fatal(err)
				}
				if err = wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				var stats *ModuleStats
				if diagnosticsEnabled {
					stats = &ModuleStats{}
				}
				c, err := CompileModuleWith(m, CompileOptions{Workers: workers, Interruptible: interruptible, Profile: source, SourceMaps: source, Stats: stats})
				if err != nil {
					t.Fatal(err)
				}
				for i := range m.Code {
					want := i%2 == 0 && !interruptible && !(profileEnabled && source)
					if got := directPreparedMarked(c.DirectPreparedBounded, i); got != want {
						t.Fatalf("workers=%d interrupt=%v source=%v function=%d bounded=%v want=%v", workers, interruptible, source, i, got, want)
					}
				}
				if c.CodeImage != nil {
					c.CodeImage.Close()
				}
			}
		}
	}
}
