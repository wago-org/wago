//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIntegerSelectReadOldAliasesAndConditions(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, add, lt := wasm.I32, byte(0x6a), byte(0x48)
		if wide {
			typ, add, lt = wasm.I64, 0x7c, 0x53
		}
		for _, form := range []string{"ordinary", "set", "tee", "flags"} {
			body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x20, 2, 0x21, 3, 0x20, 0, 0x20, 0, 0x20, 1}
			if form == "flags" {
				body = append(body, 0x20, 0, 0x20, 1, lt)
			} else {
				body = append(body, 0x20, 3)
			}
			body = append(body, 0x1b)
			if form == "set" {
				body = append(body, 0x21, 0, 0x20, 0)
			} else if form == "tee" {
				body = append(body, 0x22, 0)
			}
			body = append(body, add, 0x0b)
			m := modMem(t, 1, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
			for _, on := range []bool{false, true} {
				for _, guard := range []bool{false, true} {
					for _, pair := range [][2]uint64{{0, 7}, {1, 2}, {0x80000000, 0xffffffff}, {0x8000000000000000, ^uint64(0)}, {0xfeedface00000001, 0xdeadbeef00000007}, {^uint64(0), 1}} {
						for _, condition := range []uint64{0, 1, 2, 0xffffffff, 1 << 32, (1 << 32) | 1} {
							x, y := pair[0], pair[1]
							chosen := y
							active := uint32(condition) != 0
							if form == "flags" {
								if wide {
									active = int64(x) < int64(y)
								} else {
									active = int32(x) < int32(y)
								}
							}
							if active {
								chosen = x
							}
							want := x + chosen
							opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-read": on}}
							var stats ModuleStats
							if diagnosticsEnabled && pair == [2]uint64{0, 7} && condition == 0 {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, x, y, condition)
							if !wide {
								got, want = uint64(uint32(got)), uint64(uint32(want))
							}
							if err != nil || got != want {
								t.Fatalf("wide=%v form=%s on=%v bounds=%v pair=%x cond=%x got=%x want=%x err=%v", wide, form, on, guard, pair, condition, got, want, err)
							}
							if opts.Stats != nil && (stats.Funcs[0].Peephole["integer-select-read"] > 0) != on {
								t.Fatalf("missing admission wide=%v form=%s on=%v stats=%v", wide, form, on, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestIntegerSelectReadWideRegionalPressure(t *testing.T) {
	// More than64 declared locals force packed/sparse ownership and regional
	// eviction. Every local remains observable after a destination-aliasing tee.
	for _, wide := range []bool{false, true} {
		typ, constant, add, decl := wasm.I32, byte(0x41), byte(0x6a), byte(0x7f)
		if wide {
			typ, constant, add, decl = wasm.I64, 0x42, 0x7c, 0x7e
		}
		body := []byte{1, 80, decl, 0x3f, 0, 0x1a}
		for i := 0; i < 80; i++ {
			body = append(body, 0x20, 0, constant)
			body = append(body, wasmtest.SLEB64(int64(i+1))...)
			body = append(body, add, 0x21, byte(i+3))
		}
		body = append(body, 0x20, 3, 0x20, 3, 0x20, 82, 0x20, 2, 0x1b, 0x22, 3, add)
		for i := 0; i < 80; i++ {
			body = append(body, 0x20, byte(i+3), add)
		}
		body = append(body, 0x0b)
		m := modMem(t, 1, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
		for _, on := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 7, 0x80000000, 0xfffffffffffffff0} {
					for _, c := range []uint64{0, 1, 2, 1 << 32} {
						old := x + 1
						selected := x + 80
						if uint32(c) != 0 {
							selected = old
						}
						want := old + selected
						for i := 0; i < 80; i++ {
							if i == 0 {
								want += selected
							} else {
								want += x + uint64(i+1)
							}
						}
						got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-read": on}}, x, 0, c)
						if !wide {
							got, want = uint64(uint32(got)), uint64(uint32(want))
						}
						if err != nil || got != want {
							t.Fatalf("wide=%v on=%v bounds=%v x=%x cond=%x got=%x want=%x err=%v", wide, on, guard, x, c, got, want, err)
						}
					}
				}
			}
		}
	}
}

func TestIntegerSelectReadDeferredWidthAndOwnedCondition(t *testing.T) {
	// An arithmetic condition is owned and can become the output when both
	// branches are borrowed. Deferred I64 branches must keep all64 bits.
	for _, arithmeticBranches := range []bool{false, true} {
		body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 0}
		if arithmeticBranches {
			body = append(body, 0x42, 1, 0x7c)
		}
		body = append(body, 0x20, 1)
		if arithmeticBranches {
			body = append(body, 0x42, 1, 0x7d)
		}
		body = append(body, 0x20, 2, 0x41, 1, 0x6a, 0x1b, 0x7c, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I64, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, body)
		for _, on := range []bool{false, true} {
			for _, borrow := range []bool{false, true} {
				for _, guard := range []bool{false, true} {
					for _, x := range []uint64{0, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
						for _, c := range []uint64{0, 0xffffffff, 1 << 32, (1 << 32) | 0xffffffff} {
							y := ^x
							a, b := x, y
							if arithmeticBranches {
								a++
								b--
							}
							chosen := b
							if uint32(c)+1 != 0 {
								chosen = a
							}
							opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-read": on, "select-source-read": borrow}}
							got, err := runArm64WrapperWithOptions(t, m, opts, x, y, c)
							if err != nil || got != x+chosen {
								t.Fatalf("arithmetic=%v on=%v borrow=%v bounds=%v x=%x c=%x got=%x want=%x err=%v", arithmeticBranches, on, borrow, guard, x, c, got, x+chosen, err)
							}
						}
					}
				}
			}
		}
	}
}

func TestIntegerSelectReadCachedConstantOwnership(t *testing.T) {
	const k = uint32(0x12345678)
	// A multiply makes k eligible for a loop constant lease. The select must
	// read that cached register without turning it into the writable output.
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x41}
	body = append(body, wasmtest.SLEB32(int32(k))...)
	body = append(body, 0x6c, 0x21, 0, 0x20, 0, 0x41)
	body = append(body, wasmtest.SLEB32(int32(k))...)
	body = append(body, 0x20, 2, 0x1b, 0x21, 3, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0x20, 0, 0x6a, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		for _, borrow := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, c := range []uint64{0, 1, 2, 1 << 32} {
					x := uint32(7)
					for range 3 {
						x *= k
					}
					chosen := k
					if uint32(c) != 0 {
						chosen = x
					}
					var stats ModuleStats
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-read": on, "select-source-read": borrow, "loop-int-const": true}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, 7, 3, c)
					if err != nil || uint32(got) != x+chosen {
						t.Fatalf("on=%v borrow=%v bounds=%v cond=%x got=%x want=%x err=%v", on, borrow, guard, c, got, x+chosen, err)
					}
					if opts.Stats != nil && stats.Funcs[0].Peephole["loop-int-const"] == 0 {
						t.Fatalf("constant was not cached: %v", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}

func TestIntegerSelectReadTypedReferenceResult(t *testing.T) {
	// Typed externref select is also lowered through the integer carrier path.
	// Its64-bit payload and GC-root classification must survive the new result.
	body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 1, 0x20, 2, 0x1c, 1, 0x6f, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.ExternRef, wasm.ExternRef, wasm.I32}, []wasm.ValType{wasm.ExternRef}, body)
	for _, on := range []bool{false, true} {
		for _, borrow := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, c := range []uint64{0, 1, 2, 1 << 32} {
					x, y := uint64(0xfeedface12345678), uint64(0xdeadbeef98765432)
					want := y
					if uint32(c) != 0 {
						want = x
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-read": on, "select-source-read": borrow}}, x, y, c)
					if err != nil || got != want {
						t.Fatalf("on=%v borrow=%v bounds=%v c=%x got=%x want=%x err=%v", on, borrow, guard, c, got, want, err)
					}
				}
			}
		}
	}
}

func TestIntegerSelectReadComparisonBranchIsI32(t *testing.T) {
	// The deferred branch node carries an I64 comparison input width, while
	// its result and the alternate branch are I32. An X CSEL would leak the
	// alternate's dirty upper bits through the following unsigned extension.
	for _, eqz := range []bool{false, true} {
		for _, flags := range []bool{false, true} {
			body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0}
			if eqz {
				body = append(body, 0x50)
			} else {
				body = append(body, 0x20, 1, 0x51)
			}
			body = append(body, 0x20, 2, 0x20, 3)
			if flags {
				body = append(body, 0x45)
			}
			body = append(body, 0x1b, 0xad, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I64, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
			// The candidate must emit W CSEL for these I32 branches. The
			// unsigned extension can mask a too-wide select, so inspect the
			// actual native instruction as well as executing the result.
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"integer-select-read": true}})
			if err != nil {
				t.Fatal(err)
			}
			selects := 0
			for i := 0; i+4 <= len(cm.Code); i += 4 {
				w := binary.LittleEndian.Uint32(cm.Code[i:])
				if w&0x7fe00c00 == 0x1a800000 {
					selects++
					if w>>31 != 0 {
						t.Fatalf("eqz=%v flags=%v emitted X CSEL for I32 branches at %x", eqz, flags, i)
					}
				}
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if selects == 0 {
				t.Fatalf("eqz=%v flags=%v no native CSEL", eqz, flags)
			}
			for _, on := range []bool{false, true} {
				for _, borrow := range []bool{false, true} {
					for _, guard := range []bool{false, true} {
						for _, x := range []uint64{0, 1, 1 << 32, ^uint64(0)} {
							for _, y := range []uint64{0, 1, 1 << 32} {
								for _, c := range []uint64{0, 1, 2, 1 << 32} {
									truth := x == y
									if eqz {
										truth = x == 0
									}
									active := uint32(c) != 0
									if flags {
										active = !active
									}
									want := uint64(0x98765432)
									if active {
										want = 0
										if truth {
											want = 1
										}
									}
									got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-read": on, "select-source-read": borrow}}, x, y, 0xdeadbeef98765432, c)
									if err != nil || got != want {
										t.Fatalf("eqz=%v flags=%v on=%v borrow=%v bounds=%v x=%x y=%x c=%x got=%x want=%x err=%v", eqz, flags, on, borrow, guard, x, y, c, got, want, err)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestIntegerSelectReadPreservesCollectorRoot(t *testing.T) {
	// The execution fixture validates the reference payload. This checks the
	// collector metadata, including when the reused output comes from the
	// other branch and is not itself a root before the select.
	for _, rootBranch := range []int{-1, 0, 1} {
		f := fn{a: &a64.Asm{}, s: newStack(), policy: currentCodegenPolicy()}
		a := f.pushReg(X0, mtI64)
		a.st.setGCRoot(rootBranch == 0)
		b := f.pushReg(X1, mtI64)
		b.st.setGCRoot(rootBranch == 1)
		c := f.pushReg(X2, mtI32)
		if !f.tryIntegerSelectRead(c, false) {
			t.Fatal("reference carriers were declined")
		}
		result := f.s.back()
		if f.depth() != 1 || result.st.typ != mtI64 || result.st.hasGCRoot() != (rootBranch >= 0) {
			t.Fatalf("root branch %d result=%+v depth=%d", rootBranch, result.st, f.depth())
		}
	}
}
