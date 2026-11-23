//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestScalarFloatSelectBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, flags := range []bool{false, true} {
			typ, tofloat, toint := wasm.I32, byte(0xbe), byte(0xbc)
			if wide {
				typ, tofloat, toint = wasm.I64, 0xbf, 0xbd
			}
			body := []byte{0, 0x20, 0, tofloat, 0x20, 1, tofloat, 0x20, 2}
			if flags {
				body = append(body, 0x41, 7, 0x49)
			}
			body = append(body, 0x1b, toint, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-fcsel"] != 0) != flags {
				t.Fatalf("wide=%v flags=%v missing FCSEL: %v", wide, flags, stats.Funcs[0].Peephole)
			}
			vals := []uint64{0, 0x80000000, 0x7fc12345, 0xffc54321, 0x7f812345, 1, 0x7f800000}
			if wide {
				vals = []uint64{0, 0x8000000000000000, 0x7ff8000000001234, 0xfff8000000004321, 0x7ff0000000001234, 1, 0x7ff0000000000000}
			}
			for _, a := range vals {
				for _, b := range vals {
					for _, cond := range []uint64{0, 1, 2, 7, 0xffffffff, 1 << 32, (1 << 32) | 1} {
						want := b
						active := uint32(cond) != 0
						if flags {
							active = uint32(cond) < 7
						}
						if active {
							want = a
						}
						got, e := runArm64WrapperWithOptions(t, m, opts, a, b, cond)
						if !wide {
							got = uint64(uint32(got))
						}
						if e != nil || got != want {
							t.Fatalf("wide=%v flags=%v a=%x b=%x c=%x got=%x want=%x err=%v", wide, flags, a, b, cond, got, want, e)
						}
					}
				}
			}
		}
	}
}

func TestScalarFloatSelectSpillAndTypedBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, typed := range []bool{false, true} {
			for _, flags := range []bool{false, true} {
				ft, it, c, add, reinterpret := wasm.F32, wasm.I32, byte(0x43), byte(0x92), byte(0xbc)
				if wide {
					ft, it, c, add, reinterpret = wasm.F64, wasm.I64, 0x44, 0xa0, 0xbd
				}
				typeByte := byte(0x7d)
				if wide {
					typeByte = 0x7c
				}
				body := []byte{1, 1, typeByte}
				for i := 0; i < 40; i++ {
					body = append(body, 0x20, 0, c)
					if wide {
						body = append(body, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f)
					} else {
						body = append(body, 0, 0, 0x80, 0x3f)
					}
					body = append(body, add)
				}
				body = append(body, 0x20, 0, 0x20, 1, 0x20, 2)
				if flags {
					body = append(body, 0x41, 0, 0x47)
				}
				if typed {
					body = append(body, 0x1c, 1, typeByte)
				} else {
					body = append(body, 0x1b)
				}
				body = append(body, 0x21, 3)
				for i := 0; i < 40; i++ {
					body = append(body, 0x1a)
				}
				body = append(body, 0x20, 3, reinterpret, 0x0b)
				m := mod1(t, []wasm.ValType{ft, ft, wasm.I32}, []wasm.ValType{it}, body)
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-fcsel-flags"] != 0) != flags {
					t.Fatalf("flags=%v typed=%v counters=%v", flags, typed, stats.Funcs[0].Peephole)
				}
				a, b := uint64(0x7f812345), uint64(0x80000000)
				if wide {
					a, b = 0x7ff0000000001234, 0x8000000000000000
				}
				for _, cond := range []uint64{0, 1, 0xffffffff} {
					want := b
					if cond != 0 {
						want = a
					}
					got, e := runArm64WrapperWithOptions(t, m, opts, a, b, cond)
					if !wide {
						got = uint64(uint32(got))
					}
					if e != nil || got != want {
						t.Fatalf("wide=%v typed=%v cond=%x got=%x want=%x err=%v", wide, typed, cond, got, want, e)
					}
				}
			}
		}
	}
}

func TestScalarFloatSelectEagerTraps(t *testing.T) {
	for _, wide := range []bool{false, true} {
		convert, load, align, reinterpret, result := byte(0xb2), byte(0x2a), byte(2), byte(0xbc), wasm.I32
		if wide {
			convert, load, align, reinterpret, result = 0xb7, 0x2b, 3, 0xbd, wasm.I64
		}
		body := []byte{0, 0x20, 0, 0x20, 1, 0x6d, convert, 0x20, 2, load, align, 0, 0x20, 3, 0x45, 0x1b, reinterpret, 0x0b}
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{result}, body)
		for _, tc := range []struct {
			x, d, addr uint64
			trap       rt.TrapCode
		}{{7, 0, 65536, rt.TrapDivZero}, {0x80000000, 0xffffffff, 65536, rt.TrapDivOverflow}, {7, 1, 65536, rt.TrapLinMemOutOfBounds}} {
			for _, c := range []uint64{0, 1, 1 << 32} {
				_, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, tc.x, tc.d, tc.addr, c)
				var te *rt.TrapError
				if !errors.As(err, &te) || te.Code != tc.trap {
					t.Fatalf("wide=%v case=%v cond=%x trap=%v", wide, tc, c, err)
				}
			}
		}
	}
}
