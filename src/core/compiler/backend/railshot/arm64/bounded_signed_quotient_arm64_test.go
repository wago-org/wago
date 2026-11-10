//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func boundedQuotientConst(body []byte, v int32) []byte {
	body = append(body, 0x41)
	for {
		b := byte(v & 127)
		v >>= 7
		if (v == 0 && b&64 == 0) || (v == -1 && b&64 != 0) {
			return append(body, b)
		}
		body = append(body, b|128)
	}
}

func boundedQuotientPacket(body []byte, x byte, lo, hi int32, final byte) []byte {
	body = append(body, 0x22, x)
	body = boundedQuotientConst(body, lo)
	body = append(body, 0x20, x)
	body = boundedQuotientConst(body, lo)
	body = append(body, 0x4a, 0x1b, 0x22, x)
	body = boundedQuotientConst(body, hi)
	body = append(body, 0x20, x)
	body = boundedQuotientConst(body, hi)
	return append(body, 0x48, 0x1b, final, x)
}

func TestBoundedSignedQuotientNative(t *testing.T) {
	basePairs := [][2]int32{{0, 1}, {7, 1}, {-8, 1}, {8, 1}, {-9, 1}, {63, 8}, {64, 8}, {-64, 8}, {-65, 8}, {2147483647, 1}, {-2147483648, 1}, {-2147483648, 2147483647}, {2147483647, 2147483647}, {17, -2}, {-17, -2}, {2147483647, -1}, {-2147483648, -1}, {8, 0}, {-2147483648, 0}}
	for _, bounds := range [][2]int32{{-8, 7}, {-17, 39}, {0, 4095}, {-4095, 0}} {
		pairs := append([][2]int32(nil), basePairs...)
		for _, d := range []int32{2, 17, 4096} {
			for _, bound := range bounds {
				for _, offset := range []int32{-1, 0, 1} {
					pairs = append(pairs, [2]int32{bound*d + offset, d})
				}
			}
		}
		for _, x := range []byte{0, 1, 2} {
			for _, final := range []byte{0x21, 0x22} {
				// Keep an older alias of x on the stack across all three writes.
				body := []byte{1, 1, 0x7f, 0x20, x, 0x20, 0, 0x20, 1, 0x6d}
				body = boundedQuotientPacket(body, x, bounds[0], bounds[1], final)
				if final == 0x22 {
					body = append(body, 0x1a)
				}
				body = append(body, 0x20, x, 0x73, 0xad, 0x0b)
				m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
				for _, enabled := range []bool{false, true} {
					for _, borrowed := range []bool{false, true} {
						for _, guard := range []bool{false, true} {
							for _, pair := range pairs {
								n, d := pair[0], pair[1]
								var stats ModuleStats
								opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"bounded-signed-quotient": enabled, "borrowed-div-rem": borrowed}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, uint64(uint32(n))|0xfeedabcd00000000, uint64(uint32(d))|0xbeeffade00000000)
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["bounded-signed-quotient"] != 0) != enabled {
									t.Fatalf("admission enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
								}
								trap := rt.TrapCode(0)
								if d == 0 {
									trap = rt.TrapDivZero
								} else if n == -2147483648 && d == -1 {
									trap = rt.TrapDivOverflow
								}
								if trap != 0 {
									var te *rt.TrapError
									if !errors.As(err, &te) || te.Code != trap {
										t.Fatalf("n=%d d=%d trap=%v want=%v", n, d, err, trap)
									}
									continue
								}
								q := n / d
								if q < bounds[0] {
									q = bounds[0]
								}
								if q > bounds[1] {
									q = bounds[1]
								}
								old := int32(0)
								if x == 0 {
									old = n
								}
								if x == 1 {
									old = d
								}
								want := uint64(uint32(q ^ old))
								if err != nil || got != want {
									t.Fatalf("bounds=%v x=%d final=%x enabled=%v borrowed=%v guard=%v n=%d d=%d got=%x want=%x err=%v", bounds, x, final, enabled, borrowed, guard, n, d, got, want, err)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestBoundedSignedQuotientDeclinesDifferentFinalLocal(t *testing.T) {
	body := []byte{1, 2, 0x7f, 0x20, 0, 0x20, 1, 0x6d}
	body = boundedQuotientPacket(body, 2, -8, 7, 0x21)
	body[len(body)-1] = 3
	body = append(body, 0x20, 2, 0x20, 3, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	opts := CompileOptions{Optimizations: map[string]bool{"bounded-signed-quotient": true}}
	if diagnosticsEnabled {
		opts.Stats = &stats
	}
	got, err := runArm64WrapperWithOptions(t, m, opts, 100, 1)
	if err != nil || got != 107 {
		t.Fatalf("intermediate local=%d err=%v", got, err)
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["bounded-signed-quotient"] != 0 {
		t.Fatal("discarded observable intermediate local")
	}
}

func TestBoundedSignedQuotientKeepsEarlierTrap(t *testing.T) {
	body := []byte{1, 1, 0x7f, 0x20, 0, 0x41, 0, 0x6d, 0x20, 0, 0x20, 1, 0x6d}
	body = boundedQuotientPacket(body, 2, -8, 7, 0x22)
	body = append(body, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, guard := range []bool{false, true} {
		_, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"bounded-signed-quotient": true}}, 0x80000000, 0xffffffff)
		var te *rt.TrapError
		if !errors.As(err, &te) || te.Code != rt.TrapDivZero {
			t.Fatalf("guard=%v earlier trap=%v", guard, err)
		}
	}
}

func TestBoundedSignedQuotientPressureAndCompaction(t *testing.T) {
	params := make([]wasm.ValType, 16)
	for i := range params {
		params[i] = wasm.I32
	}
	for _, x := range []byte{0, 1, 15} {
		body := []byte{0, 0x20, x, 0x20, 0, 0x20, 1, 0x6d}
		body = boundedQuotientPacket(body, x, -17, 39, 0x22)
		body = append(body, 0x6a)
		for i := byte(0); i < 16; i++ {
			body = append(body, 0x20, i, 0x6a)
		}
		body = append(body, 0xad, 0x0b)
		m := mod1(t, params, []wasm.ValType{wasm.I64}, body)
		for _, compact := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, pair := range [][2]int32{{10000, 3}, {-10000, 3}, {57, 2}, {57, -2}, {-57, -2}, {2147483647, 1}} {
					args := make([]uint64, 16)
					for i := range args {
						args[i] = uint64(i*17+3) | 0xffeeddcc00000000
					}
					args[0], args[1] = uint64(uint32(pair[0]))|0xbeeffeed00000000, uint64(uint32(pair[1]))|0xabcdbeef00000000
					q := pair[0] / pair[1]
					if q < -17 {
						q = -17
					}
					if q > 39 {
						q = 39
					}
					want := uint32(args[x]) + uint32(q)
					for i, v := range args {
						if byte(i) == x {
							v = uint64(uint32(q))
						}
						want += uint32(v)
					}
					var stats ModuleStats
					opts := CompileOptions{CompactNative: compact, ElideBoundsChecks: guard, Optimizations: map[string]bool{"bounded-signed-quotient": true}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, args...)
					if err != nil || got != uint64(want) {
						t.Fatalf("x=%d compact=%v guard=%v pair=%v got=%x want=%x err=%v", x, compact, guard, pair, got, want, err)
					}
					if diagnosticsEnabled && stats.Funcs[0].Peephole["bounded-signed-quotient"] == 0 {
						t.Fatal("missing pressure admission")
					}
				}
			}
		}
	}
}
