//go:build amd64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep the first load's address live after its arithmetic consumer so this
// exercises the delayed memory fold, including aliasing and trap barriers.
func TestDraglineAMD64LateFloatMemoryFoldSemantics(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, width := range []int{4, 8} {
		typ, load, store, add, mul := wasm.F32, byte(0x2a), byte(0x38), byte(0x92), byte(0x94)
		values := []uint64{0, 0x80000000, 0x3f800000, 0xbf800000, 0x3fa00000, 0x7f800000, 0xff800000, 0x7fc00000, 0x7f800001, 1, 0x7f7fffff}
		if width == 8 {
			typ, load, store, add, mul = wasm.F64, 0x2b, 0x39, 0xa0, 0xa2
			values = []uint64{0, 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000, 0x3ff4000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000000, 0x7ff0000000000001, 1, 0x7fefffffffffffff}
		}
		for op := byte(0); op < 4; op++ {
			for _, barrier := range []string{"reads", "store", "division"} {
				for _, mode := range modes {
					t.Run(fmt.Sprintf("f%d/op=%d/%s/bounds=%v", width*8, op, barrier, mode), func(t *testing.T) {
						body := []byte{0x20, 0, load, 0, 0}
						switch barrier {
						case "store":
							body = append(body, 0x41, 0, 0x20, 2, store, 0, 0)
						case "division":
							body = append(body, 0x41, 1, 0x41, 0, 0x6e, 0x1a)
						}
						body = append(body, 0x20, 2, 0x20, 1, load, 0, 0, mul, add+op)
						// The marker must execute only after both reads and the division.
						body = append(body, 0x20, 0, 0x41, 63, 0x71, 0x41, 42, 0x3a, 0, 0, 0x0b)
						source := wasmtest.Module(
							wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, typ}, []wasm.ValType{typ}))),
							wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
							wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
							wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
							wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
						)
						makeInstance := func(compiler CompilerEngine, bounds BoundsCheckMode) *Instance {
							t.Helper()
							compiled, err := Compile(NewRuntimeConfig().WithCompiler(compiler).WithTarget(TargetNative).WithBoundsChecks(bounds), source)
							if err != nil {
								t.Fatal(err)
							}
							t.Cleanup(func() { compiled.Close() })
							instance, err := Instantiate(compiled, InstantiateOptions{})
							if err != nil {
								t.Fatal(err)
							}
							t.Cleanup(func() { instance.Close() })
							return instance
						}
						// Explicit checks retain the first read's trap before division.
						ref := makeInstance(CompilerRailshot, BoundsChecksExplicit)
						candidate := makeInstance(CompilerDragline, mode)
						refMem, gotMem := ref.Memory().UnsafeBytes(), candidate.Memory().UnsafeBytes()
						write := func(address int, value uint64) {
							if width == 4 {
								binary.LittleEndian.PutUint32(refMem[address:], uint32(value))
								return
							}
							binary.LittleEndian.PutUint64(refMem[address:], value)
						}
						read := func(address uint64) uint64 {
							if address+uint64(width) > uint64(len(refMem)) {
								return 0
							}
							if width == 4 {
								return uint64(binary.LittleEndian.Uint32(refMem[address:]))
							}
							return binary.LittleEndian.Uint64(refMem[address:])
						}
						for _, a := range values {
							for _, b := range values {
								for _, coefficient := range []uint64{values[2], values[4], values[8]} {
									for _, pointers := range [][2]uint64{{0, 16}, {16, 0}, {0, 0}, {1, 2}, {65528, 16}, {65535, 16}, {0, 65535}, {65535, 65535}} {
										clear(refMem)
										write(0, a)
										write(16, b)
										write(65528, a)
										copy(gotMem, refMem)
										lhs := read(pointers[0])
										if barrier == "store" {
											write(0, coefficient)
										}
										rhs := read(pointers[1])
										copy(refMem, gotMem)
										want, wantErr := ref.Invoke("run", pointers[0], pointers[1], coefficient)
										got, gotErr := candidate.Invoke("run", pointers[0], pointers[1], coefficient)
										var wantTrap, gotTrap *TrapError
										if wantErr != nil {
											if !errors.As(wantErr, &wantTrap) || !errors.As(gotErr, &gotTrap) || wantTrap.Code != gotTrap.Code {
												t.Fatalf("pointers=%v: trap=%v, want %v", pointers, gotErr, wantErr)
											}
										} else if gotErr != nil || len(got) != 1 || len(want) != 1 || !draglineFoldFloatEqual(width, got[0], want[0], lhs, rhs, coefficient) {
											t.Fatalf("pointers=%v a=%x b=%x coefficient=%x: got %x,%v; want %x", pointers, a, b, coefficient, got, gotErr, want)
										}
										if !bytes.Equal(gotMem, refMem) {
											t.Fatalf("pointers=%v: memory differs after invocation", pointers)
										}
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

func draglineFoldFloatEqual(width int, got, want uint64, inputs ...uint64) bool {
	mask, infinity, quiet := uint64(0x7fffffff), uint64(0x7f800000), uint64(0x400000)
	if width == 8 {
		mask, infinity, quiet = 0x7fffffffffffffff, 0x7ff0000000000000, 0x8000000000000
	}
	if want&mask <= infinity {
		return got == want
	}
	if got&mask <= infinity || got&quiet == 0 {
		return false
	}
	for _, input := range inputs {
		if input&mask > infinity && input&mask != infinity|quiet {
			return true // Arithmetic NaNs may preserve a noncanonical payload.
		}
	}
	return got&mask == infinity|quiet
}
