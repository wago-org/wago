//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarGroupSumProof(t *testing.T) {
	old := scalarGroupSumEnabled
	scalarGroupSumEnabled = true
	t.Cleanup(func() { scalarGroupSumEnabled = old })
	for _, tc := range []struct {
		name  string
		words []uint32
		want  bool
	}{
		{"isolated", []uint32{0x0b090294, 0x11000673, 0x6b15027f}, true},
		{"feedback", []uint32{0x2a1403e9, 0x0b090294, 0x11000673, 0x6b15027f}, false},
		{"other-write", []uint32{0x52800014, 0x0b090294, 0x11000673, 0x6b15027f}, false},
		{"latch-observes-sum", []uint32{0x0b090294, 0x11000673, 0x6b14027f}, false},
		{"flags-add", []uint32{0x2b090294, 0x11000673, 0x6b15027f}, false},
		{"double-source", []uint32{0x0b140294, 0x11000673, 0x6b15027f}, false},
	} {
		f := gpClassEmitter()
		f.pinnedLocalMask = maskOf(X19, X20)
		p := f.planScalarGroupSum(tc.words, X19, X11)
		if (p.index >= 0) != tc.want {
			t.Fatalf("%s plan=%+v", tc.name, p)
		}
		if len(f.a.B) != 0 {
			t.Fatal("planning emitted code")
		}
		f.pinned = allAllocatableGP()
		if f.planScalarGroupSum(tc.words, X19, X11).index >= 0 {
			t.Fatal("pressure did not decline")
		}
	}
}

func TestScalarGroupSumWrappingSeedsAndTails(t *testing.T) {
	old := scalarGroupSumEnabled
	t.Cleanup(func() { scalarGroupSumEnabled = old })
	sizes := []uint64{63, 64, 65, 127, 128, 129, 4095, 4096, 4097}
	for n := uint64(0); n <= 33; n++ {
		sizes = append(sizes, n)
	}
	for _, wideCounter := range []bool{false, true} {
		ctByte := byte(0x7f)
		if wideCounter {
			ctByte = 0x7e
		}
		ct, ck, ca, ceq, can, clt := wasm.I32, byte(0x41), byte(0x6a), byte(0x45), byte(0x71), byte(0x49)
		if wideCounter {
			ct, ck, ca, ceq, can, clt = wasm.I64, 0x42, 0x7c, 0x50, 0x83, 0x54
		}
		for _, wideSum := range []bool{false, true} {
			st, sbt, sk, sa := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a)
			if wideSum {
				st, sbt, sk, sa = wasm.I64, 0x7e, 0x42, 0x7c
			}
			body := []byte{1, 1, ctByte, 0x02, 0x40, 0x20, 0, ceq, 0x0d, 0, 0x03, 0x40,
				0x20, 1, 0x02, sbt, 0x20, 2, ck, 63, can, ceq, 0x04, sbt, sk, 19, 0x05, sk, 3, 0x0b, 0x0b, sa, 0x21, 1,
				0x20, 2, ck, 1, ca, 0x22, 2, 0x20, 0, clt, 0x0d, 0, 0x0b, 0x0b, 0x20, 1, 0x0b}
			m := mod1(t, []wasm.ValType{ct, st}, []wasm.ValType{st}, body)
			for _, enabled := range []bool{false, true} {
				scalarGroupSumEnabled = enabled
				for _, compact := range []bool{false, true} {
					for _, n := range sizes {
						for _, seed := range []uint64{0, ^uint64(0), 0x8877665580000001} {
							in := n
							if !wideCounter {
								in |= 0x8877665500000000
							}
							want := seed + 3*n + 16*((n+63)/64)
							if !wideSum {
								want = uint64(uint32(want))
							}
							got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact}, in, seed)
							// The internal wrapper exposes raw carrier bits; I32 semantics
							// observe only the low word, including the zero-iteration return.
							if !wideSum {
								got = uint64(uint32(got))
							}
							if err != nil || got != want {
								t.Fatalf("counter64%v sum64%v enabled%v compact%v n%d seed%x got%x want%x err%v", wideCounter, wideSum, enabled, compact, n, seed, got, want, err)
							}
						}
					}
				}
			}
		}
	}
}
