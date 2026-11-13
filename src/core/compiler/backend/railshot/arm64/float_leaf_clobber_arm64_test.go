//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatLocalAcrossPinPreservingSIMDLeaf(t *testing.T) {
	callee := []byte{0}
	for i := 0; i < 12; i++ {
		callee = append(callee, simdConst(i64x2Bytes(int64(i+1), int64(i+11)))...)
	}
	for i := 1; i < 12; i++ {
		callee = append(callee, simdOp(81)...)
	}
	callee = append(callee, simdOp(29)...)
	callee = append(callee, 1, 0x0b)
	for _, wide := range []bool{false, true} {
		want, localType, constOp, reinterpret, result := uint64(0x40490fdb), byte(0x7d), byte(0x43), byte(0xbc), wasm.I32
		var bits [8]byte
		if wide {
			want, localType, constOp, reinterpret, result = 0x400921fb54442d18, 0x7c, 0x44, 0xbd, wasm.I64
		}
		binary.LittleEndian.PutUint64(bits[:], want)
		caller := []byte{1, 1, localType, constOp}
		if wide {
			caller = append(caller, bits[:]...)
		} else {
			caller = append(caller, bits[:4]...)
		}
		caller = append(caller, 0x21, 0, 0x10, 1, 0x1a, 0x20, 0, reinterpret, 0x0b)
		m := modFuncs(t, funcDef{nil, []wasm.ValType{result}, caller}, funcDef{nil, []wasm.ValType{wasm.I64}, callee})
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"inline": false}})
		if err != nil || got != want {
			t.Fatalf("wide=%v got %#x, want %#x, err %v", wide, got, want, err)
		}
	}
}
