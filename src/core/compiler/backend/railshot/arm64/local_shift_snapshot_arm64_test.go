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
							body := []byte{0, 0x03, 0x7f - (c - 0x41), 0x20, 0}
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
							body = append(body, 0x20, 0, c, 1, add, 0x21, 0, add+9, 0x20, 0, add+9, 0x20, 1, add+9, 0x0b, 0x0b)
							m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
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
									got, err := runArm64WrapperWithOptions(t, m, opts, input, other)
									if err != nil || got != want {
										t.Fatalf("wide=%v op=%d shift=%d count=%d enabled=%v input=%x got=%x want=%x err=%v", wide, offset, shiftOffset, count, enabled, input, got, want, err)
									}
									if diagnosticsEnabled {
										n := stats.Funcs[0].Peephole["local-shift-snapshot"]
										if !enabled && n != 0 {
											t.Fatal("disabled cover selected")
										}
										if enabled && count == 13 && offset == 9 && suffix >= 3 && stats.Funcs[0].Peephole["local-shift-snapshot-consumer"] == 0 {
											t.Fatal("concrete sibling consumer not captured")
										}
										hits += uint64(n)
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
}
