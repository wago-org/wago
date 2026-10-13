//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func ccmpPairModule(t testing.TB, wide, guarded, firstEqual, lastEqual bool) *wasm.Module {
	typ, and, xor, eq, ne, eqz, constant := wasm.I32, byte(0x71), byte(0x73), byte(0x46), byte(0x47), byte(0x45), byte(0x41)
	if wide {
		typ, and, xor, eq, ne, eqz, constant = wasm.I64, 0x83, 0x85, 0x51, 0x52, 0x50, 0x42
	}
	body := []byte{0, 0x3f, 0, 0x1a, 0x02, 0x40}
	if guarded {
		body = append(body, 0x02, 0x40, 0x20, 0, 0x20, 2, and)
		if firstEqual {
			body = append(body, eqz)
		} else if wide {
			body = append(body, eqz, 0x45)
		}
		body = append(body, 0x0d, 0, 0x20, 1, 0x20, 0, constant, 0x7f, xor, and)
		if lastEqual {
			body = append(body, eqz)
		} else if wide {
			body = append(body, eqz, 0x45)
		}
		body = append(body, 0x0d, 1, 0x0b)
	} else {
		body = append(body, 0x20, 0, 0x20, 1)
		cmp := ne
		if firstEqual {
			cmp = eq
		}
		body = append(body, cmp, 0x0d, 0, 0x20, 0, 0x20, 2, and)
		if lastEqual {
			body = append(body, eqz)
		} else if wide {
			body = append(body, eqz, 0x45)
		}
		body = append(body, 0x0d, 0)
	}
	body = append(body, 0x41, 42, 0x0f, 0x0b, 0x41, 7, 0x0b)
	return modMem(t, 1, []wasm.ValType{typ, typ, typ}, []wasm.ValType{wasm.I32}, body)
}

func TestCCMPPairsIndependentExecution(t *testing.T) {
	old := guardedTestCCMPEnabled
	defer func() { guardedTestCCMPEnabled = old }()
	for _, wide := range []bool{false, true} {
		for _, guarded := range []bool{false, true} {
			for _, firstEq := range []bool{false, true} {
				for _, lastEq := range []bool{false, true} {
					m := ccmpPairModule(t, wide, guarded, firstEq, lastEq)
					for _, a := range []uint64{0, 1, 3, 0x80000000, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
						for _, b := range []uint64{0, 1, 3, ^uint64(0)} {
							for _, mask := range []uint64{0, 1, 7, ^uint64(0)} {
								x, y, z := a, b, mask
								if !wide {
									x, y, z = uint64(uint32(a)), uint64(uint32(b)), uint64(uint32(mask))
								}
								first := (x == y) == firstEq
								second := (x&z == 0) == lastEq
								selected := first || second
								if guarded {
									first = (x&z == 0) == firstEq
									second = (y&^x == 0) == lastEq
									selected = !first && second
								}
								want := uint64(42)
								if selected {
									want = 7
								}
								guardedTestCCMPEnabled = true
								if guarded {
									guardedTestCCMPEnabled = true
								}
								got := runArm64u(t, m, a, b, mask)
								if got != want {
									t.Fatalf("wide%v guarded%v eq%v,%v input%x,%x,%x: got%d want%d", wide, guarded, firstEq, lastEq, a, b, mask, got, want)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestCCMPPairsAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := guardedTestCCMPEnabled
	defer func() { guardedTestCCMPEnabled = old }()
	for _, guarded := range []bool{false, true} {
		guardedTestCCMPEnabled = true
		peep := "ccmp-guarded-tests"
		if guarded {
			guardedTestCCMPEnabled = true
			peep = "ccmp-guarded-tests"
		}
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("guarded%v/wide%v", guarded, wide), func(t *testing.T) {
				stats := compileWithStats(t, ccmpPairModule(t, wide, guarded, false, true), false).Funcs[0]
				want := 0
				if guarded {
					want = 1
				}
				if stats.Peephole[peep] != want {
					t.Fatalf("not admitted: %v", stats.Peephole)
				}
			})
		}
	}
}

func TestGuardedTestsDeclineLiveFlagsAndIncomingEdges(t *testing.T) {
	base := []uint32{0x6A01001F, 0x54000061, 0x6A23005F, 0x54000060, 0x7100009F, 0xD65F03C0, 0xD65F03C0}
	for _, test := range []struct {
		name   string
		word   int
		value  uint32
		target int
		opaque bool
		fold   bool
	}{
		{"eligible", -1, 0, -1, false, true},
		{"incoming-middle", -1, 0, 8, false, false},
		{"live-flags-fallthrough", 4, 0x1A810000, -1, false, false},
		{"live-flags-target", 6, 0x54000000, -1, false, false},
		{"scratch-input", 0, 0x6A01021F, -1, false, false},
		{"wrong-skip", 1, 0x54000041, -1, false, false},
		{"opaque", -1, 0, -1, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			words := append([]uint32{}, base...)
			if test.word >= 0 {
				words[test.word] = test.value
			}
			b := make([]byte, 4*len(words))
			for i, w := range words {
				binary.LittleEndian.PutUint32(b[i*4:], w)
			}
			targets := make([]uint64, 1)
			if test.target >= 0 {
				branchTargetAdd(targets, test.target, len(b))
			}
			f := fn{opaqueFragments: test.opaque}
			f.foldGuardedTests(b, len(b), targets)
			if test.fold {
				if got := rdWord(b, 8); got != 0x7A400A00 {
					t.Fatalf("CCMP word %x", got)
				}
				if got := rdWord(b, 0); got != 0x0A230050 {
					t.Fatalf("BIC word %x", got)
				}
			} else {
				for i, w := range words {
					if rdWord(b, i*4) != w {
						t.Fatalf("unsafe rewrite at word %d", i)
					}
				}
			}
		})
	}
}

func TestGuardedTestCCMPPolicyRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(ccmpPairModule(t, false, true, false, true), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"guarded-test-ccmp": false}})
	if err != nil {
		t.Fatal(err)
	}
	cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["ccmp-guarded-tests"] != 0 {
		t.Fatal("disabled cover admitted")
	}
}
