//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestHighPressureAddTree(t *testing.T) {
	old := addTreeEnabled
	defer func() { addTreeEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, block, add := wasm.I32, byte(0x7f), byte(0x6a)
		if wide {
			typ, block, add = wasm.I64, 0x7e, 0x7c
		}
		params := []wasm.ValType{typ, typ, typ, typ, typ, typ, typ, typ}
		for _, sink := range []bool{false, true} {
			// A balanced eight-leaf sum has a register requirement of four.
			// Put the assignment destination in the final leaf.
			body := []byte{0, 0x03, block, 0x20, 1, 0x20, 2, add, 0x20, 3, 0x20, 4, add, add,
				0x20, 5, 0x20, 6, add, 0x20, 7, 0x20, 0, add, add, add}
			if sink {
				body = append(body, 0x21, 0, 0x20, 0)
			}
			body = append(body, 0x0b, 0x0b)
			m := mod1(t, params, []wasm.ValType{typ}, body)
			for _, enabled := range []bool{false, true} {
				addTreeEnabled = enabled
				var stats ModuleStats
				var opts CompileOptions
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				args := []uint64{0xdeadbeefffffffff, 3, 7, 1 << 63, 1 << 31, 11, 17, 23}
				want := uint64(0)
				for _, arg := range args {
					want += arg
				}
				if !wide {
					want = uint64(uint32(want))
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, args...)
				if err != nil || got != want {
					t.Fatalf("wide=%v sink=%v enabled=%v: got=%x want=%x err=%v", wide, sink, enabled, got, want, err)
				}
				if enabled && diagnosticsEnabled && stats.Funcs[0].Peephole["high-pressure-add-tree"] == 0 {
					t.Fatalf("wide=%v sink=%v: tree was not selected: %v", wide, sink, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestRepeatedSourceAddTree(t *testing.T) {
	old := addTreeEnabled
	defer func() { addTreeEnabled = old }()
	for _, enabled := range []bool{false, true} {
		addTreeEnabled = enabled
		// (x + 7) + (x + x), updating x in place.
		body := []byte{0, 0x03, 0x7f, 0x20, 0, 0x41, 7, 0x6a, 0x20, 0, 0x20, 0, 0x6a, 0x6a, 0x21, 0, 0x20, 0, 0x0b, 0x0b}
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		var stats ModuleStats
		var opts CompileOptions
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		for _, arg := range []uint64{0, 1, 0x80000000, 0xdeadbeefffffffff} {
			want := uint64(uint32(arg)*3 + 7)
			got, err := runArm64WrapperWithOptions(t, m, opts, arg)
			if err != nil || got != want {
				t.Fatalf("arg=%x enabled=%v: got=%x want=%x err=%v", arg, enabled, got, want, err)
			}
		}
		if enabled && diagnosticsEnabled && stats.Funcs[0].Peephole["repeated-source-add-tree"] == 0 {
			t.Fatalf("tree was not selected: %v", stats.Funcs[0].Peephole)
		}
	}
}
