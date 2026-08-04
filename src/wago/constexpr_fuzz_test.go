package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func FuzzScalarConstExprProgram(f *testing.F) {
	f.Add([]byte{0x41, 0x01, 0x0b})
	f.Add([]byte{0x23, 0x00, 0x41, 0x02, 0x6a, 0x0b})
	f.Add([]byte{0x41, 0x01})
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		if index != 0 {
			return 0, wasm.ValType{}, false, false
		}
		return 7, wasm.I32, false, true
	}
	f.Fuzz(func(t *testing.T, program []byte) {
		if len(program) > 4096 {
			t.Skip()
		}
		_, _, _ = evalScalarConstExprProgram(program, wasm.I32, resolve)
	})
}
