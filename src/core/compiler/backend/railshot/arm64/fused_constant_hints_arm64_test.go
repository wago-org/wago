//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestFusedConstantHints(t *testing.T) {
	old := fusedConstantHintsEnabled
	defer func() { fusedConstantHintsEnabled = old }()
	for _, enabled := range []bool{false, true} {
		fusedConstantHintsEnabled = enabled
		for _, wide := range []bool{false, true} {
			mul, add, constant, typ := byte(0x6c), byte(0x6a), byte(0x41), byte(1)
			if wide {
				mul, add, constant, typ = 0x7e, 0x7c, 0x42, 2
			}
			body := []byte{0x03, 0x40, mul, constant, 1, add, 0x0b, 0x0b}
			h, err := scanBodyBytes(body, 0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			want := uint8(0)
			if enabled {
				want = 1
			}
			if h.loopIntConstCount != want || enabled && (h.loopIntConst[0] != 1 || h.loopIntConstTypes&3 != typ) {
				t.Fatalf("wide%v enabled%v count%d", wide, enabled, h.loopIntConstCount)
			}
			for _, previous := range []byte{0x20, 0x21, 0x22, 0x6a, 0x6b, 0x7c, 0x7d} {
				if fusedConstantHintConsumer(previous, add, typ, 1) != add {
					t.Fatal("unrelated consumer changed")
				}
			}
			if fusedConstantHintConsumer(mul, add, typ, 0) != add {
				t.Fatal("zero allocated a lease")
			}
		}
	}
}

func TestFusedConstantHintsExecution(t *testing.T) {
	old := fusedConstantHintsEnabled
	defer func() { fusedConstantHintsEnabled = old }()
	body := []byte{0, 0x03, 0x40, 0x20, 0, 0x20, 0, 0x41, 4, 0x6a, 0x6c, 0x41, 1, 0x6a, 0x21, 0, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x20, 0, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		fusedConstantHintsEnabled = enabled
		for _, count := range []uint32{1, 2, 17, 65} {
			for _, seed := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
				want := seed
				for n := uint32(0); n < count; n++ {
					want = want*(want+4) + 1
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, uint64(seed), uint64(count))
				if err != nil || got != uint64(want) {
					t.Fatalf("enabled%v count%d seed%x got%x want%x err%v", enabled, count, seed, got, want, err)
				}
			}
		}
	}
}
