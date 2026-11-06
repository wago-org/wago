//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestScalarBranchLoopRetainsDivisionTrapsInEveryCopy(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, sub, ge, div, ne := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6b), byte(0x4f), byte(0x6e), byte(0x47)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, sub, ge, div, ne = wasm.I64, 0x7e, 0x42, 0x7c, 0x7d, 0x5a, 0x80, 0x52
			fold = func(x uint64) uint64 { return x }
		}
		body := []byte{1, 3, bt, 0x02, 0x40, 0x03, 0x40,
			0x20, 3, 0x20, 0, ge, 0x0d, 1,
			0x20, 1, 0x20, 2, div, 0x21, 5,
			0x20, 4, 0x20, 5, k, 0, ne}
		if wide {
			body = append(body, 0xad)
		}
		body = append(body, add, 0x21, 4, 0x20, 5)
		if wide {
			body = append(body, 0xa7)
		}
		body = append(body, 0x41, 1, 0x71, 0x04, 0x40,
			0x20, 4, k, 3, add, 0x21, 4, 0x0b,
			0x20, 2, k, 1, sub, 0x21, 2,
			0x20, 3, k, 1, add, 0x21, 3, 0x0c, 0,
			0x0b, 0x0b, 0x20, 4, 0x0b)
		params, result := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
		m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
		for _, borrowed := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for bound := uint64(0); bound < 11; bound++ {
						for initialDivisor := uint64(0); initialDivisor < 12; initialDivisor++ {
							for _, input := range []uint64{7, ^uint64(0)} {
								n, d, want := fold(input), initialDivisor, uint64(0)
								traps := false
								for range bound {
									if d == 0 {
										traps = true
										break
									}
									q := n / d
									if q != 0 {
										want = fold(want + 1)
									}
									if q&1 != 0 {
										want = fold(want + 3)
									}
									d = fold(d - 1)
								}
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "borrowed-div-rem": borrowed, "value-facts": false}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								a, b, c := bound, input, initialDivisor
								if !wide {
									a |= 0xdeadbeef00000000
									b |= 0xcafebabe00000000
									c |= 0xa5a5a5a500000000
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, a, b, c)
								var trap *rt.TrapError
								if traps {
									if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
										t.Fatalf("wide=%t borrowed=%t enabled=%t workers=%d bound=%d d=%d err=%v want division by zero", wide, borrowed, enabled, workers, bound, initialDivisor, err)
									}
								} else if err != nil || got != want {
									t.Fatalf("wide=%t borrowed=%t enabled=%t workers=%d bound=%d d=%d got=%x err=%v want=%x", wide, borrowed, enabled, workers, bound, initialDivisor, got, err, want)
								}
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-div-trap"] != 0) != (enabled && !profileEnabled) {
									t.Fatalf("division-loop admission wide=%t borrowed=%t enabled=%t stats=%v", wide, borrowed, enabled, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestScalarBranchLoopCopiesOriginalDivisionTrapIdentity(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	f := branchLoopProofFixture()
	wrWord(f.a.B, 32, 0x1a9f07e9) // CSET W9,NE
	wrWord(f.a.B, 36, 0x34000001) // unresolved CBZ W1
	f.sc.trapSites[trapDivZero] = []trapSite{{branch: 36, function: 7, pc: 113}}
	if !f.unrollScalarBranches(&f.ctrl[1]) {
		t.Fatal("proved conditional division trap rejected")
	}
	sites := f.sc.trapSites[trapDivZero]
	if len(sites) != 4 {
		t.Fatalf("trap sites=%v want four", sites)
	}
	for phase, site := range sites {
		if site.branch != uint32(36+40*phase) || site.function != 7 || site.pc != 113 {
			t.Fatalf("copy %d changed the trap identity: %+v", phase, site)
		}
	}
}

func TestScalarBranchLoopRejectsUnprovedTrapKinds(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	for _, code := range []uint32{trapDivOverflow, trapMemOOB, trapInterrupted} {
		f := branchLoopProofFixture()
		wrWord(f.a.B, 36, 0x34000001)
		f.sc.trapSites[code] = []trapSite{{branch: 36}}
		if f.unrollScalarBranches(&f.ctrl[1]) || len(f.a.B) != 40 || len(f.sc.trapSites[code]) != 1 {
			t.Fatalf("unproved trap kind %d admitted or modified", code)
		}
	}
}
