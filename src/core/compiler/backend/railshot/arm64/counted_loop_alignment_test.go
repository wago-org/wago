//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestCountedLoopBackedgeBodyAlignmentArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		opts := CompileOptions{Stats: &stats, Optimizations: map[string]bool{"counted-loop-body-align": enabled}}
		m := countedLoopLatchModuleArm64(t, true)
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for pc := 0; pc+4 <= len(cm.Code); pc += 4 {
			word := binary.LittleEndian.Uint32(cm.Code[pc:])
			if word&0xff00001f != 0x54000001 {
				continue
			} // B.NE
			delta := int32(word<<8) >> 13 // signed imm19, in instructions
			target := pc + int(delta)*4
			if target >= pc {
				continue
			}
			found = true
			if enabled && target%32 != 0 {
				t.Fatalf("backedge %d targets %d, not a fetch boundary", pc, target)
			}
		}
		cm.CodeImage.Close()
		if !found {
			t.Fatal("countdown backedge was not exercised")
		}
		want := 0
		if enabled {
			want = 1
		}
		if hits := stats.Funcs[0].Peephole["counted-loop-body-align"]; hits != want {
			t.Fatalf("enabled=%v alignment selections=%d want=%d", enabled, hits, want)
		}
		for _, n := range []uint64{0, 1, 2, 7, 1000} {
			got, err := runArm64WrapperWithOptions(t, m, opts, n)
			if err != nil || got != n {
				t.Fatalf("enabled=%v n=%d: got=%d err=%v", enabled, n, got, err)
			}
		}
	}
}

func TestCountedLoopAlignmentDeclinesPointerBackedgeArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x20, 0, 0x28, 2, 0, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		for _, input := range []uint64{0, 8, 65532} {
			var stats ModuleStats
			opts := CompileOptions{Stats: &stats, Optimizations: map[string]bool{"counted-loop-body-align": enabled}}
			got, err := runArm64WrapperWithOptions(t, m, opts, input)
			if err != nil || got != 0 {
				t.Fatalf("enabled=%v input=%d: got=%d err=%v", enabled, input, got, err)
			}
			if stats.Funcs[0].Peephole["counted-loop-body-align"] != 0 || stats.Funcs[0].Peephole["counted-loop-latch"] != 0 || stats.Funcs[0].Peephole["pretest-loop-latch"] != 0 {
				t.Fatal("memory-fed pointer backedge admitted as countdown or pretest latch")
			}
		}
	}
}
