//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestFloatCompareBranchSemantics(t *testing.T) {
	saved := floatBranchEnabled
	defer func() { floatBranchEnabled = saved }()
	for _, wide := range []bool{false, true} {
		typ, first := wasm.F32, byte(0x5b)
		values := []uint64{0, 0x80000000, 0x3f800000, 0xbf800000, 1, 0x7f800000, 0xff800000, 0x7fc01234, 0x7f800001}
		if wide {
			typ, first = wasm.F64, 0x61
			values = []uint64{0, 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000, 1, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8123456789abc, 0x7ff0000000000001}
		}
		for cmp := byte(0); cmp < 6; cmp++ {
			for _, branch := range []bool{false, true} {
				for _, invert := range []bool{false, true} {
					body := []byte{0}
					if branch {
						body = append(body, 0x02, 0x7f, 0x41, 17)
					}
					body = append(body, 0x20, 0, 0x20, 1, first+cmp)
					if invert {
						body = append(body, 0x45)
					}
					if branch {
						body = append(body, 0x0d, 0, 0x1a, 0x41, 29, 0x0b)
					} else {
						body = append(body, 0x04, 0x7f, 0x41, 17, 0x05, 0x41, 29, 0x0b)
					}
					body = append(body, 0x0b)
					m := modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
					for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
						for _, on := range []bool{false, true} {
							floatBranchEnabled = on
							t.Run(fmt.Sprintf("wide=%v/cmp=%d/br=%v/invert=%v/features=%x/on=%v", wide, cmp, branch, invert, features, on), func(t *testing.T) {
								for _, a := range values {
									for _, b := range values {
										af, bf := math.Float64frombits(a), math.Float64frombits(b)
										if !wide {
											af, bf = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
										}
										truth := false
										switch cmp {
										case 0:
											truth = af == bf
										case 1:
											truth = af != bf
										case 2:
											truth = af < bf
										case 3:
											truth = af > bf
										case 4:
											truth = af <= bf
										case 5:
											truth = af >= bf
										}
										if invert {
											truth = !truth
										}
										want := uint64(29)
										if truth {
											want = 17
										}
										var stats ModuleStats
										got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, Stats: optionalTestStats(&stats)}, nil, a, b)
										if err != nil || got != want {
											t.Fatalf("a=%x b=%x got=%d want=%d err=%v", a, b, got, want, err)
										}
										if diagnosticsEnabled {
											if (stats.Funcs[0].Peephole["float-branch-fuse"] > 0) != on {
												t.Fatalf("fusion mismatch: %+v", stats.Funcs[0].Peephole)
											}
										}
									}
								}
							})
						}
					}
				}
			}
		}
	}
}

func TestFloatCompareBranchPreservesPressureAndMerge(t *testing.T) {
	saved := floatBranchEnabled
	floatBranchEnabled = true
	defer func() { floatBranchEnabled = saved }()
	for _, cmp := range []byte{0x63, 0x64} {
		body := []byte{1, 13, 0x7c}
		for x := byte(0); x < 12; x++ {
			body = append(body, 0x44)
			bits := math.Float64bits(float64(x + 1))
			for shift := uint(0); shift < 64; shift += 8 {
				body = append(body, byte(bits>>shift))
			}
			body = append(body, 0x21, x)
		}
		// Keep 24 independently evaluated values below the predicate. Reconciliation
		// must preserve both borrowed comparands, all pinned locals, and the FP merge.
		for i := 0; i < 24; i++ {
			x := byte(i % 12)
			body = append(body, 0x20, x, 0x20, x, 0xa0)
		}
		body = append(body, 0x20, 0, 0x20, 1, cmp, 0x04, 0x7c, 0x20, 0, 0x05, 0x20, 1, 0x0b)
		for i := 0; i < 24; i++ {
			body = append(body, 0xa0)
		}
		for x := byte(0); x < 12; x++ {
			body = append(body, 0x20, x, 0x20, x, 0xa0, 0xa0)
		}
		body = append(body, 0x0b)
		m := mod1(t, nil, []wasm.ValType{wasm.F64}, body)
		var stats ModuleStats
		got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"ext-fp-pins": true, "reg-merge": true}}, nil)
		want := float64(469)
		if cmp == 0x64 {
			want = 470
		}
		if err != nil || math.Float64frombits(got) != want {
			t.Fatalf("cmp=%x result=%g want=%g err=%v", cmp, math.Float64frombits(got), want, err)
		}
		if diagnosticsEnabled {
			if stats.Funcs[0].Peephole["float-branch-fuse"] != 1 {
				t.Fatalf("missing fusion: %+v", stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestFloatCompareBranchLoop(t *testing.T) {
	saved := floatBranchEnabled
	floatBranchEnabled = true
	defer func() { floatBranchEnabled = saved }()
	// One declared f64 counter; increment before the ordered backedge test.
	body := []byte{1, 1, 0x7c, 0x03, 0x40, 0x20, 1, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0, 0x21, 1, 0x20, 1, 0x20, 0, 0x63, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}, body)
	for _, target := range []float64{0, 1, 4, 9, math.NaN()} {
		var stats ModuleStats
		got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, nil, math.Float64bits(target))
		want := target
		if !(target > 1) {
			want = 1
		}
		if err != nil || math.Float64frombits(got) != want {
			t.Fatalf("target=%g got=%g want=%g err=%v", target, math.Float64frombits(got), want, err)
		}
		if diagnosticsEnabled {
			if stats.Funcs[0].Peephole["float-branch-fuse"] != 1 {
				t.Fatalf("missing loop fusion: %+v", stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestFloatCompareBranchMaterializationOwnership(t *testing.T) {
	f := &fn{a: &encoderamd64.Asm{}, s: newStack(), reserved: maskOf(R9)}
	left, right := f.pushFReg(0, mtF64), f.pushFReg(1, mtF64)
	node := f.s.alloc()
	node.setElemKind(ekDeferred)
	node.setDeferredOp(opFLt)
	node.setValueType(mtI32)
	node.arg0, node.arg1 = left, right
	labelDeferredNode(node)
	f.s.pushDeferred(node)
	if got := f.condenseFloatCompare(node, R8); got != R8 {
		t.Fatalf("result=%v", got)
	}
	if node.st.kind != stReg || node.st.typ != mtI32 || f.regUser[R8] != node || f.fregUser[0] != nil || f.fregUser[1] != nil || f.reserved != maskOf(R9) {
		t.Fatal("materialization ownership or reservations changed")
	}
	want := &encoderamd64.Asm{}
	want.Ucomis(1, 0, true)
	want.SetccReg(condA, R8)
	if !bytes.Equal(f.a.B, want.B) {
		t.Fatalf("got %x want %x", f.a.B, want.B)
	}
}

func TestFloatCompareBranchMemoryOperands(t *testing.T) {
	saved := floatBranchEnabled
	floatBranchEnabled = true
	defer func() { floatBranchEnabled = saved }()
	for _, wide := range []bool{false, true} {
		load, cmp, width := byte(0x2a), byte(0x5d), 4
		a, b := uint64(math.Float32bits(-2)), uint64(math.Float32bits(3))
		if wide {
			load, cmp, width = 0x2b, 0x63, 8
			a, b = math.Float64bits(-2), math.Float64bits(3)
		}
		body := []byte{0, 0x20, 0, load, 0, 0, 0x20, 1, load, 0, 0, cmp, 0x04, 0x7f, 0x41, 17, 0x05, 0x41, 29, 0x0b, 0x0b}
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, guard := range []bool{false, true} {
			var stats ModuleStats
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Stats: optionalTestStats(&stats)}, func(mem []byte) {
				if wide {
					binary.LittleEndian.PutUint64(mem[:8], a)
					binary.LittleEndian.PutUint64(mem[65536-width:], b)
				} else {
					binary.LittleEndian.PutUint32(mem[:4], uint32(a))
					binary.LittleEndian.PutUint32(mem[65536-width:], uint32(b))
				}
			}, 0, uint64(65536-width))
			if err != nil || got != 17 {
				t.Fatalf("wide=%v guard=%v got=%d err=%v", wide, guard, got, err)
			}
			if diagnosticsEnabled {
				if stats.Funcs[0].Peephole["float-branch-fuse"] != 1 {
					t.Fatalf("missing memory fusion: %+v", stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestFloatCompareIntegerSelect(t *testing.T) {
	saved := floatBranchEnabled
	floatBranchEnabled = true
	defer func() { floatBranchEnabled = saved }()
	for _, wide := range []bool{false, true} {
		typ, first := wasm.F32, byte(0x5b)
		values := []uint64{0, 1 << 31, 1, 0x3f800000, 0xbf800000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345}
		if wide {
			typ, first = wasm.F64, 0x61
			values = []uint64{0, 1 << 63, 1, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234}
		}
		for op := byte(0); op < 6; op++ {
			for _, invert := range []bool{false, true} {
				for _, typed := range []bool{false, true} {
					body := []byte{0, 0x20, 2, 0x20, 3, 0x20, 0, 0x20, 1, first + op}
					if invert {
						body = append(body, 0x45)
					}
					if typed {
						body = append(body, 0x1c, 1, 0x7e)
					} else {
						body = append(body, 0x1b)
					}
					body = append(body, 0x0b)
					m := mod1(t, []wasm.ValType{typ, typ, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
					for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats)})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							t.Cleanup(func() { cm.CodeImage.Close() })
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["float-branch-fuse"] != 1 || stats.Funcs[0].Peephole["select-flags"] != 1) {
							t.Fatalf("fusion not selected: %v", stats.Funcs[0].Peephole)
						}
						for _, a := range values {
							for _, b := range values {
								x, y := math.Float64frombits(a), math.Float64frombits(b)
								if !wide {
									x, y = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
								}
								truth := false
								switch op {
								case 0:
									truth = x == y
								case 1:
									truth = x != y
								case 2:
									truth = x < y
								case 3:
									truth = x > y
								case 4:
									truth = x <= y
								case 5:
									truth = x >= y
								}
								if invert {
									truth = !truth
								}
								left, right := uint64(0x12345678abcdef01), uint64(0xfedcba9876543210)
								want := right
								if truth {
									want = left
								}
								got := runCompiledAmd64u(t, cm, a, b, left, right)
								if got != want {
									t.Fatalf("wide=%v op=%d invert=%v typed=%v features=%x a=%x b=%x got=%x want=%x", wide, op, invert, typed, features, a, b, got, want)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestFloatCompareAllRelationsPendingLoads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, load, align, first := wasm.F32, byte(0x2a), byte(2), byte(0x5b)
		if wide {
			ft, load, align, first = wasm.F64, 0x2b, 3, 0x61
		}
		for k := byte(0); k < 6; k++ {
			body := []byte{1, 1, 0x7f}
			for i := byte(1); i <= 40; i++ {
				body = append(body, 0x20, 2, 0x41, i, 0x6c, load, align, 0)
			}
			body = append(body, 0x20, 0, 0x20, 1, first+k, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x21, 3)
			for range 40 {
				body = append(body, 0x1a)
			}
			body = append(body, 0x20, 3, 0x0b)
			m := mod1(t, []wasm.ValType{ft, ft, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			a, b := uint64(0x7f812345), uint64(0x3f800000)
			if wide {
				a, b = 0x7ff0000000001234, 0x3ff0000000000000
			}
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, a, b, 8)
			want := uint64(0)
			if k == 1 {
				want = 1
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%d got=%d want=%d err=%v", wide, k, got, want, err)
			}
		}
	}
}

func TestFloatCompareSelectMaterialization(t *testing.T) {
	saved := floatBranchEnabled
	floatBranchEnabled = true
	defer func() { floatBranchEnabled = saved }()
	for op := byte(0); op < 6; op++ {
		// Float-valued selections materialize the predicate and preserve the
		// selected payload, including a signaling NaN and negative zero.
		body := []byte{0, 0x20, 2, 0x20, 3, 0x20, 0, 0x20, 1, 0x61 + op, 0x1c, 1, 0x7c, 0xbd, 0x0b}
		m := mod1(t, []wasm.ValType{wasm.F64, wasm.F64, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I64}, body)
		left, right := uint64(0x7ff0000000001234), uint64(1<<63)
		want := right
		if op == 1 {
			want = left
		}
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, enabled := range []bool{false, true} {
				var stats ModuleStats
				opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"st-flags": enabled}}
				got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 0x7ff8000000000001, math.Float64bits(1), left, right)
				if err != nil || got != want {
					t.Fatalf("op=%d features=%x enabled=%v got=%x want=%x err=%v", op, features, enabled, got, want, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["float-branch-materialize"] == 1) != enabled {
					t.Fatalf("materialization/rollback mismatch: %v", stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestFloatCompareAllRelationsLoadTrap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		load, align, first := byte(0x2a), byte(2), byte(0x5b)
		if wide {
			load, align, first = 0x2b, 3, 0x61
		}
		for k := byte(0); k < 6; k++ {
			// Even comparisons whose result is false must evaluate the load.
			body := []byte{0, 0x20, 0, load, align, 0, 0x41, 0, load, align, 0, first + k, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x0b}
			m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 0)
			want := uint64(1)
			if k == 1 || k == 2 || k == 3 {
				want = 0
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%d got=%d want=%d err=%v", wide, k, got, want, err)
			}
			if _, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 65536); err == nil {
				t.Fatalf("wide=%v op=%d: load trap eliminated", wide, k)
			}
		}
	}
}
