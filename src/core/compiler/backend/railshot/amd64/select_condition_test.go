//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

func selectFixedConditionModule(t testing.TB, wide bool, branchOp, conditionOp byte, pressure int, flags bool) *wasm.Module {
	t.Helper()
	typ, constant, add := wasm.I32, byte(0x41), byte(0x6a)
	if wide {
		typ, constant, add = wasm.I64, 0x42, 0x7c
	}
	body := []byte{0x00}
	// Earlier select results stay live while the final select is lowered.
	for range pressure {
		body = append(body, 0x20, 0x00, constant, 0x00, 0x41, 0x01, 0x1b)
	}
	body = append(body,
		0x20, 0x00, 0x20, 0x01, branchOp, // a op b
		constant, 0xe3, 0x00, // 99
		0x20, 0x02, 0x20, 0x03, conditionOp, // c op d: i32 condition
	)
	if flags {
		body = append(body, 0x45) // i32.eqz: flags-select control
	}
	body = append(body, 0x1b)
	for range pressure {
		body = append(body, add)
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{typ, typ, wasm.I32, wasm.I32}, []wasm.ValType{typ}, body)
}

func TestSelectPreservesFixedRegisterCondition(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, op := range []struct {
			name     string
			i32, i64 byte
		}{
			{"div_u", 0x6e, 0x80}, {"div_s", 0x6d, 0x7f},
			{"rem_u", 0x70, 0x82}, {"rem_s", 0x6f, 0x81},
			{"shl", 0x74, 0x86}, {"shr_u", 0x76, 0x88}, {"shr_s", 0x75, 0x87},
			{"rotl", 0x77, 0x89}, {"rotr", 0x78, 0x8a},
		} {
			for _, rem := range []bool{false, true} {
				t.Run(fmt.Sprintf("wide=%v/%s/rem_condition=%v", wide, op.name, rem), func(t *testing.T) {
					branchOp := op.i32
					if wide {
						branchOp = op.i64
					}
					conditionOp, divisor := byte(0x6e), uint64(1)
					if rem {
						conditionOp, divisor = 0x70, 2
					}
					m := selectFixedConditionModule(t, wide, branchOp, conditionOp, 0, false)
					for _, tc := range []struct {
						name string
						args []uint64
						want uint64
					}{
						{"true_zero", []uint64{0, 1, 1, divisor}, 0},
						{"false", []uint64{8, 2, 0, divisor}, 99},
						{"false_nonzero_remainder", []uint64{8, 3, 0, divisor}, 99},
					} {
						t.Run(tc.name, func(t *testing.T) {
							if got := runAmd64u(t, m, tc.args...); got != tc.want {
								t.Fatalf("select%v = %d, want %d", tc.args, got, tc.want)
							}
						})
					}
				})
			}
		}
	}
}

func TestSelectFixedRegisterConditionPressure(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, flags := range []bool{false, true} {
			t.Run(fmt.Sprintf("wide=%v/flags=%v", wide, flags), func(t *testing.T) {
				op := byte(0x6e)
				if wide {
					op = 0x80
				}
				m := selectFixedConditionModule(t, wide, op, 0x6e, 20, flags)
				condition := uint64(0)
				if flags {
					condition = 1
				}
				if got := runAmd64u(t, m, 8, 2, condition, 1); got != 259 {
					t.Fatalf("pressured select = %d, want 259", got)
				}
			})
		}
	}
}

func TestSelectBothBranchesDeferred(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0x6e,
		0x20, 0x02, 0x20, 0x03, 0x70,
		0x20, 0x04, 0x20, 0x05, 0x6e,
		0x1b, 0x0b,
	})
	for _, tc := range []struct {
		args []uint64
		want uint64
	}{
		{[]uint64{0, 1, 199, 100, 1, 1}, 0},
		{[]uint64{8, 2, 199, 100, 0, 1}, 99},
	} {
		if got := runAmd64u(t, m, tc.args...); got != tc.want {
			t.Errorf("select%v = %d, want %d", tc.args, got, tc.want)
		}
	}
}

func TestSelectFixedRegisterConditionPreservesI64HighBits(t *testing.T) {
	for _, tc := range []struct {
		op         byte
		a, b, want uint64
	}{
		{0x80, 0x200000008, 2, 0x100000004},
		{0x82, 0x100000008, 0x200000000, 0x100000008},
	} {
		m := selectFixedConditionModule(t, true, tc.op, 0x6e, 0, false)
		if got := runAmd64u(t, m, tc.a, tc.b, 1, 1); got != tc.want {
			t.Errorf("wide select = %#x, want %#x", got, tc.want)
		}
	}
}

func TestSelectOperandsTrapInWasmOrder(t *testing.T) {
	// Both branches and the condition divide. The earliest trapping operand must
	// win, even when that branch would not be selected by the condition.
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0x6d,
		0x20, 0x02, 0x20, 0x03, 0x6d,
		0x20, 0x04, 0x20, 0x05, 0x6d,
		0x1b, 0x0b,
	})
	for _, tc := range []struct {
		name string
		args []uint64
		want runtime.TrapCode
	}{
		{"first_overflow", []uint64{0x80000000, 0xffffffff, 1, 0, 1, 0}, runtime.TrapDivOverflow},
		{"first_zero", []uint64{1, 0, 0x80000000, 0xffffffff, 1, 0}, runtime.TrapDivZero},
		{"second_overflow", []uint64{1, 1, 0x80000000, 0xffffffff, 1, 0}, runtime.TrapDivOverflow},
		{"condition_zero", []uint64{1, 1, 1, 1, 1, 0}, runtime.TrapDivZero},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runMemAmd64(t, m, nil, tc.args...)
			var trap *runtime.TrapError
			if !errors.As(err, &trap) || trap.Code != tc.want {
				t.Fatalf("trap = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSelectReloadsConditionReclaimedByShift(t *testing.T) {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), localSlot: []uint32{0}}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 8})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32})
	f.pushBinOp(opShl, mtI32)
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 99})
	condition := f.pushReg(RCX, mtI32)
	f.emitSelect()
	if condition.st.kind != stReg {
		t.Fatalf("condition was not reloaded after RCX was reclaimed: %+v", condition.st)
	}
	if f.depth() != 1 || f.pinned != 0 {
		t.Fatalf("select left depth=%d, pins=%#x", f.depth(), f.pinned)
	}
}

func TestSelectConditionLoweringDoesNotAllocate(t *testing.T) {
	f := &fn{a: &encoder.Asm{B: make([]byte, 0, 128)}, s: newStackWithCap(16), localSlot: []uint32{0}}
	allocs := testing.AllocsPerRun(1000, func() {
		f.s.reset()
		f.a.B = f.a.B[:0]
		f.regUser = [16]*elem{}
		f.maxSpill = 0
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 8})
		f.pushValue(storage{kind: stLocalRef, typ: mtI32})
		f.pushBinOp(opShl, mtI32)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 99})
		f.pushReg(RCX, mtI32)
		f.emitSelect()
	})
	if allocs != 0 {
		t.Fatalf("allocations per select = %.2f, want 0", allocs)
	}
}

func TestSelectFixedRegisterConditionKeepsFlagsFusion(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := selectFixedConditionModule(t, false, 0x6e, 0x6e, 0, true)
	stats := compileWithStats(t, m, false).Funcs[0]
	if stats.Peephole["select-flags"] != 1 || stats.Peephole["compare-setcc"] != 0 {
		t.Fatalf("lost flags-select fusion: %v", stats.Peephole)
	}
}

func BenchmarkSelectFixedRegisterCondition(b *testing.B) {
	for _, tc := range []struct {
		name  string
		wide  bool
		op    byte
		flags bool
		want  uint64
	}{
		{"i32_div", false, 0x6e, false, 4},
		{"i64_rem", true, 0x82, false, 2},
		{"i32_shift", false, 0x74, false, 32},
		{"flags", false, 0x6e, true, 99},
	} {
		m := selectFixedConditionModule(b, tc.wide, tc.op, 0x6e, 0, tc.flags)
		opts := CompileOptions{DeferCodeMapping: true}
		b.Run(tc.name+"/compile", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				benchCompiledSink = cm
			}
			b.ReportMetric(float64(len(benchCompiledSink.Code)), "code-bytes")
		})
		b.Run(tc.name+"/execute", func(b *testing.B) {
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			eng, err := runtime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := runtime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			arena, err := runtime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer arena.Close()
			code, entry, err := runtime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer runtime.Unmap(code)
			args, results, trap := arena.Alloc(32), arena.Alloc(8), arena.Alloc(runtime.TrapBufferBytes)
			divisor := uint64(2)
			if tc.op == 0x82 {
				divisor = 3
			}
			for i, v := range []uint64{8, divisor, 1, 1} {
				binary.LittleEndian.PutUint64(args[i*8:], v)
			}
			address, memory := entry+uintptr(cm.Entry[0]), jm.LinearMemory()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := eng.Call(address, args, memory, trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got := binary.LittleEndian.Uint64(results); got != tc.want {
				b.Fatalf("select = %d, want %d", got, tc.want)
			}
			b.ReportMetric(float64(len(cm.Code)), "code-bytes")
		})
	}
}
