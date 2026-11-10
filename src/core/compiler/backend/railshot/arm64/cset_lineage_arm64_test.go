//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"syscall"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func csetLineageFixture(cmpWide, setWide, copyWide, resultWide bool, cc, selCC Cond) (enc.Asm, int, int, int, int) {
	var a enc.Asm
	a.SubImm64(SP, SP, 16)
	a.MovReg64(X2, X0)
	producer := a.Len()
	if cmpWide {
		a.CmpReg64(X0, X1)
	} else {
		a.CmpReg32(X0, X1)
	}
	set := a.Len()
	if setWide {
		a.Cset64(X9, cc)
	} else {
		a.Cset32(X9, cc)
	}
	a.Store64(X2, SP, 0)
	a.Load64(X11, SP, 0)
	if copyWide {
		a.MovReg64(X10, X9)
	} else {
		a.MovReg32(X10, X9)
	}
	cmp := a.Len()
	a.CmpImm32(X10, 0)
	sel := a.Len()
	a.Csel(X0, X11, X1, selCC, resultWide)
	a.CmpImm32(X0, 1)
	a.AddImm64(SP, SP, 16)
	a.Ret()
	return a, producer, set, cmp, sel
}

func TestCsetLineageNativeWidthsConditionsAndCopies(t *testing.T) {
	old := csetLineageEnabled
	csetLineageEnabled = true
	defer func() { csetLineageEnabled = old }()
	pairs := [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {0x80000000, 0x7fffffff}, {0x7fffffff, 0x80000000}, {0x8000000000000000, 1}, {0x7fffffffffffffff, 0xffffffffffffffff}, {^uint64(0), 0}, {0xfeedface00000000, 0xdeadbeef00000001}, {0x8000000000000000, 0x7fffffffffffffff}}
	for _, cmpWide := range []bool{false, true} {
		for _, setWide := range []bool{false, true} {
			for _, copyWide := range []bool{false, true} {
				for _, resultWide := range []bool{false, true} {
					for cc := Cond(0); cc < 14; cc++ {
						for _, selCC := range []Cond{condE, condNE} {
							a, _, _, cmp, sel := csetLineageFixture(cmpWide, setWide, copyWide, resultWide, cc, selCC)
							before := append([]byte(nil), a.B...)
							f := fn{policy: currentCodegenPolicy()}
							f.foldCsetLineage(a.B, len(a.B), 0, nil)
							if rdWord(a.B, cmp) != nopWord {
								t.Fatalf("declined widths=%v/%v/%v/%v cc=%v sel=%v", cmpWide, setWide, copyWide, resultWide, cc, selCC)
							}
							expectedCC := cc
							if selCC == condE {
								expectedCC = invertCond(cc)
							}
							if Cond(rdWord(a.B, sel)>>12&15) != expectedCC {
								t.Fatal("condition mapping")
							}
							for i := 0; i < len(before); i += 4 {
								if i != cmp && i != sel && !bytes.Equal(before[i:i+4], a.B[i:i+4]) {
									t.Fatalf("unexpected changed word %x", i)
								}
							}
							original, err := arm64spike.MapExec(before)
							if err != nil {
								t.Fatal(err)
							}
							candidate, err := arm64spike.MapExec(a.B)
							if err != nil {
								t.Fatal(err)
							}
							for _, pair := range pairs {
								want := arm64spike.Call2(uintptr(unsafe.Pointer(&original[0])), uintptr(pair[0]), uintptr(pair[1]))
								got := arm64spike.Call2(uintptr(unsafe.Pointer(&candidate[0])), uintptr(pair[0]), uintptr(pair[1]))
								if got != want {
									t.Fatalf("widths=%v/%v/%v/%v cc=%v sel=%v pair=%x got=%x want=%x", cmpWide, setWide, copyWide, resultWide, cc, selCC, pair, got, want)
								}
							}
							syscall.Munmap(original)
							syscall.Munmap(candidate)
						}
					}
				}
			}
		}
	}
}

func TestCsetLineageEntryAndOpaqueRejection(t *testing.T) {
	old := csetLineageEnabled
	csetLineageEnabled = true
	defer func() { csetLineageEnabled = old }()
	base, producer, _, _, sel := csetLineageFixture(true, false, true, true, Cond(3), condE)
	for entry := producer + 4; entry <= sel; entry += 4 {
		for _, external := range []bool{false, true} {
			b := append([]byte(nil), base.B...)
			var targets [16]uint64
			var entries []int
			if external {
				entries = []int{entry}
			} else {
				branchTargetAdd(targets[:], entry, len(b))
			}
			f := fn{policy: currentCodegenPolicy()}
			f.foldCsetLineage(b, len(b), 0, targets[:], entries...)
			if !bytes.Equal(b, base.B) {
				t.Fatalf("entry %x external=%v modified", entry, external)
			}
		}
	}
	for _, opaque := range []bool{false, true} {
		b := append([]byte(nil), base.B...)
		f := fn{policy: currentCodegenPolicy(), moduleEH: !opaque, opaqueFragments: opaque}
		f.foldCsetLineage(b, len(b), 0, nil)
		if !bytes.Equal(b, base.B) {
			t.Fatal("EH/opaque modified")
		}
	}
	b := append([]byte(nil), base.B...)
	for len(b) <= 4096 {
		b = append(b, 0x1f, 0x20, 0x03, 0xd5)
	}
	before := append([]byte(nil), b...)
	f := fn{policy: currentCodegenPolicy()}
	f.foldCsetLineage(b, len(b), 0, nil)
	if !bytes.Equal(b, before) {
		t.Fatal("size cap")
	}
}

func TestCsetLineageRejectsFlagReuseAndChangedBooleans(t *testing.T) {
	old := csetLineageEnabled
	csetLineageEnabled = true
	defer func() { csetLineageEnabled = old }()
	base, _, _, cmp, sel := csetLineageFixture(true, false, true, true, Cond(3), condE)
	for name, edit := range map[string]func([]byte){
		"loaded-overwrite":           func(b []byte) { wrWord(b, cmp-4, rdWord(b, cmp-8)&^uint32(31)|10) },
		"copied-overwrite":           func(b []byte) { wrWord(b, cmp-4, 0xd280000a) },
		"flags-overwrite":            func(b []byte) { wrWord(b, cmp-4, 0xf100013f) },
		"unknown":                    func(b []byte) { wrWord(b, cmp-4, 0xffffffff) },
		"non-EQ-NE-consumer":         func(b []byte) { wrWord(b, sel, rdWord(b, sel)&^uint32(15<<12)|uint32(2<<12)) },
		"subsequent-cset-flags-read": func(b []byte) { wrWord(b, sel+4, 0x1a9f37eb) },
		"conditional-compare-tail":   func(b []byte) { wrWord(b, sel+4, 0x7a410000) },
	} {
		b := append([]byte(nil), base.B...)
		edit(b)
		before := append([]byte(nil), b...)
		f := fn{policy: currentCodegenPolicy()}
		f.foldCsetLineage(b, len(b), 0, nil)
		if !bytes.Equal(b, before) {
			t.Fatalf("unsafe %s modified", name)
		}
	}
	// A boolean kept alive beyond the bounded producer window is declined.
	b := append([]byte(nil), base.B[:cmp]...)
	for range 33 {
		b = append(b, 0x1f, 0x20, 0x03, 0xd5)
	}
	b = append(b, base.B[cmp:]...)
	before := append([]byte(nil), b...)
	f := fn{policy: currentCodegenPolicy()}
	f.foldCsetLineage(b, len(b), 0, nil)
	if !bytes.Equal(b, before) {
		t.Fatal("producer window cap")
	}
}

func TestCsetLineageSequentialBooleanConsumers(t *testing.T) {
	old := csetLineageEnabled
	csetLineageEnabled = true
	defer func() { csetLineageEnabled = old }()
	a, _, _, first, sel := csetLineageFixture(true, false, true, true, Cond(3), condE)
	// Replace the first tail writer with another compare/select consumer of the
	// same original boolean; the last CMP restores the flags before return.
	tail := append([]byte(nil), a.B[sel+4:]...)
	a.B = a.B[:sel+4]
	second := a.Len()
	a.CmpImm32(X9, 0)
	a.Csel(X2, X2, X1, condNE, true)
	a.B = append(a.B, tail...)
	f := fn{policy: currentCodegenPolicy()}
	f.foldCsetLineage(a.B, len(a.B), 0, nil)
	if rdWord(a.B, first) != nopWord || rdWord(a.B, second) != nopWord {
		t.Fatal("sequential flag lineage was lost")
	}
}

func TestCsetLineageBoundedStartMatchesFullScan(t *testing.T) {
	old := csetLineageEnabled
	csetLineageEnabled = true
	defer func() { csetLineageEnabled = old }()
	for _, padding := range []int{0, 256, 1024} {
		for _, gap := range []int{0, 120, 128} {
			a, _, set, cmp, _ := csetLineageFixture(true, false, true, true, Cond(3), condE)
			var b []byte
			for at := 0; at < padding; at += 4 {
				b = append(b, 0x1f, 0x20, 0x03, 0xd5)
			}
			b = append(b, a.B[:set]...)
			for at := 0; at < gap; at += 4 {
				b = append(b, 0x1f, 0x20, 0x03, 0xd5)
			}
			b = append(b, a.B[set:]...)
			full := append([]byte(nil), b...)
			f := fn{policy: currentCodegenPolicy()}
			f.foldCsetLineage(full, len(full), 0, nil)
			f.foldCsetLineage(b, len(b), padding+set+gap, nil)
			if !bytes.Equal(b, full) {
				t.Fatalf("start boundary changed admission padding=%d gap=%d", padding, gap)
			}
			if gap == 0 && rdWord(b, padding+cmp) != nopWord {
				t.Fatal("bounded start lost a qualifying producer")
			}
		}
	}
}

func TestCsetLineageDirectCallKeepsBooleanAndCallerLocals(t *testing.T) {
	// Keep a saved boolean observable after the select and keep twelve I64
	// locals plus an older stack value live across a real non-inlined call.
	for _, wide := range []bool{false, true} {
		typ, lt, add := wasm.I32, byte(0x49), byte(0x6a)
		if wide {
			typ, lt, add = wasm.I64, 0x54, 0x7c
		}
		caller := []byte{1, 12, 0x7e}
		for i := 0; i < 12; i++ {
			caller = append(caller, 0x20, 2, 0x42)
			caller = append(caller, wasmtest.SLEB64(int64(uint64(i+1)*0x01020304050607))...)
			caller = append(caller, 0x85, 0x21, byte(i+3))
		}
		caller = append(caller, 0x20, 2, 0x20, 0, 0x20, 1, 0x10, 1)
		if !wide {
			caller = append(caller, 0xad)
		}
		caller = append(caller, 0x85)
		for i := 0; i < 12; i++ {
			caller = append(caller, 0x20, byte(i+3), 0x85)
		}
		caller = append(caller, 0x0b)
		callee := []byte{1, 2, 0x7f, 0x20, 0, 0x20, 1, lt, 0x21, 2,
			0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x21, 0,
			0x20, 0, 0x20, 1, lt, 0x21, 3, 0x20, 0, 0x20, 2}
		if wide {
			callee = append(callee, 0xad)
		}
		callee = append(callee, add, 0x20, 3)
		if wide {
			callee = append(callee, 0xad)
		}
		callee = append(callee, add, 0x0b)
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, wasm.I64}, []wasm.ValType{wasm.I64}, caller}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, callee})
		for _, on := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, pair := range [][2]uint64{{0, 1}, {1, 0}, {7, 7}, {0xfeedface00000001, 0xdeadbeef00000002}, {0x80000000, 0xffffffff}, {0x8000000000000000, ^uint64(0)}} {
					x, y := pair[0], pair[1]
					if !wide {
						x, y = uint64(uint32(x)), uint64(uint32(y))
					}
					chosen, c := y, uint64(0)
					if x < y {
						chosen, c = x, 1
					}
					result := chosen + 2*c
					if !wide {
						result = uint64(uint32(result))
					}
					seed := uint64(0xdeadbeef01234567)
					want := seed ^ result
					for i := 0; i < 12; i++ {
						want ^= seed ^ (uint64(i+1) * 0x01020304050607)
					}
					var stats ModuleStats
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"inline": false, "integer-select-local-read": true, "cset-lineage": on}}
					if diagnosticsEnabled && pair == [2]uint64{0, 1} {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, pair[0], pair[1], seed)
					if err != nil || got != want {
						t.Fatalf("wide=%v on=%v guard=%v pair=%x got=%x want=%x err=%v", wide, on, guard, pair, got, want, err)
					}
					if opts.Stats != nil && (stats.Funcs[1].Peephole["cset-lineage"] > 0) != on {
						t.Fatalf("callee admission wide=%v on=%v bounds=%v stats=%v", wide, on, guard, stats.Funcs[1].Peephole)
					}
				}
			}
		}
	}
}
