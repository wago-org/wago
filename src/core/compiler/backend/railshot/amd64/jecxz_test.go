//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSizeDirectJecxzBulkTails(t *testing.T) {
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	tests := []struct {
		name string
		body []byte
	}{
		{"memory.copy", []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b}},
		{"memory.fill", []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0b, 0x00, 0x0b}},
	}
	before := directJecxzEnabled
	t.Cleanup(func() { directJecxzEnabled = before })
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := modMem(t, 1, params, nil, test.body)
			compile := func(enabled bool) *CodegenStats {
				directJecxzEnabled = enabled
				size := OptimizeSize
				stats := &ModuleStats{}
				cm, err := CompileModuleWith(m, CompileOptions{Objective: &size, Stats: stats, Workers: 1})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					t.Cleanup(func() { cm.CodeImage.Close() })
				}
				return stats.Funcs[0]
			}
			long := compile(false)
			short := compile(true)
			if got := long.CodeBytes - short.CodeBytes; got < 1 {
				t.Fatalf("code delta = %d bytes, want at least 1", got)
			}
			if got := short.Peephole["direct-jecxz"]; got != 1 {
				t.Fatalf("direct-jecxz hits = %d, want 1", got)
			}
		})
	}
}
