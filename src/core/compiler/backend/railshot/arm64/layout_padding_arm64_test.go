//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestLayoutPaddingSkip(t *testing.T) {
	old := layoutPaddingSkipEnabled
	defer func() { layoutPaddingSkipEnabled = old }()
	for _, on := range []bool{false, true} {
		layoutPaddingSkipEnabled = on
		for _, words := range []uint32{0, 1, 7, 8, 9, 64, 513} {
			f := fn{a: &a64.Asm{}, policy: currentCodegenPolicy(), phasePadWords: int(words), byteBoundsPhaseWords: words}
			f.emitPhasePadding()
			if f.a.Len() != int(words)*4 || f.phasePadWords != 0 || f.byteBoundsPhaseWords != 0 {
				t.Fatalf("on=%v words=%d state mismatch", on, words)
			}
			for i := uint32(0); i < words; i++ {
				want := uint32(0xd503201f)
				if on && words >= 8 && i == 0 {
					want = 0x14000000 | words
				}
				if got := binary.LittleEndian.Uint32(f.a.B[i*4:]); got != want {
					t.Fatalf("on=%v words=%d i=%d got=%x want=%x", on, words, i, got, want)
				}
			}
			f.emitPhasePadding()
			if f.a.Len() != int(words)*4 {
				t.Fatal("emitted padding twice")
			}
		}
	}
}

func TestLayoutPaddingSkipNativeTrapPrefix(t *testing.T) {
	body := []byte{0, 0x41, 0, 0x41, 42, 0x3a, 0, 0}
	for i := byte(0); i < 32; i++ {
		body = append(body, 0x20, 0, 0x2d, 0, i, 0x1a)
	}
	body = append(body, 0x03, 0x40, 0x0b, 0x41, 0xc0, 0, 0x41, 55, 0x3a, 0, 0, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
	for _, addr := range []uint32{0, 1, 64, 65504, 65505, 65535, 65536, 0xffffffff} {
		var previous []byte
		for _, compact := range []bool{false, true} {
			for _, on := range []bool{false, true} {
				got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"layout-padding-skip": on}}, uint64(addr)|0xaabbccdd00000000)
				valid := uint64(addr)+32 <= 65536
				want := make([]byte, 65536)
				want[0] = 42
				if valid {
					want[64] = 55
				}
				if (err == nil) != valid || !bytes.Equal(got, want) {
					t.Fatalf("addr=%x compact=%v on=%v valid=%v err=%v", addr, compact, on, valid, err)
				}
				if previous != nil && !bytes.Equal(got, previous) {
					t.Fatal("enabled/disabled memory differs")
				}
				previous = got
			}
		}
	}
}
