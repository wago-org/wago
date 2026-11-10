//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func guardedXorFixture(wide bool) (enc.Asm, int, int) {
	var a enc.Asm
	a.MovReg64(X2, X1)
	if wide {
		a.CmpImm64(X0, 8)
	} else {
		a.CmpImm32(X0, 8)
	}
	guard := a.Bcond(Cond(3))
	a.MovImm64(X9, 5)
	a.MovImm64(X10, 3)
	if wide {
		a.Madd64(X9, X0, X10, X9)
		a.CmpImm64(X9, 11)
		a.Cset64(X11, Cond(12))
		a.Eor64(X2, X2, X11)
	} else {
		a.Madd32(X9, X0, X10, X9)
		a.CmpImm32(X9, 11)
		a.Cset32(X11, Cond(12))
		a.Eor32(X2, X2, X11)
	}
	join := a.Len()
	wrWord(a.B, guard, 0x54000003|uint32((join-guard)/4)<<5)
	a.CmpImm32(X0, 0)
	a.MovReg64(X12, X2)
	a.CmpImm32(X12, 0)
	if wide {
		a.MovReg64(X0, X12)
	} else {
		a.MovReg32(X0, X12)
	}
	for _, r := range []Reg{X2, X9, X10, X11, X12} {
		a.MovImm64(r, 0)
	}
	a.Ret()
	return a, guard, join
}

func TestGuardedBooleanXorDirectCallPreservesCallerLocals(t *testing.T) {
	// Twelve full-width caller locals plus an older operand remain observable
	// across a real register-ABI call. The callee's declared accumulator local
	// excludes the narrow pin-preserving ABI; its scratch/native pin clobbers
	// must therefore be published and reconciled by ordinary call lowering.
	caller := []byte{1, 12, 0x7e}
	for i := 0; i < 12; i++ {
		caller = append(caller, 0x20, 1, 0x42)
		caller = append(caller, wasmtest.SLEB64(int64(uint64(i+1)*0x01020304050607))...)
		caller = append(caller, 0x85, 0x21, byte(i+2))
	}
	caller = append(caller, 0x20, 1, 0x20, 0, 0x41, 7, 0x10, 1, 0xad, 0x85)
	for i := 0; i < 12; i++ {
		caller = append(caller, 0x20, byte(i+2), 0x85)
	}
	caller = append(caller, 0x0b)
	// An empty loop selects the full compiler without adding memory effects.
	callee := []byte{1, 1, 0x7f, 0x03, 0x40, 0x0b, 0x20, 1, 0x21, 2, 0x20, 0, 0x41, 8, 0x4f, 0x04, 0x40,
		0x20, 2, 0x20, 0, 0x41, 3, 0x6c, 0x41, 5, 0x6a, 0x41, 11, 0x4a, 0x73, 0x21, 2, 0x0b,
		0x20, 2, 0x41, 0xff, 0x07, 0x71, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, caller}, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, callee})
	for _, on := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			for _, x := range []uint64{0, 1, 7, 8, 9, 0x7fffffff, 0x80000000, 0xffffffff} {
				for _, seed := range []uint64{0, 1, 0xdeadbeef00000001, ^uint64(0)} {
					var stats ModuleStats
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"inline": false, "guarded-boolean-xor": on}}
					if diagnosticsEnabled && x == 0 && seed == 0 {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, x, seed)
					if opts.Stats != nil && (stats.Funcs[1].Peephole["guarded-boolean-xor"] != 0) != on {
						t.Fatalf("strict callee admission on=%v guard=%v stats=%v", on, guard, stats.Funcs[1].Peephole)
					}
					result := uint32(7)
					if uint32(x) >= 8 && int32(uint32(x)*3+5) > 11 {
						result ^= 1
					}
					want := seed ^ uint64(result)
					for i := 0; i < 12; i++ {
						want ^= seed ^ (uint64(i+1) * 0x01020304050607)
					}
					if err != nil || got != want {
						t.Fatalf("on=%v guard=%v x=%x seed=%x got=%x want=%x err=%v", on, guard, x, seed, got, want, err)
					}
				}
			}
		}
	}
}

func TestGuardedBooleanXorNativeCarriers(t *testing.T) {
	old := guardedBooleanXorEnabled
	guardedBooleanXorEnabled = true
	defer func() { guardedBooleanXorEnabled = old }()
	for _, wide := range []bool{false, true} {
		for guardCond := uint32(0); guardCond < 14; guardCond++ {
			for _, on := range []bool{false, true} {
				a, guard, _ := guardedXorFixture(wide)
				wrWord(a.B, guard, rdWord(a.B, guard)&^15|guardCond)
				if on {
					f := fn{policy: currentCodegenPolicy()}
					f.foldGuardedBooleanXors(a.B, len(a.B), nil)
					if rdWord(a.B, guard) != nopWord {
						t.Fatal("pure guarded fixture declined")
					}
				}
				code, err := arm64spike.MapExec(a.B)
				if err != nil {
					t.Fatal(err)
				}
				for _, x := range []uint64{0, 1, 7, 8, 9, 0x7fffffff, 0x80000000, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
					for _, inside := range []uint64{0, 1, 7, 0xfeedface00000000, ^uint64(0)} {
						want := inside
						active := !guardedXorBranchExpected(guardCond, x, wide)
						value := int64(x*3 + 5)
						if !wide {
							value = int64(int32(uint32(x)*3 + 5))
						}
						if active {
							if value > 11 {
								want ^= 1
							}
							if !wide {
								want = uint64(uint32(want))
							}
						}
						if !wide {
							want = uint64(uint32(want))
						}
						got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(x), uintptr(inside))
						if uint64(got) != want {
							t.Fatalf("wide=%v guard=%d on=%v x=%x carrier=%x got=%x want=%x", wide, guardCond, on, x, inside, got, want)
						}
					}
				}
			}
		}
	}
}

func guardedXorBranchExpected(c uint32, x uint64, wide bool) bool {
	value, difference := int64(x), int64(x-8)
	if !wide {
		x = uint64(uint32(x))
		value = int64(int32(x))
		difference = int64(int32(uint32(x) - 8))
	}
	overflow := value < 0 && difference >= 0
	switch c {
	case 0:
		return x == 8
	case 1:
		return x != 8
	case 2:
		return x >= 8
	case 3:
		return x < 8
	case 4:
		return difference < 0
	case 5:
		return difference >= 0
	case 6:
		return overflow
	case 7:
		return !overflow
	case 8:
		return x > 8
	case 9:
		return x <= 8
	case 10:
		return value >= 8
	case 11:
		return value < 8
	case 12:
		return value > 8
	case 13:
		return value <= 8
	}
	panic("invalid condition")
}

func TestGuardedBooleanXorDeclinesObservationsAndEntries(t *testing.T) {
	old := guardedBooleanXorEnabled
	guardedBooleanXorEnabled = true
	defer func() { guardedBooleanXorEnabled = old }()
	for _, shape := range []string{"valid", "scratch-read", "high-read", "high-result", "memory", "flags", "logical-SP", "external", "entry", "EH", "custom", "custom-unknown"} {
		a, guard, join := guardedXorFixture(false)
		targets := make([]uint64, (len(a.B)/4+63)/64)
		var entries []int
		f := fn{policy: currentCodegenPolicy()}
		switch shape {
		case "scratch-read":
			wrWord(a.B, join, 0x7100013f) // CMP W9,0 before killing scratch
		case "high-read":
			wrWord(a.B, join+8, 0xf100019f) // CMP X12,0 observes propagated upper bits
		case "high-result":
			wrWord(a.B, join+12, 0xaa0c03e0) // MOV X0,X12 exports changed upper bits
		case "memory":
			wrWord(a.B, guard+4, 0xb9400009)
		case "flags":
			wrWord(a.B, guard+4, 0x71000409)
		case "logical-SP":
			wrWord(a.B, guard+4, 0x1200143f) // AND WSP,W1,#63 changes the stack pointer.
		case "external":
			branchTargetAdd(targets, guard+4, len(a.B))
		case "entry":
			entries = []int{guard + 4}
		case "EH":
			f.moduleEH = true
		case "custom":
			f.customInstructions = map[uint32]railcore.CustomInstruction{0: {}}
		case "custom-unknown":
			wrWord(a.B, guard+4, 0xffffffff)
		}
		f.foldGuardedBooleanXors(a.B, len(a.B), targets, entries...)
		if (rdWord(a.B, guard) == nopWord) != (shape == "valid") {
			t.Fatalf("shape=%s incorrectly admitted/declined", shape)
		}
	}
}
