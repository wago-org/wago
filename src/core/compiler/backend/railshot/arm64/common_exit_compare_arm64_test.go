//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestCommonExitCompareTruthTable(t *testing.T) {
	masks := []uint16{0xf0f0, 0x0f0f, 0xcccc, 0x3333, 0xff00, 0x00ff, 0xaaaa, 0x5555, 0x0c0c, 0xf3f3, 0xaa55, 0x55aa, 0x0a05, 0xf5fa}
	for c, m := range masks {
		for flags := uint32(0); flags < 16; flags++ {
			if got, want := compareCondHolds(Cond(c), flags), m&(1<<flags) != 0; got != want {
				t.Fatalf("cond=%d flags=%x got=%v want=%v", c, flags, got, want)
			}
		}
	}
	for c1, m1 := range masks {
		for c2, m2 := range masks {
			fallback := uint32(0)
			for m2&(1<<fallback) == 0 {
				fallback++
			}
			for first := uint32(0); first < 16; first++ {
				for second := uint32(0); second < 16; second++ {
					expected := m1&(1<<first) != 0 || m2&(1<<second) != 0
					flags := fallback
					if m1&(1<<first) == 0 {
						flags = second
					}
					if got := compareCondHolds(Cond(c2), flags); got != expected {
						t.Fatalf("pair=%d/%d flags=%x/%x got=%v want=%v", c1, c2, first, second, got, expected)
					}
				}
			}
		}
	}
}

func TestCommonExitCompareEncodingGoldens(t *testing.T) {
	for _, tc := range []struct{ input, want uint32 }{
		{0x6b02003f, 0x7a42a020}, {0xeb02003f, 0xfa42a020},
		{0x2b02003f, 0x3a42a020}, {0xab02003f, 0xba42a020},
		{0x7100143f, 0x7a45a820}, {0xf100143f, 0xfa45a820},
		{0x3100143f, 0x3a45a820}, {0xb100143f, 0xba45a820},
	} {
		got, ok := conditionalCompareWord(tc.input, Cond(10), 0)
		if !ok || got != tc.want {
			t.Fatalf("input=%x got=%x ok=%v want=%x", tc.input, got, ok, tc.want)
		}
	}
	for _, word := range []uint32{0x710017ff, 0x7100803f, 0x7140143f, 0x6b02043f, 0xffffffff} {
		if got, ok := conditionalCompareWord(word, Cond(10), 0); ok {
			t.Fatalf("unsupported input=%x accepted=%x", word, got)
		}
	}
}

func TestCommonExitCompareRewriteGuards(t *testing.T) {
	old := commonExitCompareEnabled
	commonExitCompareEnabled = true
	defer func() { commonExitCompareEnabled = old }()
	for _, shape := range []string{"valid", "different-target", "live-flags", "external-target", "extra-entry", "EH", "custom"} {
		words := []uint32{0x7100043f, 0x540000ab, 0x6b02003f, 0x5400006c, 0x7100007f, 0xd65f03c0, 0x7100007f, 0xd65f03c0}
		if shape == "different-target" {
			words[1] = 0x5400008b
		}
		if shape == "live-flags" {
			words[6] = 0x1a820023
		}
		b := make([]byte, len(words)*4)
		for i, w := range words {
			binary.LittleEndian.PutUint32(b[i*4:], w)
		}
		targets := []uint64{0}
		if shape == "external-target" {
			targets[0] |= 1 << 2
		}
		f := fn{policy: currentCodegenPolicy()}
		if shape == "EH" {
			f.moduleEH = true
		}
		if shape == "custom" {
			f.customInstructions = map[uint32]railcore.CustomInstruction{0: {}}
		}
		var entries []int
		if shape == "extra-entry" {
			entries = []int{8}
		}
		f.foldCommonExitCompares(b, len(b), targets, entries...)
		changed := rdWord(b, 8) == nopWord
		if changed != (shape == "valid") {
			t.Fatalf("shape=%s changed=%v", shape, changed)
		}
		if rdWord(b, 0) != words[0] || rdWord(b, 12) != words[3] {
			t.Fatal("first compare/last branch changed")
		}
	}
}

func commonExitRelation(op int, x, y uint64, wide bool) bool {
	if !wide {
		x, y = uint64(uint32(x)), uint64(uint32(y))
	}
	signedX, signedY := int64(x), int64(y)
	if !wide {
		signedX, signedY = int64(int32(x)), int64(int32(y))
	}
	switch op {
	case 0:
		return x == y
	case 1:
		return x != y
	case 2:
		return signedX < signedY
	case 3:
		return x < y
	case 4:
		return signedX > signedY
	case 5:
		return x > y
	case 6:
		return signedX <= signedY
	case 7:
		return x <= y
	case 8:
		return signedX >= signedY
	case 9:
		return x >= y
	}
	return false
}

func TestCommonExitCompareNativeBranches(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, cons, base, eq := wasm.I32, byte(0x41), byte(0x46), byte(0x46)
		if wide {
			typ, cons, base, eq = wasm.I64, 0x42, 0x51, 0x51
		}
		for first := 0; first < 10; first++ {
			for second := 0; second < 10; second++ {
				body := []byte{0, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, cons, 1, base + byte(first), 0x0d, 0,
					0x20, 0, cons, 5, base + byte(second), 0x0d, 0,
					0x20, 0, cons, 17, eq, 0x04, 0x7f, 0x41, 42, 0x05, 0x41, 43, 0x0b, 0x0f,
					0x0b, 0x20, 0, cons, 23, eq, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 8, 0x0b, 0x0b}
				m := modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, body)
				for _, enabled := range []bool{false, true} {
					for _, guard := range []bool{false, true} {
						for _, x := range []uint64{0, 1, 4, 5, 17, 23, 0xffffffff, 0x80000000, 0x8000000000000000, ^uint64(0)} {
							opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"common-exit-compare": enabled}}
							var stats ModuleStats
							if diagnosticsEnabled && first == 2 && second == 4 && x == 0 {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, x)
							if opts.Stats != nil && (stats.Funcs[0].Peephole["common-exit-compare"] != 0) != enabled {
								t.Fatalf("missing compare-pair admission wide=%v guard=%v enabled=%v stats=%v", wide, guard, enabled, stats.Funcs[0].Peephole)
							}
							value := x
							if !wide {
								value = uint64(uint32(x))
							}
							want := uint64(43)
							if value == 17 {
								want = 42
							}
							if commonExitRelation(first, x, 1, wide) || commonExitRelation(second, x, 5, wide) {
								want = 8
								if value == 23 {
									want = 7
								}
							}
							if err != nil || uint64(uint32(got)) != want {
								t.Fatalf("wide=%v comparisons=%d/%d enabled=%v guard=%v x=%x got=%d want=%d err=%v", wide, first, second, enabled, guard, x, got, want, err)
							}
						}
					}
				}
			}
		}
	}
}
