//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// A constant-first addition reaches leaDisp rather than the right-constant
// identity simplifier. Its deferred i32 result promises a zero upper half.
func TestZeroDisplacementI32AddressCanonicalization(t *testing.T) {
	body := []byte{0, 0x41, 32, 0x41, 0, 0x20, 0, 0x6a, 0x2b, 0, 0, 0x39, 0, 0, 0x41, 0, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, base := range []uint32{0, 8, 17, 65528} {
		initial := bytes.Repeat([]byte{0x39}, 65536)
		copy(initial[base:], []byte{1, 2, 3, 4, 5, 6, 7, 8})
		want := append([]byte(nil), initial...)
		copy(want[32:], initial[base:base+8])
		for _, upper := range []uint64{0, 0xfeed00000000, 0xffffffff00000000} {
			for _, compact := range []bool{false, true} {
				got, err := runLinearSpanMemory(t, m, CompileOptions{CompactNative: compact}, initial, uint64(base)|upper)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("base%x upper%x compact%v: %v", base, upper, compact, err)
				}
			}
		}
	}
}
