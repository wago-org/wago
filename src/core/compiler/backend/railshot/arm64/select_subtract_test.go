//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/bits"
	"testing"
)

func selectSubtractFixture(t testing.TB, wide, swap, zeroTrue, typed bool, relation int, zero byte, destinations ...byte) *wasm.Module {
	destination := byte(2)
	if len(destinations) > 0 {
		destination = destinations[0]
	}
	preserve := len(destinations) > 1 && destinations[1] != 0
	tee := len(destinations) > 2 && destinations[2] != 0
	typ, c, xor, rol, sub := wasm.I32, byte(0x41), byte(0x73), byte(0x77), byte(0x6b)
	typByte := byte(0x7f)
	cmp := byte(0x46 + relation)
	if wide {
		typ, c, xor, rol, sub = wasm.I64, 0x42, 0x85, 0x89, 0x7d
		cmp = byte(0x51 + relation)
		typByte = 0x7e
	}
	// A real linear-memory effect keeps this test on the ordinary backend,
	// instead of the separate numeric scalar compiler.
	b := []byte{1, 1, typByte, 0x41, 0, 0x41, 0, 0x36, 2, 0}
	if preserve {
		b = append(b, 0x20, destination)
	}
	b = append(b, 0x20, 0)
	if zeroTrue {
		b = append(b, c, zero, 0x20, 1)
	} else {
		b = append(b, 0x20, 1, c, zero)
	}
	if swap {
		b = append(b, 0x20, 1, 0x20, 0)
	} else {
		b = append(b, 0x20, 0, 0x20, 1)
	}
	b = append(b, cmp)
	if typed {
		b = append(b, 0x1c, 1, typByte)
	} else {
		b = append(b, 0x1b)
	}
	b = append(b, sub)
	if destination != 255 {
		if tee {
			b = append(b, 0x22, destination)
		} else {
			b = append(b, 0x21, destination, 0x20, destination)
		}
	}
	b = append(b, 0x20, 0, c, 5, rol, xor, 0x20, 1, c, 11, rol, xor)
	if preserve {
		b = append(b, xor)
	}
	b = append(b, 0x0b)
	return modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, b)
}

func selectSubtractPredicate(a, b uint64, wide bool, relation int) bool {
	if !wide {
		a, b = uint64(uint32(a)), uint64(uint32(b))
	}
	sa, sb := int64(a), int64(b)
	if !wide {
		sa, sb = int64(int32(a)), int64(int32(b))
	}
	switch relation {
	case 0:
		return a == b
	case 1:
		return a != b
	case 2:
		return sa < sb
	case 3:
		return a < b
	case 4:
		return sa > sb
	case 5:
		return a > b
	case 6:
		return sa <= sb
	case 7:
		return a <= b
	case 8:
		return sa >= sb
	case 9:
		return a >= b
	}
	panic("relation")
}

func TestSelectSubtractSemantics(t *testing.T) {
	pairs := [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {3, 7}, {7, 3}, {0xffffffff, 0}, {0x80000000, 1}, {0x7fffffff, 0x80000000}, {0x8000000000000000, 1}, {0x7fffffffffffffff, 0x8000000000000000}, {0xffffffffffffffff, 0}, {0x8877665500000001, 0x9988776600000002}, {0xffffffffffffffff, 0x8000000000000000}}
	for _, wide := range []bool{false, true} {
		for relation := 0; relation < 10; relation++ {
			for _, swap := range []bool{false, true} {
				for _, zeroTrue := range []bool{false, true} {
					for _, on := range []bool{false, true} {
						m := selectSubtractFixture(t, wide, swap, zeroTrue, relation%2 == 0, relation, 0)
						for _, pair := range pairs {
							a, b := pair[0], pair[1]
							if !wide {
								a, b = uint64(uint32(a)), uint64(uint32(b))
							}
							x, y := a, b
							if swap {
								x, y = y, x
							}
							predicate := selectSubtractPredicate(x, y, wide, relation)
							selected := b
							if predicate == zeroTrue {
								selected = 0
							}
							want := a - selected
							if wide {
								want ^= bits.RotateLeft64(a, 5) ^ bits.RotateLeft64(b, 11)
							} else {
								want = uint64(uint32(want) ^ bits.RotateLeft32(uint32(a), 5) ^ bits.RotateLeft32(uint32(b), 11))
							}
							got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"select-subtract": on}}, pair[0], pair[1])
							if err != nil || got != want {
								t.Fatalf("wide%v rel%d swap%v zeroTrue%v on%v pair%x got%x want%x err%v", wide, relation, swap, zeroTrue, on, pair, got, want, err)
							}
						}
					}
				}
			}
		}
	}
}

func TestSelectSubtractSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		for relation := 0; relation < 10; relation++ {
			for _, zero := range []byte{0, 1} {
				var stats ModuleStats
				cm, err := CompileModuleWith(selectSubtractFixture(t, wide, true, true, false, relation, zero), CompileOptions{Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				cm.CodeImage.Close()
				hit := stats.Funcs[0].Peephole["select-subtract"]
				if (hit != 0) != (zero == 0) {
					t.Fatalf("wide%v rel%d zero%d hit%d", wide, relation, zero, hit)
				}
			}
		}
	}
}

func TestSelectSubtractAliasing(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, destination := range []byte{0, 1, 2} {
			for _, on := range []bool{false, true} {
				m := selectSubtractFixture(t, wide, true, true, true, 4, 0, destination, 1)
				for _, pair := range [][2]uint64{{0, 0}, {3, 7}, {7, 3}, {0xffffffff, 1}, {0x80000000, 0x7fffffff}, {0x8000000000000000, 0x7fffffffffffffff}, {0x8877665500000001, 0x9988776600000002}} {
					a, b := pair[0], pair[1]
					if !wide {
						a, b = uint64(uint32(a)), uint64(uint32(b))
					}
					old := uint64(0)
					if destination == 0 {
						old = a
					}
					if destination == 1 {
						old = b
					}
					result := a
					if !selectSubtractPredicate(b, a, wide, 4) {
						result -= b
					}
					if !wide {
						result = uint64(uint32(result))
					}
					if destination == 0 {
						a = result
					}
					if destination == 1 {
						b = result
					}
					want := result ^ bits.RotateLeft64(a, 5) ^ bits.RotateLeft64(b, 11) ^ old
					if !wide {
						want = uint64(uint32(result) ^ bits.RotateLeft32(uint32(a), 5) ^ bits.RotateLeft32(uint32(b), 11) ^ uint32(old))
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"select-subtract": on}}, pair[0], pair[1])
					if err != nil || got != want {
						t.Fatalf("wide%v dest%d on%v pair%x got%x want%x err%v", wide, destination, on, pair, got, want, err)
					}
				}
			}
		}
	}
}

func TestSelectSubtractWithoutLocalSink(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			m := selectSubtractFixture(t, wide, false, true, false, 3, 0, 255)
			for _, pair := range [][2]uint64{{0, 1}, {3, 7}, {7, 3}, {0xffffffff, 1}, {0x8000000000000000, 0xffffffffffffffff}, {0x8877665500000001, 0x9988776600000002}} {
				a, b := pair[0], pair[1]
				if !wide {
					a, b = uint64(uint32(a)), uint64(uint32(b))
				}
				result := a
				if a >= b {
					result -= b
				}
				want := result ^ bits.RotateLeft64(a, 5) ^ bits.RotateLeft64(b, 11)
				if !wide {
					want = uint64(uint32(result) ^ bits.RotateLeft32(uint32(a), 5) ^ bits.RotateLeft32(uint32(b), 11))
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"select-subtract": on}}, pair[0], pair[1])
				if err != nil || got != want {
					t.Fatalf("wide%v on%v pair%x got%x want%x err%v", wide, on, pair, got, want, err)
				}
			}
		}
	}
}

func TestSelectSubtractTeeOldReads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			m := selectSubtractFixture(t, wide, true, true, true, 4, 0, 0, 1, 1)
			for _, pair := range [][2]uint64{{3, 7}, {7, 3}, {0x80000000, 0x7fffffff}, {0x8000000000000000, 0x7fffffffffffffff}, {0x8877665500000001, 0x9988776600000002}} {
				a, b := pair[0], pair[1]
				if !wide {
					a, b = uint64(uint32(a)), uint64(uint32(b))
				}
				result := a
				if !selectSubtractPredicate(b, a, wide, 4) {
					result -= b
				}
				if !wide {
					result = uint64(uint32(result))
				}
				want := result ^ bits.RotateLeft64(result, 5) ^ bits.RotateLeft64(b, 11) ^ a
				if !wide {
					want = uint64(uint32(result) ^ bits.RotateLeft32(uint32(result), 5) ^ bits.RotateLeft32(uint32(b), 11) ^ uint32(a))
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"select-subtract": on}}, pair[0], pair[1])
				if err != nil || got != want {
					t.Fatalf("wide%v on%v pair%x got%x want%x err%v", wide, on, pair, got, want, err)
				}
			}
		}
	}
}
