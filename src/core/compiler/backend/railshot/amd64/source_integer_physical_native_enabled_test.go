//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

func TestNativeSourceIntegerPhysicalRealDAG(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, workers := range []int{1, 2} {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			for _, op := range []byte{0x71, 0x72, 0x73} {
				second := byte(0x73)
				if op == 0x73 {
					second = 0x6a
				}
				if typ == wasm.I64 {
					op += 0x12
					second += 0x12
				}
				m := mod1(t, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0x20, 2, second, 0x0b})
				r := nativeSourceMaterializationCompile(t, m, workers)
				if len(r) != 2 || r[1].Result.Verdict != regalloccheck.Inconclusive || !strings.Contains(r[1].Result.Message, "whole-function integer byte/provenance proof complete; final-module coverage pending") {
					t.Fatalf("workers=%d type=%v op=%x reports=%+v", workers, typ, op, r)
				}
			}
		}
	}
}
