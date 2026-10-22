//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailDispatcherSharesFrozenOwners(t *testing.T) {
	sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}
	f := tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
	owner := tailDynamicOwnerForTest(t, []tailRAFunction{f})
	f = owner.functions[0]
	first := tailDynamicSiteForTest(t, f, []tailRADynamicOwner{owner}, f.info.FrameBytes-8)
	second := tailDynamicSiteForTest(t, f, []tailRADynamicOwner{owner}, f.info.FrameBytes-8)
	second.Parameters, second.Results = []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F64}
	second.dynamic.key = wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: second.Parameters, Results: second.Results})
	distinct := owner
	distinct.functions = append([]tailRAFunction(nil), owner.functions...)
	third := tailDynamicSiteForTest(t, f, []tailRADynamicOwner{distinct}, f.info.FrameBytes-8)
	dispatcher, err := NewNativeTailDispatcherDetached([]NativeTailSite{first, second, third}, len(f.snapshot))
	if dispatcher != nil {
		t.Cleanup(func() {
			if err := dispatcher.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := dispatcher.dynamic[0].site, dispatcher.dynamic[1].site, dispatcher.dynamic[2].site
	if unsafe.SliceData(a.owners) != unsafe.SliceData(b.owners) || unsafe.SliceData(a.owners) == unsafe.SliceData(c.owners) ||
		unsafe.SliceData(a.owners[0].functions) == unsafe.SliceData(owner.functions) || a.key == b.key {
		t.Fatal("owner sharing lost input identity, immutable copy, or site-local key")
	}
	first.dynamic.key ^= 1
	second.dynamic.referenceBase = 0
	owner.functions[0].signature.Params[0] = wasm.F64
	if a.key != wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.F32}}) ||
		b.referenceBase != f.info.FrameBytes-8 || !wasm.EqualValType(a.owners[0].functions[0].signature.Params[0], wasm.I32) ||
		!wasm.EqualValType(b.owners[0].functions[0].signature.Params[0], wasm.I32) {
		t.Fatal("caller mutation changed shared frozen metadata")
	}
}
