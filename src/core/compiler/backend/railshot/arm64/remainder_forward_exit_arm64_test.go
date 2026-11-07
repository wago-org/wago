//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestRemainderForwardExitResultsAndTrapIdentity(t *testing.T) {
	old, oldZero := remainderForwardExitEnabled, normalZeroBranchEnabled
	t.Cleanup(func() { remainderForwardExitEnabled = old; normalZeroBranchEnabled = oldZero })
	hits := 0
	for _, guarded := range []bool{false, true} {
		body := []byte{0, 0x3f, 0, 0x1a}
		if guarded {
			body = append(body, 0x02, 0x40, 0x20, 1, 0x45, 0x0d, 0)
		}
		body = append(body, 0x03, 0x40, 0x20, 0, 0x20, 1, 0x22, 0, 0x70, 0x22, 1, 0x0d, 0, 0x0b)
		if guarded {
			body = append(body, 0x0b)
		}
		body = append(body, 0x20, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, normal := range []bool{false, true} {
			normalZeroBranchEnabled = normal
			for _, compact := range []bool{false, true} {
				for _, fold := range []bool{false, true} {
					for _, pair := range [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {48, 18}, {13, 8}, {55, 34}, {65535, 65534}, {0xffffffff, 17}, {0xa5a5000000000037, 0xdead000000000022}, {19, 1 << 32}, {^uint64(0), ^uint64(0) - 1}} {
						a, b := uint32(pair[0]), uint32(pair[1])
						traps := !guarded && b == 0
						for b != 0 {
							a, b = b, a%b
						}
						var previous rt.TrapFrame
						for _, enabled := range []bool{false, true} {
							remainderForwardExitEnabled = enabled
							var stats ModuleStats
							opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"branch-fold": fold}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, pair[:]...)
							if traps {
								var trap *rt.TrapError
								if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter {
									t.Fatalf("missing precise division trap: %v", err)
								}
								if !enabled {
									previous = trap.Frames[0]
								} else if trap.Frames[0] != previous {
									t.Fatalf("trap frame=%+v want=%+v", trap.Frames[0], previous)
								}
							} else if err != nil || uint32(got) != a {
								t.Fatalf("guarded=%v normal=%v compact=%v fold=%v enabled=%v pair=%x got=%x want=%x err=%v", guarded, normal, compact, fold, enabled, pair, got, a, err)
							}
							if diagnosticsEnabled {
								selected := stats.Funcs[0].Peephole["remainder-forward-exit"]
								if (selected != 0) != (enabled && normal && fold && !compact) {
									t.Fatalf("unexpected selection normal=%v compact=%v fold=%v enabled=%v stats=%v", normal, compact, fold, enabled, stats.Funcs[0].Peephole)
								}
								hits += selected
							}
						}
					}
				}
			}
		}
	}
	if diagnosticsEnabled && hits == 0 {
		t.Fatal("forward remainder exit was never selected")
	}
}
