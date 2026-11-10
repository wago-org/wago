//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"testing"
)

func regionalAddressFixture(t testing.TB, overwrite bool) *wasm.Module {
	body := []byte{1, 32, 0x7f, 0x20, 0, 0x21, 2, 0x41, 0, 0x21, 3}
	for x := byte(4); x < 34; x++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(x))...)
		body = append(body, 0x21, x)
	}
	// Descending offsets: the first explicit proof covers every later store.
	for i := 7; i >= 0; i-- {
		body = append(body, 0x20, 2, 0x42, byte(i+1), 0x37, 3, byte(i*8))
	}
	for i := 0; i < 8; i++ {
		body = append(body, 0x20, 3, 0x20, 2, 0x28, 2, byte(i*8), 0x6a, 0x21, 3)
	}
	for x := byte(4); x < 34; x++ {
		body = append(body, 0x20, 3, 0x20, x, 0x6a, 0x21, 3)
	}
	// Keep an older address and a deferred load alive across source overwrite.
	body = append(body, 0x20, 3, 0x20, 2, 0x20, 2, 0x28, 2, 0)
	if overwrite {
		body = append(body, 0x20, 1, 0x21, 2)
	}
	body = append(body, 0x6a, 0x6a)
	if overwrite {
		body = append(body, 0x20, 2, 0x6a)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestRegionalMemoryReadPressureAliasesAndBounds(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		m := regionalAddressFixture(t, overwrite)
		for _, enabled := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, address := range []uint64{0, 3, 32768, 65472, 65473, 0xfffffff0} {
					var stats ModuleStats
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"regional-memory-read": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					if os.Getenv("WAGO_REGIONAL_MEMORY_DEBUG") == "1" && !overwrite && !guard && address == 0 {
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(map[bool]string{false: "/tmp/regional-memory-fixture-off.bin", true: "/tmp/regional-memory-fixture-on.bin"}[enabled], cm.Code, 0600); err != nil {
							t.Fatal(err)
						}
						t.Logf("entry=%v stats=%+v", cm.Entry, stats.Funcs[0])
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, address, 4096)
					if address > 65472 {
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != rt.TrapLinMemOutOfBounds {
							t.Fatalf("on=%v guard=%v address=%x trap=%v", enabled, guard, address, err)
						}
					} else {
						want := uint64(36+555+1) + address
						if overwrite {
							want += 4096
						}
						if err != nil || got != want {
							t.Fatalf("overwrite=%v on=%v guard=%v address=%x got=%d want=%d err=%v", overwrite, enabled, guard, address, got, want, err)
						}
					}
					if diagnosticsEnabled {
						if (stats.Funcs[0].Peephole["regional-memory-read"] != 0) != enabled {
							t.Fatalf("missing loan admission %+v", stats.Funcs[0].Peephole)
						}
						wantProofs := 1
						if !guard && !overwrite {
							wantProofs = 2
						} // final owned address transfer needs its own explicit proof
						if enabled && stats.Funcs[0].BoundsChecks != wantProofs {
							t.Fatalf("overwrite=%v guard=%v bounds proofs=%d want%d", overwrite, guard, stats.Funcs[0].BoundsChecks, wantProofs)
						}
					}
				}
			}
		}
	}
}
