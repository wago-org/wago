//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestMaskedZeroCompareSelects(t *testing.T) {
	old := maskedZeroCompareEnabled
	t.Cleanup(func() { maskedZeroCompareEnabled = old })
	hits := 0
	for _, wide := range []bool{false, true} {
		typ, bt, k, and, eq, ne, mul, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x71), byte(0x46), byte(0x47), byte(0x6c), byte(0x6a)
		if wide {
			typ, bt, k, and, eq, ne, mul, add = wasm.I64, 0x7e, 0x42, 0x83, 0x51, 0x52, 0x7e, 0x7c
		}
		for _, dynamic := range []bool{false, true} {
			for _, commuted := range []bool{false, true} {
				for _, equal := range []bool{false, true} {
					for _, invert := range []bool{false, true} {
						body := []byte{0, 0x20, 0, k, 7, mul}
						if commuted {
							body = append(body, k, 0)
						}
						body = append(body, 0x20, 0)
						if dynamic {
							body = append(body, 0x20, 1)
						} else {
							body = append(body, k)
							if wide {
								maskBits := uint64(0x8040201080402010)
								body = append(body, wasmtest.SLEB64(int64(maskBits))...)
							} else {
								body = append(body, wasmtest.SLEB32(-2143281136)...)
							}
						}
						body = append(body, and)
						if !commuted {
							body = append(body, k, 0)
						}
						cmp := ne
						if equal {
							cmp = eq
						}
						body = append(body, cmp)
						if invert {
							body = append(body, 0x45)
						}
						body = append(body, 0x04, bt, k, 19, 0x05, k, 3, 0x0b, add, 0x0b)
						m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
						for _, enabled := range []bool{false, true} {
							maskedZeroCompareEnabled = enabled
							for _, compact := range []bool{false, true} {
								for _, input := range []uint64{0, 1, 0x8877665580000000, ^uint64(0)} {
									for _, mask := range []uint64{0, 1, 63, 0x8040201080402010, ^uint64(0)} {
										usedMask := uint64(0x8040201080402010)
										if dynamic {
											usedMask = mask
										}
										value := input & usedMask
										if !wide {
											value = uint64(uint32(value))
										}
										truth := value != 0
										if equal {
											truth = !truth
										}
										if invert {
											truth = !truth
										}
										arm := uint64(3)
										if truth {
											arm = 19
										}
										want := input*7 + arm
										if !wide {
											want = uint64(uint32(want))
										}
										var stats ModuleStats
										opts := CompileOptions{CompactNative: compact}
										if diagnosticsEnabled {
											opts.Stats = &stats
										}
										got, err := runArm64WrapperWithOptions(t, m, opts, input, mask)
										if !wide {
											got = uint64(uint32(got))
										}
										if err != nil || got != want {
											t.Fatalf("wide%v dynamic%v commuted%v equal%v invert%v enabled%v compact%v input%x mask%x got%x want%x err%v", wide, dynamic, commuted, equal, invert, enabled, compact, input, mask, got, want, err)
										}
										if diagnosticsEnabled {
											n := stats.Funcs[0].Peephole["masked-zero-compare"]
											if !enabled && n != 0 {
												t.Fatal("disabled rule selected")
											}
											hits += n
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if diagnosticsEnabled && hits == 0 {
		t.Fatal("masked comparison rule never exercised")
	}
}

func TestMaskedZeroComparePreservesLoadTraps(t *testing.T) {
	old := maskedZeroCompareEnabled
	t.Cleanup(func() { maskedZeroCompareEnabled = old })
	for _, wide := range []bool{false, true} {
		load, k, and, ne, bt, store, align, size := byte(0x28), byte(0x41), byte(0x71), byte(0x47), byte(0x7f), byte(0x36), byte(2), uint64(4)
		if wide {
			load, k, and, ne, bt, store, align, size = 0x29, 0x42, 0x83, 0x52, 0x7e, 0x37, 3, 8
		}
		for _, mask := range []byte{0, 63} {
			body := []byte{0, 0x41, 0x80, 1, 0x20, 0, load, align, 0, k, mask, and, k, 0, ne, 0x04, bt, k, 19, 0x05, k, 3, 0x0b, store, align, 0, 0x0b}
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
			for _, enabled := range []bool{false, true} {
				maskedZeroCompareEnabled = enabled
				for _, address := range []uint64{0, 65536 - size, 65537 - size, 0xffffffff} {
					got, err := runScalarUnrollMemory(t, m, CompileOptions{}, address)
					want := make([]byte, 65536)
					if address > 65536-size {
						var tr *rt.TrapError
						if !errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds {
							t.Fatalf("wide%v mask%d enabled%v address%x err%v", wide, mask, enabled, address, err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if wide {
							binary.LittleEndian.PutUint64(want[128:], 3)
						} else {
							binary.LittleEndian.PutUint32(want[128:], 3)
						}
					}
					if !bytes.Equal(got, want) {
						t.Fatal("memory changed outside the expected result store")
					}
				}
			}
		}
	}
}

func TestMaskedZeroCompareKeepsNonzeroComparisons(t *testing.T) {
	old := maskedZeroCompareEnabled
	maskedZeroCompareEnabled = true
	t.Cleanup(func() { maskedZeroCompareEnabled = old })
	for _, wide := range []bool{false, true} {
		typ, k, and, eq, ne, lt := wasm.I32, byte(0x41), byte(0x71), byte(0x46), byte(0x47), byte(0x49)
		if wide {
			typ, k, and, eq, ne, lt = wasm.I64, 0x42, 0x83, 0x51, 0x52, 0x54
		}
		for _, op := range []byte{eq, ne, lt} {
			body := []byte{0, 0x20, 0, k, 63, and, k, 1, op, 0x04, 0x7f, 0x41, 19, 0x05, 0x41, 3, 0x0b, 0x0b}
			m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, body)
			for x := uint64(0); x < 65; x++ {
				truth := x&63 == 1
				if op == ne {
					truth = !truth
				}
				if op == lt {
					truth = x&63 < 1
				}
				want := uint64(3)
				if truth {
					want = 19
				}
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, x)
				if err != nil || got != want {
					t.Fatalf("wide%v op%x x%d got%d want%d err%v", wide, op, x, got, want, err)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["masked-zero-compare"] != 0 {
					t.Fatal("nonzero comparison selected zero rule")
				}
			}
		}
	}
}
