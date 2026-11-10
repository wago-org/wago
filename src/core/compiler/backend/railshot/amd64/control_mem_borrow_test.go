//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestControlIntervalPreservesTrapPrecedence(t *testing.T) {
	for _, loads := range []int{1, 2} {
		for _, pressure := range []int{0, 4, 5, 6, 7} {
			for _, copies := range []int{0, 20} {
				for _, load := range []struct{ op, align byte }{{0x28, 2}, {0x29, 3}, {0x2a, 2}, {0x2b, 3}, {0x2d, 0}} {
					// Hot loop locals65/66 own permanent pins. Local67 supplies a
					// distinct out-of-bounds address through a temporary regional lease.
					body := []byte{1, 80, 0x7f, 0x41, 0x80, 0x80, 4, 0x21, 67, 0x20, 67, 0x1a, 0x41, 0, 0x21, 65, 0x41, 1, 0x21, 66, 0x03, 0x40}
					for range 64 {
						body = append(body, 0x20, 65, 0x20, 66, 0x6a, 0x21, 65)
					}
					body = append(body, 0x0b)
					// Eager FP conversions leave owned GP values below both traps.
					for range pressure {
						body = append(body, 0x20, 65, 0xb8, 0xbd)
					}
					body = append(body, 0x41, 9, 0x20, 0, 0x6e)
					for range copies {
						body = append(body, 0x20, 67)
					}
					body = append(body, 0x20, 67, load.op, load.align, 0)
					if loads == 2 {
						body = append(body, 0x41, 0, load.op, load.align, 0)
					}
					body = append(body, 0x02, 0x40, 0x0b)
					for range loads + copies {
						body = append(body, 0x1a)
					}
					body = append(body, 0x21, 68)
					for range pressure {
						body = append(body, 0x1a)
					}
					body = append(body, 0x20, 68, 0x0b)
					m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
					m.Memories = modMem(t, 1, nil, nil, []byte{0, 0x0b}).Memories
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, ElideBoundsChecks: true})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
					st := stats.Funcs[0]
					if st.Peephole["interval-control"] == 0 {
						t.Fatal("missing regional admission")
					}
					key := "interval-address-detach"
					if loads == 2 {
						key = "interval-address-flush"
					}
					// Greater pressure can evict the lease before the deferred load exists.
					if pressure <= 4 && st.Peephole[key] == 0 {
						t.Fatalf("loads=%d pressure=%d copies=%d op=%x missing %s: %+v", loads, pressure, copies, load.op, key, st.Peephole)
					}
					for _, denom := range []uint32{0, 1} {
						err := callStoreValueGuarded(t, m, denom)
						want := wruntime.TrapDivZero
						if denom != 0 {
							want = wruntime.TrapLinMemOutOfBounds
						}
						var trap *wruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != want {
							t.Fatalf("loads=%d pressure=%d copies=%d op=%x denom=%d got=%v want=%v", loads, pressure, copies, load.op, denom, err, want)
						}
					}
				}
			}
		}
	}
}
