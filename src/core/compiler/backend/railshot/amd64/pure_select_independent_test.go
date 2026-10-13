//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func TestPureSelectIndependentCapturedMemory(t *testing.T) {
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	// The load commits through tee. The condition stores to the same address.
	// Both select arms must use the captured value, then observe the new store.
	body := []byte{0,
		0x20, 0, 0x28, 2, 0, 0x22, 0,
		0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c,
		0x20, 0,
		0x41, 0, 0x20, 2, 0x36, 2, 0,
		0x20, 1, 0x1b,
		0x41, 0, 0x28, 2, 0, 0x6a, 0x0b,
	}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		pureSelectEnabled = enabled
		for _, guard := range []bool{false, true} {
			for _, condition := range []uint64{0, 1, 0xffffffff} {
				got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, func(mem []byte) {
					binary.LittleEndian.PutUint32(mem, 31)
				}, 0, condition, 63)
				want := uint64(31 + 63)
				if condition != 0 {
					want = (31*17^3)*19 + 63
				}
				if err != nil || uint32(got) != uint32(want) || binary.LittleEndian.Uint32(mem) != 63 {
					t.Fatalf("enabled=%v guard=%v cond=%x: got=%x want=%x stored=%d err=%v", enabled, guard, condition, got, want, binary.LittleEndian.Uint32(mem), err)
				}
			}
		}
	}
}

func TestPureSelectIndependentMemoryTrapOrder(t *testing.T) {
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	for _, cheapLoad := range []bool{false, true} {
		body := []byte{0, 0x41, 0, 0x41, 29, 0x36, 2, 0}
		if !cheapLoad {
			body = append(body, 0x20, 0, 0x28, 2, 0)
		} else {
			body = append(body, 0x20, 1)
		}
		body = append(body, 0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c)
		if cheapLoad {
			body = append(body, 0x20, 0, 0x28, 2, 0)
		} else {
			body = append(body, 0x20, 1)
		}
		// The memory trap must precede this condition's divide-by-zero.
		body = append(body, 0x20, 1, 0x41, 0, 0x6e, 0x1b, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			pureSelectEnabled = enabled
			// runMemAmd64WithOptions uses arena-backed memory. It does not
			// provide the protected mapping needed for an OOB guard-mode test.
			_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 65536, 7)
			var trap *runtime.TrapError
			if !errors.As(err, &trap) || trap.Code != runtime.TrapLinMemOutOfBounds || binary.LittleEndian.Uint32(mem) != 29 {
				t.Fatalf("cheapLoad=%v enabled=%v: trap=%v stored=%d", cheapLoad, enabled, err, binary.LittleEndian.Uint32(mem))
			}
		}
	}
}

func TestPureSelectIndependentBranchJoinAndSpills(t *testing.T) {
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		pureSelectEnabled = enabled
		for _, wide := range []bool{false, true} {
			typ, constant, mul, xor, add := wasm.I32, byte(0x41), byte(0x6c), byte(0x73), byte(0x6a)
			if wide {
				typ, constant, mul, xor, add = wasm.I64, 0x42, 0x7e, 0x85, 0x7c
			}
			for _, pressure := range []int{0, 24} {
				body := []byte{0}
				// Keep distinct computed values live through both select paths and
				// through the Wasm if join. These must retain their own spill slots.
				for i := range pressure {
					body = append(body, 0x20, 0, constant, byte(i), add)
				}
				body = append(body, 0x20, 2, 0x04, byte(typ.Num()))
				for _, salt := range []byte{3, 5} {
					if salt == 5 {
						body = append(body, 0x05)
					}
					body = append(body, 0x20, 0, 0x20, 1, xor, constant, 17, mul, constant, salt, xor, constant, 19, mul,
						0x20, 0, 0x20, 3, 0x1b)
				}
				body = append(body, 0x0b)
				for range pressure {
					body = append(body, add)
				}
				body = append(body, 0x0b)
				m := mod1(t, []wasm.ValType{typ, typ, wasm.I32, wasm.I32}, []wasm.ValType{typ}, body)
				t.Run(fmt.Sprintf("enabled=%v/wide=%v/pressure=%d", enabled, wide, pressure), func(t *testing.T) {
					for _, x := range []uint64{0, 7, 0x80000000, 0xffffffffffffffff} {
						for _, branch := range []uint64{0, 1} {
							for _, condition := range []uint64{0, 1} {
								salt := uint64(5)
								if branch != 0 {
									salt = 3
								}
								want := x
								if condition != 0 {
									want = ((x^11)*17 ^ salt) * 19
								}
								for i := range pressure {
									want += x + uint64(i)
								}
								if !wide {
									want = uint64(uint32(want))
								}
								if got := runAmd64u(t, m, x, 11, branch, condition); got != want {
									t.Fatalf("x=%x branch=%d cond=%d: got=%x want=%x", x, branch, condition, got, want)
								}
							}
						}
					}
				})
			}
		}
	}
}
