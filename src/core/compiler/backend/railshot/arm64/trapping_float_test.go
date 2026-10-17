//go:build arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestTrappingFloatUsesTwoRangeChecksAndNativeUnsignedConversion(t *testing.T) {
	for _, sourceWide := range []bool{false, true} {
		for _, resultWide := range []bool{false, true} {
			for _, signed := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					source, result, op := wasm.F32, wasm.I32, byte(0xa8)
					if sourceWide {
						source = wasm.F64
						op += 2
					}
					if resultWide {
						result = wasm.I64
						op += 6
					}
					if !signed {
						op++
					}
					m := modFuncs(t, funcDef{params: []wasm.ValType{source}, results: []wasm.ValType{result}, body: []byte{0, 0x20, 0, op, 0x0b}})
					c, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"reg-abi": regABI}})
					if err != nil {
						t.Fatal(err)
					}
					defer c.CodeImage.Close()
					comparisons, unsigned := 0, 0
					for i := 0; i+4 <= len(c.Code); i += 4 {
						word := binary.LittleEndian.Uint32(c.Code[i:])
						if word&0xffa0fc1f == 0x1e202000 {
							comparisons++
						}
						if word&0x7fbffc00 == 0x1e390000 {
							unsigned++
						}
					}
					wantUnsigned := 0
					if !signed {
						wantUnsigned = 1
					}
					if comparisons != 2 || unsigned != wantUnsigned {
						t.Fatalf("source64=%t/result64=%t/signed=%t/regabi=%t comparisons=%d want=2 unsigned=%d want=%d", sourceWide, resultWide, signed, regABI, comparisons, unsigned, wantUnsigned)
					}
				}
			}
		}
	}
}
