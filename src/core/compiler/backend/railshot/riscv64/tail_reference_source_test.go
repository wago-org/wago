//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTailReferenceSourceDeclaredTypesAndDeadBudget(t *testing.T) {
	for _, force := range []bool{false, true} {
		params := []wasm.ValType{wasm.I64, wasm.I32, wasm.F32, wasm.F64}
		if force {
			params = append(params, wasm.V128)
		}
		body := dynamicTailCountdownBody(len(params) - 1)
		body = append(body[:len(body)-7], 0xd2, 0, 0x15, 2, 0x0b, 0x0b)
		source := dynamicTailTestSource(params, params[1:], body)
		i, err := compileDynamicTailSource(source, 11, force)
		if err != nil {
			t.Fatal(force, err)
		}
		if len(i.plans) != 1 || i.plans[0].kind != 2 || i.plans[0].typeIndex != 2 || i.plans[0].site != 11 || i.plans[0].referenceBase < i.plans[0].sourceBase+len(params)*i.plans[0].sourceStride || i.plans[0].referenceBase > i.functions[0].FrameBytes-8 {
			t.Fatal("typed reference source plan", force, i.plans)
		}
		if !wasm.FuncTypeEqual(i.types[2], i.signatures[0]) || wasm.FuncTypeEqual(i.types[1], i.signatures[0]) {
			t.Fatal("type namespace collapsed into function index")
		}
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if image, err := CompileModuleWith(m, localTailOptions()); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("public reference tail admitted", err)
		}
		if image, err := stageProductionModule(&Compiled{code: i.code}, CompileOptions{NativeTailCalls: true, tailPlans: &i.plans}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("reference plan reached artifact staging", err)
		}
		for _, count := range []int{1024, 1025} {
			body := []byte{0}
			for n := 0; n < count; n++ {
				body = append(body, 0x15, 2)
			}
			body = append(body, 0x0b)
			i, err := compileDynamicTailSource(dynamicTailTestSource(nil, nil, body), 0, force)
			if count == 1024 {
				if err != nil || i == nil || len(i.plans) != 0 {
					t.Fatal("bounded dead reference sites", force, err)
				}
			} else if i != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("dead reference site limit", force, err)
			}
		}
	}
}

func TestTailReferenceSourceInvalidDeclaredTypeAndHeap(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			body []byte
		}{
			{"call-type-at-count", []byte{0xd2, 0, 0x15, 3, 0x0b}},
			{"heap-at-count", []byte{0xd0, 3, 0x15, 2, 0x0b}},
			{"negative-nonfunction-heap", []byte{0xd0, 0x6f, 0x15, 2, 0x0b}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if image, err := compileDynamicTailSource(dynamicTailTestSource(nil, nil, tc.body), 0, force); image != nil || err == nil {
					t.Fatal("invalid typed source admitted", force, err)
				}
			})
		}
	}
}
