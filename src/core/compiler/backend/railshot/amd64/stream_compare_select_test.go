//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	wr "github.com/wago-org/wago/src/core/runtime"
	"reflect"
	"testing"
)

func streamCompareSelectEnable(t *testing.T) {
	t.Helper()
	a, b := streamingOperandsEnabled, streamingCompareSelectEnabled
	streamingOperandsEnabled, streamingCompareSelectEnabled = true, true
	t.Cleanup(func() { streamingOperandsEnabled, streamingCompareSelectEnabled = a, b })
}
func streamCompareSelectWant(which int, wide bool, x, y uint64) bool {
	if !wide {
		x, y = uint64(uint32(x)), uint64(uint32(y))
	}
	sx, sy := int64(x), int64(y)
	if !wide {
		sx, sy = int64(int32(x)), int64(int32(y))
	}
	switch which {
	case 0:
		return x == y
	case 1:
		return x != y
	case 2:
		return sx < sy
	case 3:
		return x < y
	case 4:
		return sx > sy
	case 5:
		return x > y
	case 6:
		return sx <= sy
	case 7:
		return x <= y
	case 8:
		return sx >= sy
	default:
		return x >= y
	}
}
func TestStreamCompareSelectRelations(t *testing.T) {
	streamCompareSelectEnable(t)
	for _, wide := range []bool{false, true} {
		typ, base := wasm.I32, byte(0x46)
		if wide {
			typ, base = wasm.I64, 0x51
		}
		for rel := 0; rel < 10; rel++ {
			for _, on := range []bool{false, true} {
				for _, intr := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide%v-rel%d-on%v-intr%v", wide, rel, on, intr), func(t *testing.T) {
						streamingCompareSelectEnabled = on
						// Selected value aliases one of the comparison operands (min/max shapes).
						m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, 0x20, 0, 0x20, 1, base + byte(rel), 0x1b, 0x0b})
						opts := streamNumericControlCompile(t, m, intr)
						cm, e := CompileModuleWith(m, opts)
						if e != nil {
							t.Fatal(e)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						streamNumericControlSelected(t, opts)
						if diagnosticsEnabled && !regallocCheckEnabled {
							got := opts.Stats.Funcs[0].Peephole["stream-compare-select"]
							if (on && got != 1) || (!on && got != 0) {
								t.Fatalf("counter on%v got%d", on, got)
							}
						}
						for _, xy := range [][2]uint64{{0, 0}, {1, 9}, {9, 1}, {^uint64(0), 0}, {1 << 63, 1}, {0xdeadbeef80000000, 0xabcdef017fffffff}, {0x1111111100000007, 0xeeeeeeee00000007}} {
							want := xy[1]
							if streamCompareSelectWant(rel, wide, xy[0], xy[1]) {
								want = xy[0]
							}
							if !wide {
								want = uint64(uint32(want))
							}
							if got := runCompiledAmd64u(t, cm, xy[0], xy[1]); got != want {
								t.Fatalf("xy%x got%x want%x", xy, got, want)
							}
						}
					})
				}
			}
		}
	}
}
func TestStreamCompareSelectFixedPressure(t *testing.T) {
	streamCompareSelectEnable(t)
	// Deep live prefix plus fixed-register division/shift children. The division
	// is evaluated before select; later materialization may not corrupt its value.
	for _, op := range []byte{0x6e, 0x70, 0x74, 0x76, 0x77} {
		b := []byte{0}
		for i := 0; i < 16; i++ {
			b = append(b, 0x20, 0, 0x41, byte(i+1), 0x6a)
		}
		b = append(b, 0x20, 0, 0x20, 1, 0x20, 0, 0x20, 1, op, 0x20, 2, 0x49, 0x1b)
		for i := 0; i < 16; i++ {
			b = append(b, 0x6a)
		}
		b = append(b, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
		for _, intr := range []bool{false, true} {
			for _, on := range []bool{false, true} {
				streamingCompareSelectEnabled = on
				opts := streamNumericControlCompile(t, m, intr)
				cm, e := CompileModuleWith(m, opts)
				if e != nil {
					t.Fatal(e)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				for _, threshold := range []uint64{0, 4, 1000} {
					var v uint32
					switch op {
					case 0x6e:
						v = 21 / 3
					case 0x70:
						v = 21 % 3
					case 0x74:
						v = 21 << 3
					case 0x76:
						v = 21 >> 3
					case 0x77:
						v = 21<<3 | 21>>29
					}
					want := uint64(3)
					if v < uint32(threshold) {
						want = 21
					}
					want += 16*21 + 136
					if got := runCompiledAmd64u(t, cm, 21, 3, threshold); got != want {
						t.Fatalf("op%x on%v got%d want%d", op, on, got, want)
					}
				}
				if on && diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["stream-compare-select"] == 0 {
					t.Fatal("pressure fixture used fallback")
				}
			}
		}
	}
}
func TestStreamCompareSelectUnselectedTrap(t *testing.T) {
	streamCompareSelectEnable(t)
	for _, on := range []bool{false, true} {
		streamingCompareSelectEnabled = on
		for _, intr := range []bool{false, true} {
			// True comparison selects7, but the other eager operand must still div0.
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, []byte{0, 0x41, 7, 0x20, 0, 0x20, 1, 0x6e, 0x20, 0, 0x20, 0, 0x46, 0x1b, 0x1a, 0x0b})
			opts := streamNumericControlCompile(t, m, intr)
			_, _, err := runMemAmd64WithOptions(t, m, opts, nil, 9, 0)
			var trap *wr.TrapError
			if !errors.As(err, &trap) || trap.Code != wr.TrapDivZero {
				t.Fatal(err)
			}
			if len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != m.Code[0].LocalDeclBytes+6 {
				t.Fatalf("source%+v", trap.Frames)
			}
		}
	}
}

func TestStreamCompareSelectIndependentWidths(t *testing.T) {
	streamCompareSelectEnable(t)
	for _, compareWide := range []bool{false, true} {
		ct, vt, cmp := wasm.I32, wasm.I64, byte(0x49)
		if compareWide {
			ct, vt, cmp = wasm.I64, wasm.I32, 0x54
		}
		m := mod1(t, []wasm.ValType{vt, vt, ct, ct}, []wasm.ValType{vt}, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, cmp, 0x1b, 0x0b})
		for _, on := range []bool{false, true} {
			streamingCompareSelectEnabled = on
			opts := streamNumericControlCompile(t, m, false)
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			for _, xy := range [][2]uint64{{0x8000000000000000, 0x8000000000000001}, {0xffffffff00000001, 0xeeeeeeee00000002}, {^uint64(0), 0}} {
				left, right := uint64(0x1234567887654321), uint64(0xabcdef019abcdef0)
				want := right
				if streamCompareSelectWant(3, compareWide, xy[0], xy[1]) {
					want = left
				}
				if compareWide {
					want = uint64(uint32(want))
				}
				if got := runCompiledAmd64u(t, cm, left, right, xy[0], xy[1]); got != want {
					t.Fatalf("compareWide%v on%v got%x want%x", compareWide, on, got, want)
				}
			}
			if on && diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["stream-compare-select"] != 1 {
				t.Fatal("independent widths did not fuse")
			}
		}
	}
}

func TestStreamCompareSelectExactEmissionAndSharedFallback(t *testing.T) {
	streamCompareSelectEnable(t)
	for _, wide := range []bool{false, true} {
		for _, sharedCondition := range []bool{false, true} {
			f := &fn{a: &encoder.Asm{}}
			s := &streamState{target: f, regs: []uint8{uint8(RAX), uint8(R8), uint8(R9), uint8(R10), uint8(R11)}, tempBase: 1, nextSlot: 1}
			s.add(streamNode{})
			makeReg := func(r Reg) streamID {
				id := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(r), wide: wide, refs: 1})
				s.owners[r] = id
				return id
			}
			left, right, cl, cr := makeReg(R8), makeReg(R9), makeReg(R10), makeReg(R11)
			cond := s.add(streamNode{kind: streamDeferred, op: shared.IntLtS, left: cl, right: cr, operandWide: wide, refs: 1})
			if sharedCondition {
				s.retain(cond)
			}
			before := append([]streamNode(nil), s.nodes...)
			owners := s.owners
			ok := s.tryCompareSelect(cond, right, left)
			if sharedCondition {
				if ok || len(f.a.B) != 0 || !reflect.DeepEqual(before, s.nodes) || owners != s.owners {
					t.Fatal("shared condition must reject without mutation")
				}
				continue
			}
			if !ok {
				t.Fatal("unique deferred comparison rejected")
			}
			result := Reg(s.node(s.stack[len(s.stack)-1]).reg)
			var want encoder.Asm
			if wide {
				want.MovReg64(result, R9)
			} else {
				want.MovRegReg32(result, R9)
			}
			want.AluRR(0x39, R10, R11, wide)
			want.Cmovcc(condL, result, R8, wide)
			// Exact whole helper output: one copy, CMP and CMOV. No SETcc boolean,
			// allocator spill, or flag-clobbering instruction may occur in this span.
			if !bytes.Equal(f.a.B, want.B) {
				t.Fatalf("wide%v got%x want%x", wide, f.a.B, want.B)
			}
		}
	}
}
