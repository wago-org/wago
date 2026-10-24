//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A crowded operand stack holds independent products before its reduction.
// Exercise full-width overflow, both bounds paths and the rollback path.
func TestCrowdedProductsIndependentExecution(t *testing.T) {
	old := crowdedProductsEnabled
	defer func() { crowdedProductsEnabled = old }()
	for _, wide := range []bool{false, true} {
		m := crowdedProductModule(t, wide)
		for _, enabled := range []bool{false, true} {
			crowdedProductsEnabled = enabled
			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 1, 17, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, x)
					want := x * 136
					if !wide {
						got, want = uint64(uint32(got)), uint64(uint32(want))
					}
					if err != nil || got != want {
						t.Fatalf("wide=%v enabled=%v guard=%v x=%x got=%x want=%x err=%v", wide, enabled, guard, x, got, want, err)
					}
				}
			}
		}
	}
}

func crowdedProductModule(t *testing.T, wide bool) *wasm.Module {
	typ, constant, store, load, mul, add, align, step := wasm.I32, byte(0x41), byte(0x36), byte(0x28), byte(0x6c), byte(0x6a), byte(2), byte(4)
	if wide {
		typ, constant, store, load, mul, add, align, step = wasm.I64, 0x42, 0x37, 0x29, 0x7e, 0x7c, 3, 8
	}
	body := []byte{0}
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x41, 0, constant, i+1, store, align, i*step)
	}
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x20, 0, 0x41, 0, load, align, i*step, mul)
	}
	for i := 0; i < 15; i++ {
		body = append(body, add)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
}

func TestCrowdedProductsAdmissionAndRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := crowdedProductsEnabled
	defer func() { crowdedProductsEnabled = old }()
	m := crowdedProductModule(t, false)
	for _, enabled := range []bool{false, true} {
		crowdedProductsEnabled = enabled
		stats := compileWithStats(t, m, true).Funcs[0]
		got := stats.Peephole["crowded-product-finish"]
		if enabled && got == 0 || !enabled && got != 0 {
			t.Fatalf("enabled=%v admissions=%d", enabled, got)
		}
	}
	crowdedProductsEnabled = true
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Stats: stats, Optimizations: map[string]bool{"crowded-products": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		cm.CodeImage.Close()
	}
	if stats.Funcs[0].Peephole["crowded-product-finish"] != 0 {
		t.Fatal("policy rollback ignored")
	}
}
