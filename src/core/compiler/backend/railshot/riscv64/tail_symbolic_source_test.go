//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestTailSymbolicSourcePreservesDeclaredTypeAndReferenceHome(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, kind := range []uint32{enc.NativeTailIndirect, enc.NativeTailReference} {
			params := []wasm.ValType{wasm.I64, wasm.I32, wasm.F32, wasm.F64}
			if force {
				params = append(params, wasm.V128)
			}
			body := dynamicTailCountdownBody(len(params) - 1)
			if kind == enc.NativeTailReference {
				body = append(body[:len(body)-7], 0xd2, 0, 0x15, 2, 0x0b, 0x0b)
			}
			i, err := compileDynamicTailSource(dynamicTailTestSource(params, params[1:], body), 0, force)
			if err != nil {
				t.Fatal(err)
			}
			m, err := i.symbolicTailPlans()
			if err != nil || len(m.Plans) != 1 {
				t.Fatal("symbolic staging", force, kind, err)
			}
			p, s := m.Plans[0], i.plans[0]
			if p.Kind != kind || p.TypeIndex != 2 || p.FunctionIndex != 1 || p.ImportIndex != 0 || p.SourcePC != s.sourcePC || p.FrameBytes != i.functions[0].FrameBytes || p.SourceBase != s.sourceBase || p.SourceStride != s.sourceStride || p.ReferenceBase != s.referenceBase {
				t.Fatal("source symbolic identity", p, s)
			}
			original := i.plans[0]
			for _, mutate := range []func(){
				func() { i.siteBase = 1 },
				func() { i.plans[0].site = 1 },
				func() { i.plans[0].typeIndex = uint32(len(i.types)) },
				func() { i.plans[0].referenceBase = s.sourceBase + len(params)*s.sourceStride - 8 },
				func() { i.plans[0].referenceBase = i.functions[0].FrameBytes },
				func() { i.plans[0].sourceBase++ },
				func() { i.plans[0].kind = 3 },
			} {
				i.siteBase, i.plans[0] = 0, original
				mutate()
				if m, err := i.symbolicTailPlans(); m != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("invalid symbolic image accepted", force, kind, err)
				}
			}
		}
	}
}
