//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestShiftedALUConditionalArmsKeepExistingLayout(t *testing.T) {
	old := shiftedALUImmediateEnabled
	defer func() { shiftedALUImmediateEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, ty, c, xor, add, sub := wasm.I32, byte(0x7f), byte(0x41), byte(0x73), byte(0x6a), byte(0x6b)
		if wide {
			typ, ty, c, xor, add, sub = wasm.I64, 0x7e, 0x42, 0x85, 0x7c, 0x7d
		}
		body := []byte{1, 1, ty, 0x41, 0, 0x41, 42, 0x3a, 0, 0, 0x20, 1, 0x04, ty}
		arm := func(op byte) {
			body = append(body, 0x20, 0, c, 1, xor, c)
			body = append(body, wasmtest.SLEB64(65536)...)
			body = append(body, op, 0x21, 2, 0x20, 2)
		}
		arm(add)
		body = append(body, 0x05)
		arm(sub)
		body = append(body, 0x0b, 0x0b)
		m := mod1(t, []wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}, body)
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		for _, compact := range []bool{false, true} {
			var previous []byte
			for _, on := range []bool{false, true} {
				shiftedALUImmediateEnabled = on
				opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"ordinary-shifted-immediate": true}}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if previous == nil {
					previous = append([]byte(nil), cm.Code...)
				} else if !bytes.Equal(previous, cm.Code) {
					t.Fatalf("conditional layout changed: wide%v compact%v", wide, compact)
				}
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
				for _, x := range []uint64{0, 1, 65535, 65536, 0x7fffffff, 0x80000000, 0xffffffff, 0xfeed00000001, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffffffffffff} {
					for _, condition := range []uint64{0, 1, 0xfeed00000000, 0xfeed00000001} {
						want := (x ^ 1) - 65536
						if uint32(condition) != 0 {
							want = (x ^ 1) + 65536
						}
						if !wide {
							want = uint64(uint32(want))
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, x, condition)
						if err != nil || got != want {
							t.Fatalf("wide%v compact%v on%v x%x condition%x got%x want%x: %v", wide, compact, on, x, condition, got, want, err)
						}
					}
				}
			}
		}
	}
}
