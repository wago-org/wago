//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestImmutableIntegerCacheAllowsTerminalMultiResultReturn(t *testing.T) {
	// Eight incoming arguments keep R9/R10 out of the pin pool. Repeated loop
	// updates pin the callee-saved registers, placing body constants in R9/R10.
	// Those same registers carry the fifth and sixth results after the loop.
	params := []wasm.ValType{wasm.I64, wasm.I32, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64}
	results := []wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64, wasm.I64}
	body := []byte{1, 4, 0x7e} // four i64 locals: accumulator and three hot inputs
	for i := byte(0); i < 3; i++ {
		body = append(body, 0x20, 0, 0x42, i+1, 0x7c, 0x21, 9+i)
	}
	body = append(body, 0x03, 0x40)
	for _, c := range []int64{0x123456789abcdef, 0x23456789abcdef1} {
		for i := byte(0); i < 3; i++ {
			body = append(body, 0x20, 8, 0x20, 9+i, 0x42)
			body = append(body, wasmtest.SLEB64(c)...)
			body = append(body, 0x7e, 0x7c, 0x21, 8)
		}
	}
	body = append(body, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b)
	for i := 0; i < 8; i++ {
		body = append(body, 0x20, 8)
	}
	body = append(body, 0x0b)
	m := modFuncs(t, funcDef{params: params, results: results, body: body})
	policy := currentCodegenPolicy()
	hints, sidecar, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := sidecar.viewAt(hints[0], 0)
	sc := newScratch()
	sc.policy = policy
	sc.amd64Features = shared.AMD64ModernBaseline
	_, _, _, err = compileFunc(m, nil, 0, true, false, true, false, false, nil, &h, computeImmutableTableHints(m, hints, policy), nil, false, coreruntime.MaxHostArity, false, false, false, nil, nil, nil, inlineTargetTable{}, sc)
	f := &sc.fnState
	t.Logf("checked=%v shared=%v pins=%x caches=%v error=%v", regallocCheckEnabled, f.scalarSummary.Eligible, f.pinnedLocalMask, f.iconsts[:f.iconstN], err)
	if err != nil {
		t.Fatal(err)
	}
	cachedReturnRegister := false
	for _, cache := range f.iconsts[:f.iconstN] {
		if cache.reg == R9 || cache.reg == R10 {
			cachedReturnRegister = true
		}
	}
	if !cachedReturnRegister {
		t.Fatal("fixture must reserve a result register for an immutable body cache")
	}
}
