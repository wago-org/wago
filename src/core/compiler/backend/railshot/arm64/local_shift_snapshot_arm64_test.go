//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLocalShiftSnapshotPreservesOldAndNewValues(t *testing.T) {
	initial := localShiftSnapshotEnabled
	defer func() { localShiftSnapshotEnabled = initial }()
	hits := uint64(0)
	var concreteConsumerHits [2]uint64
	for _, wide := range []bool{false, true} {
		typ, c, add := wasm.I32, byte(0x41), byte(0x6a)
		if wide {
			typ, c, add = wasm.I64, 0x42, 0x7c
		}
		for _, offset := range []byte{0, 1, 7, 8, 9} {
			for _, shiftOffset := range []byte{10, 11, 12} {
				for _, count := range []int32{-1, 0, 1, 13, 31, 32, 63, 64} {
					for _, shiftFirst := range []bool{false, true} {
						for _, suffix := range []int{0, 1, 2, 3, 4} {
							// The first x is a separate old-value capture. The two x operands in
							// the pending shifted expression must also survive x's mutation.
							body := []byte{1, 1, 0x7f - (c - 0x41), 0x03, 0x40, 0x20, 0}
							if suffix == 4 {
								body = append(body, 0x20, 1)
							}
							body = append(body, 0x20, 0)
							if !shiftFirst {
								body = append(body, 0x20, 0)
							}
							body = append(body, c)
							body = append(body, wasmtest.SLEB32(count)...)
							body = append(body, add+shiftOffset)
							if shiftFirst {
								body = append(body, 0x20, 0)
							}
							body = append(body, add+offset)
							if suffix > 0 {
								body = append(body, c, 0xff, 0x01, add+7)
							}
							if suffix == 2 {
								body = append(body, c, 3, add+2)
							}
							if suffix == 3 {
								body = append(body, 0x20, 1, add+1)
							}
							if suffix == 4 {
								body = append(body, add+1)
							}
							body = append(body, 0x20, 0, c, 1, add, 0x21, 0, add+9, 0x20, 0, add+9, 0x20, 1, add+9, 0x21, 3, 0x20, 2, 0x41, 1, 0x6b, 0x22, 2, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
							m := mod1(t, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
							for _, enabled := range []bool{false, true} {
								old := localShiftSnapshotEnabled
								localShiftSnapshotEnabled = enabled
								for _, input := range []uint64{0, 1, 0xdeadbeef80000001, ^uint64(0)} {
									a, k := input, uint32(count)&63
									if !wide {
										a = uint64(uint32(a))
										k &= 31
									}
									var shifted uint64
									switch shiftOffset {
									case 10:
										shifted = a << k
									case 11:
										if wide {
											shifted = uint64(int64(a) >> k)
										} else {
											shifted = uint64(uint32(int32(a) >> k))
										}
									case 12:
										shifted = a >> k
									}
									v := uint64(0)
									switch offset {
									case 0:
										v = a + shifted
									case 1:
										v = a - shifted
										if shiftFirst {
											v = shifted - a
										}
									case 7:
										v = a & shifted
									case 8:
										v = a | shifted
									case 9:
										v = a ^ shifted
									}
									if suffix > 0 {
										v &= 255
									}
									if suffix == 2 {
										v *= 3
									}
									other := input ^ 0x87654321
									if suffix == 3 {
										v -= other
									}
									if suffix == 4 {
										v = other - v
									}
									want := v ^ a ^ (a + 1) ^ other
									if !wide {
										want = uint64(uint32(want))
									}
									var stats ModuleStats
									opts := CompileOptions{}
									if diagnosticsEnabled {
										opts.Stats = &stats
									}
									got, err := runArm64WrapperWithOptions(t, m, opts, input, other, 1)
									if err != nil || got != want {
										t.Fatalf("wide=%v op=%d shift=%d count=%d enabled=%v input=%x got=%x want=%x err=%v", wide, offset, shiftOffset, count, enabled, input, got, want, err)
									}
									if diagnosticsEnabled {
										n := stats.Funcs[0].Peephole["local-shift-snapshot"]
										if enabled && !wide && count == 13 && shiftOffset == 12 && offset == 9 && suffix == 3 && input == 1 {
											t.Logf("capture stats %v", stats.Funcs[0].Peephole)
										}
										if !enabled && n != 0 {
											t.Fatal("disabled cover selected")
										}
										if enabled && count == 13 && offset == 9 && suffix >= 3 && n != 0 && stats.Funcs[0].Peephole["local-shift-snapshot-consumer"] == 0 {
											t.Fatal("concrete sibling consumer not captured")
										}
										hits += uint64(n)
										if enabled && suffix >= 3 {
											concreteConsumerHits[suffix-3] += uint64(stats.Funcs[0].Peephole["local-shift-snapshot-consumer"])
										}
									}
								}
								localShiftSnapshotEnabled = old
							}
						}
					}
				}
			}
		}
	}
	if diagnosticsEnabled && hits == 0 {
		t.Fatal("snapshot cover never exercised")
	}
	if diagnosticsEnabled {
		for order, n := range concreteConsumerHits {
			if n == 0 {
				t.Fatalf("concrete consumer order %d never exercised", order)
			}
		}
	}
}

func TestLocalShiftSnapshotPreservesHighPressureFallback(t *testing.T) {
	old := localShiftSnapshotEnabled
	defer func() { localShiftSnapshotEnabled = old }()
	params := make([]wasm.ValType, 14)
	for i := range params {
		params[i] = wasm.I32
	}
	body := []byte{0, 0x03, 0x7f, 0x20, 0, 0x20, 0, 0x20, 0, 0x41, 13, 0x76, 0x73, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0, 0x73}
	for i := range params {
		body = append(body, 0x20, byte(i), 0x73)
	}
	body = append(body, 0x0b, 0x0b)
	m := mod1(t, params, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		localShiftSnapshotEnabled = enabled
		for _, seed := range []uint32{0, 7, 0x80000001, 0xffffffff} {
			args := make([]uint64, len(params))
			for i := range args {
				args[i] = uint64(seed+uint32(i)*0x87654321) | 0xdeadbeef00000000
			}
			a := uint32(args[0])
			want := a ^ (a ^ (a >> 13))
			for i, arg := range args {
				if i == 0 {
					want ^= a + 1
				} else {
					want ^= uint32(arg)
				}
			}
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, args...)
			if err != nil || got != uint64(want) {
				t.Fatalf("enabled=%v seed=%x got=%x want=%x err=%v", enabled, seed, got, want, err)
			}
			if diagnosticsEnabled && enabled {
				if stats.Funcs[0].Peephole["local-shift-snapshot-pressure"] == 0 {
					t.Fatalf("pressure fallback not exercised: %v", stats.Funcs[0].Peephole)
				}
				if stats.Funcs[0].Peephole["local-shift-snapshot"] != 0 {
					t.Fatal("high-pressure capture admitted")
				}
			}
		}
	}
}
