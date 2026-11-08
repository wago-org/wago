//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func remainderChangingDividendModule(t testing.TB, wide, second bool) *wasm.Module {
	typ, valueType, add, xor, rem := wasm.I32, byte(0x7f), byte(0x6a), byte(0x73), byte(0x70)
	if wide {
		typ, valueType, add, xor, rem = wasm.I64, 0x7e, 0x7c, 0x85, 0x82
	}
	body := []byte{1, 1, valueType}
	loop := func(nested bool) {
		body = append(body, 0x03, 0x40)
		if nested {
			body = append(body, 0x02, 0x40, 0x01, 0x0b)
		}
		body = append(body, 0x20, 4, 0x20, 0, 0x20, 1, rem, xor, 0x21, 4, 0x20, 0, 0x20, 2, add, 0x21, 0, 0x20, 3, 0x41, 0x7f, 0x6a, 0x22, 3, 0x0d, 0, 0x0b)
	}
	loop(false)
	if second {
		body = append(body, 0x41, 2, 0x21, 3)
		loop(true)
	}
	body = append(body, 0x20, 4, 0x20, 0, xor, 0x20, 1, xor, 0x20, 2, xor, 0x0b)
	return mod1(t, []wasm.ValType{typ, typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
}

func TestUnsignedRemainderChangingDividendAndWindowLifetime(t *testing.T) {
	old, oldSelection := unsignedRemainderZeroEnabled, unsignedRemainderLoopSelectionEnabled
	t.Cleanup(func() { unsignedRemainderZeroEnabled = old; unsignedRemainderLoopSelectionEnabled = oldSelection })
	unsignedRemainderLoopSelectionEnabled = true
	for _, wide := range []bool{false, true} {
		mask, min := uint64(0xffffffff), uint64(0x80000000)
		if wide {
			mask, min = ^uint64(0), 1<<63
		}
		pairs := [][2]uint64{{0, 0}, {0, 3}, {1, 3}, {2, 3}, {3, 3}, {4, 3}, {mask, mask}, {mask - 1, mask}, {min, mask}, {mask, 2}, {min, 3}, {min, min}}
		for _, second := range []bool{false, true} {
			m := remainderChangingDividendModule(t, wide, second)
			for _, borrowed := range []bool{false, true} {
				for _, pair := range pairs {
					for _, step := range []uint64{0, 1, mask} {
						for _, count := range []uint64{1, 2, 5} {
							total := count
							if second {
								total += 2
							}
							n, d, sum := pair[0], pair[1], uint64(0)
							if d != 0 {
								for i := uint64(0); i < total; i++ {
									sum ^= n % d
									n = (n + step) & mask
								}
							}
							want := (sum ^ n ^ d ^ step) & mask
							for _, enabled := range []bool{false, true} {
								unsignedRemainderZeroEnabled = enabled
								var stats ModuleStats
								admission := diagnosticsEnabled && borrowed && enabled && pair == [2]uint64{2, 3} && step == 1 && count == 5
								options := CompileOptions{Optimizations: map[string]bool{"borrowed-div-rem": borrowed}}
								if admission {
									options.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, options, pair[0], pair[1], step, count)
								if d == 0 {
									var trap *rt.TrapError
									if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
										t.Fatalf("wrong zero-divisor trap: %v", err)
									}
								} else if err != nil || got&mask != want {
									t.Fatalf("wide%t second%t borrowed%t enabled%t pair%x step%x count%d: got%x/%v want%x", wide, second, borrowed, enabled, pair, step, count, got, err, want)
								}
								if admission && stats.Funcs[0].Peephole["unsigned-remainder-zero-quotient"] != 1 {
									t.Fatalf("missing first-window admission or leaked second-window facts: %v", stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestUnsignedRemainderLoopProbeBudgetAndNoMutation(t *testing.T) {
	old, oldSelection := unsignedRemainderZeroEnabled, unsignedRemainderLoopSelectionEnabled
	unsignedRemainderZeroEnabled, unsignedRemainderLoopSelectionEnabled = true, true
	t.Cleanup(func() { unsignedRemainderZeroEnabled = old; unsignedRemainderLoopSelectionEnabled = oldSelection })
	f := &fn{nLocals: 5, ctrl: []ctrlFrame{{kind: cfFunc}}, remainderZeroLoopDepth: 44, remainderZeroLoopWrites: 99}
	flat := []byte{0x20, 0, 0x20, 1, 0x70, 0x21, 4, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0, 0x20, 3, 0x41, 0x7f, 0x6a, 0x22, 3, 0x0d, 0, 0x0b}
	late := append(make([]byte, 24), flat...)
	for i := 0; i < 24; i++ {
		late[i] = 0x01
	}
	oversized := append([]byte{0x20, 0, 0x20, 1, 0x70, 0x1a}, make([]byte, 128)...)
	for i := 6; i < len(oversized); i++ {
		oversized[i] = 0x01
	}
	oversized = append(oversized, 0x0b)
	for _, tc := range []struct {
		name   string
		body   []byte
		accept bool
		writes uint64
	}{{"flat", flat, true, 25}, {"late", late, false, 0}, {"budget", oversized, false, 0}, {"nested", []byte{0x02, 0x40, 0x0b, 0x0b}, false, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			r := wasm.ReaderFrom(tc.body)
			writes, ok := f.inspectUnsignedRemainderLoop(&r)
			if ok != tc.accept || writes != tc.writes || r.Offset() != 0 || f.remainderZeroLoopDepth != 44 || f.remainderZeroLoopWrites != 99 {
				t.Fatalf("probe ok%t writes%x offset%d depth%d state%x", ok, writes, r.Offset(), f.remainderZeroLoopDepth, f.remainderZeroLoopWrites)
			}
			if allocs := testing.AllocsPerRun(100, func() { f.inspectUnsignedRemainderLoop(&r) }); allocs != 0 {
				t.Fatalf("probe allocations%v", allocs)
			}
		})
	}
}
