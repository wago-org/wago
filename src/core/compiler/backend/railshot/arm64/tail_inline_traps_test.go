//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestTailLoopInlineDeferredTrapOrder(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	params := []wasm.ValType{wasm.I32, wasm.I32}
	results := []wasm.ValType{wasm.I32}
	for _, kind := range []string{"single-arg", "prefix", "two-args", "single-rem"} {
		body := []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05}
		switch kind {
		case "single-arg":
			body = append(body, 0x20, 0, 0x41, 0, 0x6e, 0x20, 1)
		case "prefix":
			body = append(body, 0x41, 1, 0x41, 0, 0x6e, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1)
		case "two-args":
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(-2147483648)...)
			body = append(body, 0x41, 0x7f, 0x6d, 0x41, 1, 0x41, 0, 0x6e)
		case "single-rem":
			body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 0, 0x70)
		}
		body = append(body, 0x12, 1, 0x0b, 0x0b)
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{params, results, body})
		opts := CompileOptions{}
		var stats ModuleStats
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, 1, 17)
		if kind == "single-rem" {
			if err != nil || got != 0 {
				t.Fatalf("%s got=%d,%v want 0", kind, got, err)
			}
		} else {
			want := rt.TrapDivZero
			if kind == "two-args" {
				want = rt.TrapDivOverflow
			}
			var trap *rt.TrapError
			if !errors.As(err, &trap) || trap.Code != want {
				t.Fatalf("%s error=%v want %v", kind, err, want)
			}
		}
		if diagnosticsEnabled {
			want := kind == "single-arg" || kind == "single-rem"
			if got := stats.Funcs[0].Peephole["inline-tail-deferred-trap"] != 0; got != want {
				t.Fatalf("%s deferred=%t want %t", kind, got, want)
			}
		}
	}
}

func TestTailLoopInlineGCDDestinationAliasesDivisor(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, eqz, rem, block := wasm.I32, byte(0x45), byte(0x70), byte(0x7f)
		if wide {
			typ, eqz, rem, block = wasm.I64, 0x50, 0x82, 0x7e
		}
		params := []wasm.ValType{typ, typ}
		results := []wasm.ValType{typ}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
			funcDef{params, results, []byte{0, 0x20, 1, eqz, 0x04, block, 0x20, 0, 0x05, 0x20, 1, 0x20, 0, 0x20, 1, rem, 0x12, 1, 0x0b, 0x0b}})
		for _, pair := range [][2]uint64{{0, 0}, {17, 5}, {5, 17}, {0, 31}, {31, 0}, {0xffffffff, 31}, {0x80000000, 0xffffffff}, {0xffffffffffffffff, 0x8000000000000000}} {
			a, b := pair[0], pair[1]
			if !wide {
				a, b = uint64(uint32(a)), uint64(uint32(b))
			}
			aa, bb := a, b
			if !wide {
				aa |= 0xdeadbeef00000000
				bb |= 0x1234567800000000
			}
			for b != 0 {
				a, b = b, a%b
			}
			opts := CompileOptions{}
			var stats ModuleStats
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, aa, bb)
			if err != nil || got != a {
				t.Fatalf("wide=%t pair=%v got=%d,%v want %d", wide, pair, got, err, a)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-div-rem-destination"] == 0 {
				t.Fatal("aliased remainder destination was not exercised")
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-divisor-known-nonzero"] == 0 {
				t.Fatal("dominated divisor check remains")
			}
		}
	}
}

func TestTailLoopInlineDivisionDestinationsAndSignedTraps(t *testing.T) {
	if profileEnabled {
		t.Skip("experimental tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, block := wasm.I32, byte(0x7f)
		min, minusOne := uint64(0x80000000), uint64(0xffffffff)
		if wide {
			typ, block, min, minusOne = wasm.I64, 0x7e, 1<<63, ^uint64(0)
		}
		params := []wasm.ValType{wasm.I32, typ, typ}
		results := []wasm.ValType{typ}
		for _, op := range divOps(wide) {
			m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}},
				funcDef{params, results, []byte{0, 0x20, 0, 0x45, 0x04, block, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, op.opcode, 0x20, 2, 0x12, 1, 0x0b, 0x0b}})
			for _, input := range [][3]uint64{{0, 17, 0}, {1, 17, 5}, {2, minusOne, 3}, {1, min, minusOne}, {1, 7, 0}, {2, min, 2}} {
				n, a, d := input[0], input[1], input[2]
				want := a
				trapCode := rt.TrapCode(0)
				for range n {
					if d == 0 {
						trapCode = rt.TrapDivZero
						break
					}
					if op.signed && !op.rem && want == min && d == minusOne {
						trapCode = rt.TrapDivOverflow
						break
					}
					want = refDivRem(op, wide, want, d)
				}
				opts := CompileOptions{}
				var stats ModuleStats
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, a, d)
				if trapCode != 0 {
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != trapCode {
						t.Fatalf("%s input=%v error=%v want %v", op.name, input, err, trapCode)
					}
				} else if err != nil || got != want {
					t.Fatalf("%s input=%v got=%d,%v want %d", op.name, input, got, err, want)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-div-rem-destination"] == 0 {
					t.Fatal("aliased division destination was not exercised")
				}
			}
		}
	}
}
