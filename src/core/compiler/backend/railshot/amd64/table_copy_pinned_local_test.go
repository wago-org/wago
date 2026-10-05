//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// With only two incoming arguments, the caller can pin its six live integer
// locals in R12-R15/R9/R10 even after its sole helper call is inlined.
func tableCopyPinnedLocalModule(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{1, 4, 0x7e} // accumulator and three unchanged i64 locals
	for i := byte(0); i < 3; i++ {
		body = append(body, 0x20, 0, 0x42, i+1, 0x7c, 0x21, i+3)
	}
	body = append(body, 0x03, 0x40)
	addTerms := func(constant uint64) {
		for i := byte(0); i < 3; i++ {
			body = append(body, 0x20, 2, 0x20, i+3, 0x42)
			body = append(body, wasmtest.SLEB64(int64(constant))...)
			body = append(body, 0x7e, 0x7c, 0x21, 2)
		}
	}
	addTerms(immutableScratchA)
	body = append(body, 0x10, 1)
	addTerms(immutableScratchB)
	body = append(body, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x20, 2)
	for i := byte(0); i < 3; i++ {
		body = append(body, 0x20, i+3, 0x7c)
	}
	body = append(body, 0x0b)
	helper := []byte{0x41, 4, 0x41, 0, 0x41, 2, 0xfc, 14, 0, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 8})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code(helper))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTableCopyPreservesPinnedLocalAMD64(t *testing.T) {
	savedWide, savedInline, savedCallFree := wideLoopIntConstEnabled, inlineEnabled, inlineCallFreeHintsEnabled
	wideLoopIntConstEnabled, inlineEnabled, inlineCallFreeHintsEnabled = false, true, true
	t.Cleanup(func() {
		wideLoopIntConstEnabled, inlineEnabled, inlineCallFreeHintsEnabled = savedWide, savedInline, savedCallFree
	})
	m := tableCopyPinnedLocalModule(t)
	policy := currentCodegenPolicy()
	hints, sidecar, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := sidecar.viewAt(hints[0], 0)
	sc := newScratch()
	sc.policy, sc.amd64Features = policy, shared.AMD64ModernBaseline
	var functionStats CodegenStats
	code, relocs, _, err := compileFunc(m, nil, 0, true, false, true, false, false,
		nil, &h, computeImmutableTableHints(m, hints, policy), nil, false, coreruntime.MaxHostArity,
		false, false, false, nil, nil, &functionStats, buildInlineTargets(m, hints, policy), sc)
	if err != nil {
		t.Fatal(err)
	}
	f := &sc.fnState
	if !f.pinnedLocalMask.has(R9) || f.iconstN != 0 || f.makesCalls || len(relocs) != 0 || f.scalarSummary.Eligible {
		t.Fatalf("fixture must pin R9 with cache disabled and helper inlined in fallback: pins=%x caches=%d calls=%t relocs=%d shared=%t", f.pinnedLocalMask, f.iconstN, f.makesCalls, len(relocs), f.scalarSummary.Eligible)
	}
	if diagnosticsEnabled && (functionStats.Calls["inline"] != 1 || functionStats.Peephole["all-calls-inlined"] != 1 || functionStats.Peephole["wide-loop-int-const"] != 0) {
		t.Fatal("missing inline/cache-disabled diagnostic evidence")
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: optionalTestStats(&stats), AMD64Features: shared.AMD64ModernBaseline, AMD64FeaturesSet: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
	})
	if cm.Entry[0]+len(code) > len(cm.Code) || !bytes.Equal(code, cm.Code[cm.Entry[0]:cm.Entry[0]+len(code)]) {
		t.Fatal("inspected pinned-local caller differs from executed module")
	}
	if diagnosticsEnabled && (stats.Funcs[0].SharedScalar || stats.Funcs[0].Calls["inline"] != 1 || stats.Funcs[0].PinnedLocals != functionStats.PinnedLocals) {
		t.Fatal("module compilation lost inspected fallback/inlining/pins")
	}
	for _, seed := range []uint64{3, 1 << 63, ^uint64(0)} {
		for _, iterations := range []uint64{1, 2, 7} {
			got := executeImmutableScratchInline(t, m, cm, true, seed, iterations)
			sum := 3*seed + 6
			want := iterations*sum*(immutableScratchA+immutableScratchB) + sum
			if got != want {
				t.Fatalf("seed=%x iterations=%d got=%x want=%x", seed, iterations, got, want)
			}
		}
	}
}
