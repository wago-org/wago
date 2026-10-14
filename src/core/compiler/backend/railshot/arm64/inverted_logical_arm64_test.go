//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestInvertedLogicalOperands(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		constant, xor := byte(0x41), byte(0x73)
		ops := []byte{0x71, 0x72, 0x73}
		if typ == wasm.I64 {
			constant, xor = 0x42, 0x85
			ops = []byte{0x83, 0x84, 0x85}
		}
		for index, op := range ops {
			for _, invertLeft := range []bool{false, true} {
				body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0}
				if invertLeft {
					body = append(body, constant, 0x7f, xor)
				}
				body = append(body, 0x20, 1)
				if !invertLeft {
					body = append(body, constant, 0x7f, xor)
				}
				// Assign directly to a source local, including the inverted source.
				body = append(body, op, 0x22, 0, 0x0b)
				m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
				m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
				for _, enabled := range []bool{false, true} {
					opts := CompileOptions{Optimizations: map[string]bool{"inverted-logical": enabled}}
					if diagnosticsEnabled {
						stats := &ModuleStats{}
						opts.Stats = stats
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
						if (stats.Funcs[0].Peephole["inverted-logical"] != 0) != enabled {
							t.Fatalf("type=%v op=%x left=%v enabled=%v stats=%v", typ, op, invertLeft, enabled, stats.Funcs[0].Peephole)
						}
						opts.Stats = nil
					}
					for _, in := range [][2]uint64{{0, 0}, {1, 2}, {0xffffffff, 0x80000000}, {0xfedcba9876543210, 0x13579bdf2468ace0}, {^uint64(0), ^uint64(0)}} {
						a, b := in[0], in[1]
						if invertLeft {
							a = ^a
						} else {
							b = ^b
						}
						want := a & b
						if index == 1 {
							want = a | b
						}
						if index == 2 {
							want = a ^ b
						}
						if typ == wasm.I32 {
							want = uint64(uint32(want))
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, in[:]...)
						if err != nil {
							t.Fatal(err)
						}
						if got != want {
							t.Fatalf("type=%v op=%x left=%v enabled=%v inputs=%x got=%x want=%x", typ, op, invertLeft, enabled, in, got, want)
						}
					}
				}
			}
		}
	}
}
