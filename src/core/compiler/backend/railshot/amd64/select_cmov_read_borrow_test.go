//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestSelectCmovReadBorrowPreservesArm(t *testing.T) {
	saved, savedCmov := selectReadBorrowEnabled, selectCmovReadBorrowEnabled
	defer func() { selectReadBorrowEnabled, selectCmovReadBorrowEnabled = saved, savedCmov }()
	selectReadBorrowEnabled = true
	for _, width := range []int{32, 64} {
		typ, constant, add, xor, div, rem, shift := wasm.I32, byte(0x41), byte(0x6a), byte(0x73), byte(0x6e), byte(0x70), byte(0x74)
		mask, shiftMask := uint64(0xffffffff), uint64(31)
		if width == 64 {
			typ, constant, add, xor, div, rem, shift = wasm.I64, 0x42, 0x7c, 0x85, 0x80, 0x82, 0x86
			mask, shiftMask = ^uint64(0), 63
		}
		for _, branch := range []string{"plain", "alias", "div", "rem", "shift"} {
			for _, pressure := range []int{0, 24} {
				t.Run(fmt.Sprintf("i%d/%s/pressure=%d", width, branch, pressure), func(t *testing.T) {
					body := []byte{0}
					for range pressure {
						body = append(body, 0x20, 0, constant, 0, 0x41, 1, 0x1b)
					}
					if branch == "alias" {
						body = append(body, 0x20, 1)
					} else {
						body = append(body, 0x20, 0)
					}
					if branch != "plain" && branch != "alias" {
						body = append(body, 0x20, 3)
						switch branch {
						case "div":
							body = append(body, div)
						case "rem":
							body = append(body, rem)
						case "shift":
							body = append(body, shift)
						}
					}
					// A plain i32 local condition forces ordinary CMOV, rather than
					// compare-flags selection. Re-read the arm after select to prove
					// that borrowing did not change the local's value.
					body = append(body, 0x20, 1, 0x20, 2, 0x1b, 0x20, 1, xor)
					for range pressure {
						body = append(body, add)
					}
					body = append(body, 0x0b)
					m := mod1(t, []wasm.ValType{typ, typ, wasm.I32, typ}, []wasm.ValType{typ}, body)
					for _, on := range []bool{false, true} {
						selectCmovReadBorrowEnabled = on
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"reg-abi": true}})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["select-cmov-read-borrow"] > 0) != on {
							t.Fatalf("on=%v admission=%v", on, stats.Funcs[0].Peephole)
						}
						for _, values := range [][3]uint64{{123, 17, 3}, {0, 0, 1}, {0xfedcba9876543210, 0x80000000, 33}, {17, ^uint64(0), 63}} {
							for _, condition := range []uint64{0, 1, 2, 0xffffffff} {
								args := []uint64{values[0] & mask, values[1] & mask, condition, values[2] & mask}
								a := args[0]
								switch branch {
								case "alias":
									a = args[1]
								case "div":
									a /= args[3]
								case "rem":
									a %= args[3]
								case "shift":
									a = (a << (args[3] & shiftMask)) & mask
								}
								want := uint64(0)
								if condition != 0 {
									want = a ^ args[1]
								}
								want = (want + uint64(pressure)*args[0]) & mask
								if got := runCompiledAmd64u(t, cm, args...); got != want {
									t.Fatalf("on=%v args=%x got=%x want=%x", on, args, got, want)
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestSelectReadBorrowExcludesFixedRegisters(t *testing.T) {
	for _, kind := range []storageKind{stLocalReg, stGlobReg} {
		for _, reg := range []Reg{RAX, RDX, RCX, R12, R13, R14, R15} {
			e := &elem{st: storage{kind: kind, reg: reg, typ: mtI32}}
			want := reg != RAX && reg != RDX && reg != RCX
			if got := selectReadBorrowable(e); got != want {
				t.Fatalf("kind=%v reg=%v borrowed=%v want=%v", kind, reg, got, want)
			}
		}
	}
}

func TestSelectCmovReadBorrowRestoresMasksWithoutAllocation(t *testing.T) {
	saved, savedCmov := selectReadBorrowEnabled, selectCmovReadBorrowEnabled
	defer func() { selectReadBorrowEnabled, selectCmovReadBorrowEnabled = saved, savedCmov }()
	selectReadBorrowEnabled, selectCmovReadBorrowEnabled = true, true
	f := &fn{a: &encoder.Asm{B: make([]byte, 0, 128)}, s: newStackWithCap(16), pinnedLocalMask: maskOf(R12)}
	f.pinned, f.reserved = maskOf(R8), maskOf(R15)
	allocs := testing.AllocsPerRun(1000, func() {
		f.s.reset()
		f.a.B = f.a.B[:0]
		f.regUser = [16]*elem{}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 8})
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R12})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 2})
		f.emitSelect()
	})
	if allocs != 0 {
		t.Fatalf("allocations per borrowed select = %.2f, want 0", allocs)
	}
	if f.pinned != maskOf(R8) || f.reserved != maskOf(R15) || f.depth() != 1 {
		t.Fatalf("select left pins=%#x reserved=%#x depth=%d", f.pinned, f.reserved, f.depth())
	}
}

func TestSelectCmovReservedArmBlocksIntervalEviction(t *testing.T) {
	// No stack reference or ordinary pin protects local 0 in this fixture.
	// The CMOV lease's reserved bit alone must block regional eviction.
	f := &fn{
		s:               newStack(),
		locals:          []localDef{{reg: R12, state: lsStackReg}, {reg: R13, state: lsStackReg}},
		intervalReg:     []Reg{RSP, RSP},
		intervalScore:   []uint32{1, 2},
		intervalActive:  2,
		reserved:        maskOf(R12),
		pinnedLocalMask: maskOf(R12, R13),
	}
	for r := range f.intervalOwner {
		f.intervalOwner[r] = -1
	}
	f.intervalOwner[R12], f.intervalOwner[R13] = 0, 1
	if got := f.evictIntervalLocal(0); got != R13 {
		t.Fatalf("evicted register %v, want unreserved R13", got)
	}
	if f.locals[0].reg != R12 || f.intervalOwner[R12] != 0 {
		t.Fatal("reserved arm lost its regional register")
	}
	if got := f.evictIntervalLocal(0); got != regNone {
		t.Fatalf("reserved arm was the only remaining local, but eviction returned %v", got)
	}
	f.reserved = 0
	if got := f.evictIntervalLocal(0); got != R12 {
		t.Fatalf("released arm lease did not allow eviction: got %v, want R12", got)
	}
}

func TestSelectCmovReadBorrowIntervalPressure(t *testing.T) {
	requireCompilerDiagnostics(t)
	savedScalar, savedPure := sharedScalarEnabled, pureSelectEnabled
	saved, savedCmov := selectReadBorrowEnabled, selectCmovReadBorrowEnabled
	defer func() {
		sharedScalarEnabled, pureSelectEnabled = savedScalar, savedPure
		selectReadBorrowEnabled, selectCmovReadBorrowEnabled = saved, savedCmov
	}()
	sharedScalarEnabled, pureSelectEnabled, selectReadBorrowEnabled = false, false, true
	const pressure, locals, terms = 6, 24, 4
	body := []byte{1, locals, 0x7f}
	for x := byte(3); x < 3+locals; x++ {
		body = append(body, 0x20, 0, 0x41, x+1, 0x73, 0x21, x)
	}
	// Keep owned values live while a deferred integer tree and both select
	// arms compete with the regional cache for the remaining registers.
	for range pressure {
		body = append(body, 0x20, 0, 0x41, 0, 0x41, 1, 0x1b)
	}
	for x := byte(3); x < 3+terms; x++ {
		body = append(body, 0x20, x, 0x41, 7, 0x6c)
	}
	for range terms - 1 {
		body = append(body, 0x6a)
	}
	body = append(body, 0x20, 26, 0x20, 2, 0x1b, 0x20, 26, 0x73)
	for range pressure {
		body = append(body, 0x6a)
	}
	for x := byte(3 + terms); x < 3+locals; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, nextUse := range []bool{false, true} {
		for _, borrow := range []bool{false, true} {
			t.Run(fmt.Sprintf("next-use=%v/borrow=%v", nextUse, borrow), func(t *testing.T) {
				selectCmovReadBorrowEnabled = borrow
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
					"reg-abi": true, "interval-region-pins": true, "interval-next-use": nextUse,
					"interval-scratch-lease": false, "interval-r8-lease": false,
				}})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				s := stats.Funcs[0]
				if s.Peephole["interval-region"] != 1 || s.Residency.Evictions == 0 {
					t.Fatalf("fixture did not exercise regional pressure: peep=%v residency=%+v", s.Peephole, s.Residency)
				}
				if (s.Peephole["select-cmov-read-borrow"] > 0) != borrow {
					t.Fatalf("borrow=%v admission=%v", borrow, s.Peephole)
				}
				t.Logf("regional evictions=%d borrowed CMOV arms=%d spills=%d reloads=%d", s.Residency.Evictions, s.Peephole["select-cmov-read-borrow"], s.Spills, s.Reloads)
				for _, input := range []uint32{0, 1, 0x80000000, 0xfedcba98, 0xffffffff} {
					var sum uint32
					for x := uint32(3); x < 3+terms; x++ {
						sum += (input ^ (x + 1)) * 7
					}
					for _, condition := range []uint64{0, 1, 2, 0xffffffff} {
						want := uint32(pressure) * input
						if condition != 0 {
							want += sum ^ (input ^ 27)
						}
						for x := uint32(3 + terms); x < 3+locals; x++ {
							want += input ^ (x + 1)
						}
						got := runCompiledAmd64u(t, cm, uint64(input), 0, condition)
						if got != uint64(want) {
							t.Fatalf("input=%x condition=%x got=%x want=%x", input, condition, got, want)
						}
					}
				}
			})
		}
	}
}
