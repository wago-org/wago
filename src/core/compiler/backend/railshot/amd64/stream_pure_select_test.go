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

func streamPureSelectTestEnable(t *testing.T) {
	t.Helper()
	streamGenerationTestEnable(t)
	a, b, c := streamingPureSelectEnabled, pureSelectEnabled, streamingWideHomesEnabled
	streamingPureSelectEnabled, pureSelectEnabled, streamingWideHomesEnabled = true, true, true
	t.Cleanup(func() { streamingPureSelectEnabled, pureSelectEnabled, streamingWideHomesEnabled = a, b, c })
}
func TestStreamPureSelectArithmetic(t *testing.T) {
	streamPureSelectTestEnable(t)
	for _, on := range []bool{false, true} {
		streamingPureSelectEnabled = on
		for _, wide := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				for _, intr := range []bool{false, true} {
					t.Run(fmt.Sprintf("on%v-wide%v-reverse%v-intr%v", on, wide, reverse, intr), func(t *testing.T) {
						m := pureSelectModule(t, wide, reverse, []byte{0x20, 2}, 0)
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
							n := opts.Stats.Funcs[0].Peephole["stream-pure-select-branch"]
							if on && n != 1 || !on && n != 0 {
								t.Fatal("branch select activation", on, n)
							}
						}
						for _, xy := range [][2]uint64{{7, 11}, {0xffffffff, 0x80000000}, {^uint64(0), 1 << 63}} {
							for _, cond := range []uint64{0, 1, 0x80000000, 0xffffffff, 0x1234567800000000} {
								x, y := xy[0], xy[1]
								want := x
								if (uint32(cond) != 0) != reverse {
									want = ((x^y)*17 ^ 3) * 19
								}
								if !wide {
									want = uint64(uint32(want))
								}
								if got := runCompiledAmd64u(t, cm, x, y, cond, 1); got != want {
									t.Fatalf("xy%v cond%x got%x want%x", xy, cond, got, want)
								}
							}
						}
					})
				}
			}
		}
	}
}
func TestStreamPureSelectFixedConditionAndSnapshots(t *testing.T) {
	streamPureSelectTestEnable(t)
	streamingPureSelectEnabled = true
	for _, op := range []byte{0x6e, 0x70, 0x74, 0x76, 0x77} {
		for _, intr := range []bool{false, true} {
			m := pureSelectModule(t, false, false, []byte{0x20, 2, 0x20, 3, op}, 20)
			opts := streamNumericControlCompile(t, m, intr)
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			streamNumericControlSelected(t, opts)
			for _, cond := range []uint64{0, 8} {
				want := uint64(27)
				if cond != 0 && op != 0x70 {
					want = uint64(((7^11)*17^3)*19 + 20)
				}
				if got := runCompiledAmd64u(t, cm, 7, 11, cond, 1); got != want {
					t.Fatalf("op%x intr%v got%d want%d", op, intr, got, want)
				}
			}
		}
	}
	// Captured old local values survive local.tee in the expensive chain and
	// another overwrite in the condition, even when expensive arm is not chosen.
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c, 0x20, 0, 0x20, 1, 0x22, 0, 0x1b, 0x20, 0, 0x6a, 0x0b})
	for _, intr := range []bool{false, true} {
		opts := streamNumericControlCompile(t, m, intr)
		cm, e := CompileModuleWith(m, opts)
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		streamNumericControlSelected(t, opts)
		for _, cond := range []uint64{0, 1, 37} {
			want := uint64(8)
			if cond != 0 {
				want = (8*17 ^ 3) * 19
			}
			want += cond
			if got := runCompiledAmd64u(t, cm, 7, cond); got != want {
				t.Fatalf("snapshot cond%d got%d want%d", cond, got, want)
			}
		}
	}
}
func TestStreamPureSelectUnselectedTraps(t *testing.T) {
	streamPureSelectTestEnable(t)
	streamingPureSelectEnabled = true
	for _, tc := range []struct {
		name   string
		prefix []byte
		args   []uint64
		code   wr.TrapCode
		pc     uint32
	}{
		{"load", []byte{0x20, 0, 0x28, 2, 0}, []uint64{65536, 0}, wr.TrapLinMemOutOfBounds, 2},
		{"div", []byte{0x20, 0, 0x20, 1, 0x6e}, []uint64{9, 0}, wr.TrapDivZero, 4},
		{"rem", []byte{0x20, 0, 0x20, 1, 0x70}, []uint64{9, 0}, wr.TrapDivZero, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0}, tc.prefix...)
			body = append(body, 0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c, 0x41, 7, 0x41, 0, 0x1b, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, intr := range []bool{false, true} {
				opts := streamNumericControlCompile(t, m, intr)
				_, _, err := runMemAmd64WithOptions(t, m, opts, nil, tc.args...)
				streamNumericControlSelected(t, opts)
				var trap *wr.TrapError
				if !errors.As(err, &trap) || trap.Code != tc.code || len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != m.Code[0].LocalDeclBytes+tc.pc {
					t.Fatalf("unselected eager trap/PC mismatch %+v", err)
				}
			}
		})
	}
}

func TestStreamPureSelectRejectsUnsafeShape(t *testing.T) {
	streamPureSelectTestEnable(t)
	for _, kind := range []string{"shared-internal", "no-multiply", "too-long", "unsupported-shift", "deferred-rhs"} {
		t.Run(kind, func(t *testing.T) {
			s := streamResidencyUnitState()
			left := s.locals[0]
			s.retain(left)
			n := 4
			if kind == "too-long" {
				n = 13
			}
			for i := 0; i < n; i++ {
				op := shared.IntMul
				if i%2 == 1 || kind == "no-multiply" {
					op = shared.IntXor
				}
				rhs := s.add(streamNode{kind: shared.ScalarConstant, constant: 17, refs: 1})
				if kind == "unsupported-shift" && i == 0 {
					op = shared.IntShl
				}
				if kind == "deferred-rhs" && i == 0 {
					a := s.add(streamNode{kind: shared.ScalarConstant, constant: 3, refs: 1})
					rhs = s.add(streamNode{kind: streamDeferred, op: shared.IntAdd, left: rhs, right: a, refs: 1})
				}
				left = s.add(streamNode{kind: streamDeferred, op: op, left: left, right: rhs, refs: 1})
				if kind == "shared-internal" && i == 1 {
					s.retain(left)
				}
			}
			right := s.add(streamNode{kind: shared.ScalarConstant, constant: 7, refs: 1})
			cond := s.add(streamNode{kind: shared.ScalarConstant, constant: 1, refs: 1})
			nodes := append([]streamNode{}, s.nodes...)
			cells := append([]streamBackingCell{}, s.residency.cells...)
			code := append([]byte{}, s.target.a.B...)
			slots := s.nextSlot
			if s.tryPureSelect(cond, right, left) {
				t.Fatal("unsafe or uneconomic shape selected")
			}
			if !reflect.DeepEqual(nodes, s.nodes) || !reflect.DeepEqual(cells, s.residency.cells) || !bytes.Equal(code, s.target.a.B) || s.nextSlot != slots {
				t.Fatal("rejected pure select mutated compiler state")
			}
		})
	}
}

func TestStreamPureSelectGuardedEmission(t *testing.T) {
	streamPureSelectTestEnable(t)
	for _, kind := range []string{"backed", "owned", "frame"} {
		t.Run(kind, func(t *testing.T) {
			s := streamResidencyUnitState()
			s.regs = []uint8{uint8(RAX), uint8(RCX), uint8(RDX), uint8(R8)}
			base := s.locals[0]
			s.retain(base)
			if kind == "frame" {
				s.evacuateBacking(s.residency.homes[0])
			}
			if kind == "owned" {
				s.materialize(base, 0)
			}
			operand := s.operand(base)
			left := base
			for i, op := range []shared.IntOp{shared.IntMul, shared.IntXor, shared.IntMul} {
				v := []int64{17, 3, 19}[i]
				rhs := s.add(streamNode{kind: shared.ScalarConstant, constant: v, refs: 1})
				left = s.add(streamNode{kind: streamDeferred, op: op, left: left, right: rhs, refs: 1})
			}
			right := s.add(streamNode{kind: shared.ScalarConstant, constant: 9, refs: 1})
			cond := s.add(streamNode{kind: shared.ScalarConstant, constant: 1, refs: 1})
			if !s.tryPureSelect(cond, right, left) {
				t.Fatal("valid captured leaf did not select branch")
			}
			result := Reg(s.node(s.stack[len(s.stack)-1]).reg)
			var want encoder.Asm
			if operand.Kind == shared.ScalarRegister {
				want.MovRegReg32(result, Reg(operand.Reg))
			} else {
				want.Load32(result, RSP, operand.Offset)
			}
			want.ImulRI(result, 17, false)
			want.AluRI(aluTable[opXor].digit, result, 3, false)
			want.ImulRI(result, 19, false)
			// Everything after the guard is precisely captured reads and ALU: no
			// allocator-induced spill/store/call or extra trapping instruction is emitted.
			if !bytes.HasSuffix(s.target.a.B, want.B) {
				t.Fatalf("guarded body differs: got%x want suffix%x", s.target.a.B, want.B)
			}
		})
	}
}
