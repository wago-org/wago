//go:build linux && amd64

package amd64

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestWideLocalPins(t *testing.T) {
	// Both loop-carried locals are beyond the old 64-entry score table.
	body := []byte{1, 66, 0x7f, 0x20, 0, 0x21, 66,
		0x02, 0x40, 0x03, 0x40, 0x20, 66, 0x45, 0x0d, 1,
		0x20, 65, 0x20, 66, 0x6a, 0x21, 65,
		0x20, 66, 0x41, 1, 0x6b, 0x21, 66,
		0x0c, 0, 0x0b, 0x0b, 0x20, 65, 0x0b,
	}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), CompactNative: true, Optimizations: map[string]bool{"wide-local-pins": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		wantPins := 0
		if enabled {
			wantPins = 2
		}
		if diagnosticsEnabled {
			if (stats.Funcs[0].Peephole["wide-local-pins"] != 0) != enabled || stats.Funcs[0].PinnedLocals != wantPins {
				t.Fatalf("pins not admitted: %+v", stats.Funcs[0])
			}
		}
		for _, n := range []uint64{0, 1, 10, 100} {
			got := runCompiledAmd64u(t, cm, n)
			if want := n * (n + 1) / 2; got != want {
				t.Fatalf("n=%d got=%d want=%d", n, got, want)
			}
		}
	}
}

func TestWideLocalScoreStorage(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, count := range []int{64, 65, 256, 257} {
			t.Run(fmt.Sprintf("enabled=%t/locals=%d", enabled, count), func(t *testing.T) {
				selection, err := optimizationBindings.ResolveSnapshot(map[string]bool{"wide-local-pins": enabled, "interval-region-pins": false, "wide-loop-int-const": false}, OptimizationSnapshot{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				policy := shared.DefaultCodegenPolicy(selection)
				m := sparseGlobalHintModule(128, []byte{0x03, 0x40, 0x01, 0x0b, 0x0b})
				for i := range m.Code {
					m.Code[i].Locals = wasm.Locals{Runs: []wasm.LocalRun{{Count: uint32(count), Type: wasm.I32}}}
					// Ensure the parallel scan threshold is reached.
					body := make([]byte, 2048)
					for j := range body {
						body[j] = 0x01
					}
					copy(body[len(body)-len(m.Code[i].BodyBytes):], m.Code[i].BodyBytes)
					m.Code[i].BodyBytes = body
				}
				if !parallelHintScanEligible(m, m.GlobalCount(), 4) {
					t.Fatal("fixture does not exercise parallel hints")
				}
				var first []funcHints
				var firstSidecar funcHintSidecar
				for _, workers := range []int{1, 4} {
					hints, sidecar, _, err := computeModuleHintsWithWorkersPolicy(m, m.GlobalCount(), 0, workers, nil, false, policy)
					if err != nil {
						t.Fatal(err)
					}
					wantWide := enabled && count > 64 && count <= maxWideLocalPinsLocals
					wantCount := min(count, 64)
					if wantWide {
						wantCount = count
					}
					for i, h := range hints {
						if h.hasWideLocalScores() != wantWide || len(sidecar.view(h).localScore) != wantCount {
							t.Fatalf("workers=%d function=%d: wide=%t scores=%d, want %t/%d", workers, i, h.hasWideLocalScores(), len(sidecar.view(h).localScore), wantWide, wantCount)
						}
					}
					if workers == 1 {
						first, firstSidecar = hints, sidecar
					} else if !reflect.DeepEqual(first, hints) || !reflect.DeepEqual(firstSidecar, sidecar) {
						t.Fatal("serial and parallel hints differ")
					}
				}
				compact := compactEHLocalScores(first, firstSidecar.localScore)
				for _, h := range first {
					if h.hasWideLocalScores() || retainedLocalScoreCount(h) != min(count, 64) {
						t.Fatal("EH retained wide scores")
					}
				}
				if len(compact) != len(first)*min(count, 64) {
					t.Fatal("wrong compact score length")
				}
			})
		}
	}
}

func TestWideLocalScorePackedFlag(t *testing.T) {
	h := funcHints{gcResolverAndRelocs: gcResolverSiteMask - 1 | uint32(254)<<24}
	h.setWideLocalScores(true)
	h.markNonDirectCall()
	h.markUnsupportedDynamicCall()
	for range 3 {
		h.addGCResolverSite()
		h.addCallRelocSite()
	}
	if !h.hasWideLocalScores() || !h.hasNonDirectCall() || !h.hasUnsupportedDynamicCall() || h.gcResolverSiteCount() != gcResolverSiteMask || h.callRelocSiteCount() != 255 {
		t.Fatalf("packed fields corrupted: %#v", h)
	}
	h.setWideLocalScores(false)
	if h.hasWideLocalScores() || !h.hasNonDirectCall() || !h.hasUnsupportedDynamicCall() || h.gcResolverSiteCount() != gcResolverSiteMask || h.callRelocSiteCount() != 255 {
		t.Fatalf("clearing flag corrupted packed fields: %#v", h)
	}
}

func TestWideFloatLocalPins(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64} {
		localType, add, convert := byte(0x7d), byte(0x92), byte(0xb2)
		if typ == wasm.F64 {
			localType, add, convert = 0x7c, 0xa0, 0xb7
		}
		// Integer counter 64 and floating accumulator 65 both require wide scores.
		body := []byte{2, 64, 0x7f, 1, localType, 0x20, 0, 0x21, 64,
			0x02, 0x40, 0x03, 0x40, 0x20, 64, 0x45, 0x0d, 1,
			0x20, 65, 0x20, 64, convert, add, 0x21, 65,
			0x20, 64, 0x41, 1, 0x6b, 0x21, 64, 0x0c, 0,
			0x0b, 0x0b, 0x20, 65, 0x0b}
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{typ}, body)
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), CompactNative: true, Optimizations: map[string]bool{"wide-local-pins": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled {
				if (stats.Funcs[0].Peephole["wide-local-pins"] != 0) != enabled {
					t.Fatalf("wide pins admission mismatch: %+v", stats.Funcs[0])
				}
			}
			for _, n := range []uint64{0, 1, 10, 100} {
				bits := runCompiledAmd64u(t, cm, n)
				got := math.Float64frombits(bits)
				if typ == wasm.F32 {
					got = float64(math.Float32frombits(uint32(bits)))
				}
				if want := float64(n * (n + 1) / 2); got != want {
					t.Fatalf("type=%v enabled=%t n=%d got=%v want=%v", typ, enabled, n, got, want)
				}
			}
		}
	}
}
