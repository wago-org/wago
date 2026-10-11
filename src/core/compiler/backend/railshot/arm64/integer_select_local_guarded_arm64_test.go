//go:build (linux || darwin) && arm64 && wago_guardpage

package arm64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func runIntegerSelectGuarded(t *testing.T, m *wasm.Module, opts CompileOptions, args ...uint64) (uint64, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	var jm *rt.JobMemory
	if opts.ElideBoundsChecks {
		if err := rt.InstallGuardTrapHandler(); err != nil {
			t.Fatal(err)
		}
		jm, err = rt.NewJobMemoryGuarded(65536, 65536)
	} else {
		jm, err = rt.NewJobMemory(65536)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	serArgs, results, trap := ar.Alloc(256), ar.Alloc(256), ar.Alloc(rt.TrapBufferBytes)
	for i, arg := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], arg)
	}
	if opts.ElideBoundsChecks {
		err = eng.CallGuarded(entry+uintptr(cm.Entry[0]), serArgs, jm.LinMemBase(), trap, results, jm)
	} else {
		err = eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results)
	}
	return binary.LittleEndian.Uint64(results), err
}

func TestIntegerSelectLocalReadEagerTrapsRemainOrdered(t *testing.T) {
	for _, flags := range []bool{false, true} {
		body := []byte{0, 0x20, 0, 0x20, 1, 0x6d, 0x20, 2, 0x28, 2, 0, 0x20, 3}
		if flags {
			body = append(body, 0x45)
		}
		body = append(body, 0x1b, 0x21, 0, 0x20, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, on := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, tc := range []struct {
					x, d, addr uint64
					trap       rt.TrapCode
				}{{7, 0, 65536, rt.TrapDivZero}, {0x80000000, 0xffffffff, 65536, rt.TrapDivOverflow}, {7, 1, 65536, rt.TrapLinMemOutOfBounds}, {7, 1, 0, 0}} {
					for _, c := range []uint64{0, 1, 2, 1 << 32} {
						got, err := runIntegerSelectGuarded(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"integer-select-local-read": on}}, tc.x, tc.d, tc.addr, c)
						if tc.trap != 0 {
							var te *rt.TrapError
							if !errors.As(err, &te) || te.Code != tc.trap {
								t.Fatalf("flags=%v on=%v bounds=%v case=%v cond=%x trap=%v", flags, on, guard, tc, c, err)
							}
							continue
						}
						active := uint32(c) != 0
						if flags {
							active = !active
						}
						want := uint32(0)
						if active {
							want = uint32(int32(tc.x) / int32(tc.d))
						}
						if err != nil || uint32(got) != want {
							t.Fatalf("flags=%v on=%v bounds=%v cond=%x got=%x want=%x err=%v", flags, on, guard, c, got, want, err)
						}
					}
				}
			}
		}
	}
}
