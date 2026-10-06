//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A signed remainder may finish before an observable effect and a later
// overflowing division. Computing the quotient at the remainder would move
// the trap ahead of that observation; such windows must remain untouched.
func TestDivRemPairEffectTrapOrderAMD64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, rev := range []bool{false, true} {
			for _, gap := range []string{"effect", "callback"} {
				for _, zero := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%v/remfirst=%v/%s/zero=%v", wide, rev, gap, zero), func(t *testing.T) {
						f := divRemPairFixture{Wide: wide, Signed: true, RemFirst: rev, Gap: gap}
						raw := f.Module()
						c, e := Compile(nil, raw)
						if e != nil {
							t.Fatal(e)
						}
						defer c.Close()
						calls := 0
						im := NewImports()
						im.HostFunc("env", "observe", func(HostCall) { calls++ })
						in, e := Instantiate(c, InstantiateOptions{Imports: im})
						if e != nil {
							t.Fatal(e)
						}
						defer in.Close()
						min, minus := uint64(1<<31), uint64(0xffffffff)
						if wide {
							min, minus = 1<<63, ^uint64(0)
						}
						trap := TrapDivOverflow
						want := uint64(0)
						if rev {
							want = 1
						}
						if zero {
							minus = 0
							trap = TrapDivZero
							want = 0
						}
						_, e = in.Invoke("run", min, minus, 0)
						var te *TrapError
						if !errors.As(e, &te) || te.Code != trap {
							t.Fatal(e, trap)
						}
						observed, e := in.GlobalValue("observed")
						if e != nil {
							t.Fatal(e)
						}
						if gap == "callback" {
							if uint64(calls) != want {
								t.Fatal(calls, want)
							}
						} else if observed.Bits() != want {
							t.Fatal(observed, want)
						}
					})
				}
			}
		}
	}
}

func divRemPairPrefixModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 2, 0x20, 3, 0x7f, // earlier, independently trapping division
			0x20, 0, 0x20, 1, 0x7f, // paired division
			0x20, 0, 0x20, 1, 0x81, // paired remainder
			0x0b,
		}))),
	)
}

func TestDivRemPairEarlierTrapAMD64(t *testing.T) {
	_, in := compileDivRemPair(t, divRemPairPrefixModule())
	for _, tc := range []struct {
		args [4]uint64
		trap TrapCode
	}{
		{[4]uint64{1, 0, 1 << 63, ^uint64(0)}, TrapDivOverflow},
		{[4]uint64{1 << 63, ^uint64(0), 1, 0}, TrapDivZero},
	} {
		got, e := in.Invoke("run", tc.args[:]...)
		var trap *TrapError
		if !errors.As(e, &trap) || trap.Code != tc.trap {
			t.Fatal(tc, got, e)
		}
	}
	got, e := in.Invoke("run", 100, 7, 51, 4)
	if e != nil || len(got) != 3 || got[0] != 12 || got[1] != 14 || got[2] != 2 {
		t.Fatal(got, e)
	}
}
