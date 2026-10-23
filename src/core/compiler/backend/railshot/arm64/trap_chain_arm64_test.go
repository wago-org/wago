//go:build arm64

package arm64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestMemoryTrapChainMatchesSiteTableArm64(t *testing.T) {
	for _, count := range []int{0, 1, 2, 257} {
		for _, foreign := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				t.Run(fmt.Sprintf("count=%d/foreign=%v/wide=%v", count, foreign, wide), func(t *testing.T) {
					emit := func(link bool) []byte {
						a := &a64.Asm{}
						sc := &scratch{asm: a}
						f := &fn{a: a, sc: sc, stats: &CodegenStats{}, policy: currentCodegenPolicy(), rootTraceFuncIdx: 7, traceFuncIdx: 7, linkMemoryTraps: link}
						if wide {
							a.B = make([]byte, 1<<20)
						}
						for i := 0; i < count; i++ {
							f.wasmPC = uint32(i + 5)
							if foreign && i == count/2 {
								f.traceFuncIdx = 9
								// Keep this foreign group a singleton; its precise PC
								// must survive conversion from the pending chain.
							}
							switch i % 3 {
							case 0:
								f.trapIf(a64.CondEQ, trapMemOOB)
							case 1:
								f.trapIfZero(X9, true, false, trapMemOOB)
							case 2:
								f.trapAlways(trapMemOOB)
							}
							f.traceFuncIdx = 7
						}
						if link && !foreign && !wide && len(sc.trapSites[trapMemOOB]) != 0 {
							t.Fatal("ordinary trap chain allocated site records")
						}
						f.emitTrapStubs()
						return a.B
					}
					if !bytes.Equal(emit(false), emit(true)) {
						t.Fatal("linked traps changed finalized instructions or source attribution")
					}
				})
			}
		}
	}
}

func TestMemoryTrapChainSharedBodiesMatchSiteTableArm64(t *testing.T) {
	for _, sharedBody := range []bool{false, true} {
		for _, count := range []int{0, 1, 2, 257} {
			for _, foreign := range []bool{false, true} {
				t.Run(fmt.Sprintf("shared=%v/count=%d/foreign=%v", sharedBody, count, foreign), func(t *testing.T) {
					selection, err := optimizationBindings.ResolveSnapshot(map[string]bool{"shared-trap-body": sharedBody}, OptimizationSnapshot{}, nil)
					if err != nil {
						t.Fatal(err)
					}
					emit := func(link, compact bool) []byte {
						a := &a64.Asm{}
						sc := &scratch{asm: a}
						policy := shared.CompactCodegenPolicy(selection)
						policy.CompactNative = compact
						f := &fn{a: a, sc: sc, stats: &CodegenStats{}, policy: policy, rootTraceFuncIdx: 7, traceFuncIdx: 7, linkMemoryTraps: link}
						// A second trap family exercises shared bodies and unwind tails.
						f.trapIf(a64.CondEQ, trapStackFence)
						for i := 0; i < count; i++ {
							f.wasmPC = uint32(i + 5)
							if foreign && i == count/2 {
								f.traceFuncIdx = 9
							}
							switch i % 3 {
							case 0:
								f.trapIf(a64.CondEQ, trapMemOOB)
							case 1:
								f.trapIfZero(X9, true, false, trapMemOOB)
							case 2:
								f.trapAlways(trapMemOOB)
							}
							f.traceFuncIdx = 7
						}
						if link && !foreign && len(sc.trapSites[trapMemOOB]) != 0 {
							t.Fatal("compact trap chain allocated site records")
						}
						f.emitTrapStubs()
						if _, err := f.finalizeNativeCode(0); err != nil {
							t.Fatal(err)
						}
						return a.B
					}
					for _, compact := range []bool{false, true} {
						if !bytes.Equal(emit(false, compact), emit(true, compact)) {
							t.Fatalf("compact=%v linked traps changed finalized code or attribution", compact)
						}
					}
				})
			}
		}
	}
}
