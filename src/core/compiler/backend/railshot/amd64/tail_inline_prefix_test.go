//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTailLoopInlineRetainsCallerPrefixes(t *testing.T) {
	if profileEnabled {
		t.Skip("tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, eqz, block, rem, xor, add := wasm.I32, byte(0x45), byte(0x7f), byte(0x70), byte(0x73), byte(0x6a)
		if wide {
			typ, eqz, block, rem, xor, add = wasm.I64, 0x50, 0x7e, 0x82, 0x85, 0x7c
		}
		results := []wasm.ValType{typ}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, results, []byte{0, 0x20, 2, 0x20, 0, 0x20, 1, 0x10, 1, xor, 0x20, 2, add, 0x0b}},
			funcDef{[]wasm.ValType{typ, typ}, results, []byte{0, 0x20, 1, eqz, 0x04, block, 0x20, 0, 0x05, 0x20, 1, 0x20, 0, 0x20, 1, rem, 0x12, 1, 0x0b, 0x0b}})
		for _, input := range [][3]uint64{{0, 0, 0}, {17, 5, 0xdeadbeefffffffff}, {5, 17, 0x8000000000000000}, {75025, 46368, 1234567}, {^uint64(0), 31, 0xfffffffffffffff0}} {
			a, b, prefix := input[0], input[1], input[2]
			if !wide {
				a, b, prefix = uint64(uint32(a)), uint64(uint32(b)), uint64(uint32(prefix))
			}
			for b != 0 {
				a, b = b, a%b
			}
			want := (a ^ prefix) + prefix
			if !wide {
				want = uint64(uint32(want))
			}
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runAmd64TailInlineWithOptions(t, m, opts, input[0], input[1], input[2])
			if err != nil || got != want {
				t.Fatalf("wide=%t input=%v got=%x,%v want %x", wide, input, got, err, want)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-prefix-retained"] == 0 {
				t.Fatal("stable caller prefix was spilled")
			}
		}
	}
}
