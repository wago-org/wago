//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo

package wago

import (
	"math/bits"
	"math/rand"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func wideArithmeticModule(sub byte, pressure bool) []byte {
	count := 2
	if sub < 21 {
		count = 4
	}
	params := make([]wasm.ValType, count)
	results := []wasm.ValType{wasm.I64, wasm.I64}
	body := []byte{}
	if pressure {
		// Eight values remain live below the arithmetic operands and across the
		// fixed RAX/RDX multiplication pair. Repeat parameters to exercise aliases.
		results = make([]wasm.ValType, 10)
		for i := range results {
			results[i] = wasm.I64
		}
		for i := 0; i < 8; i++ {
			body = append(body, 0x20, byte(i%count))
		}
	}
	for i := range params {
		params[i] = wasm.I64
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0xfc, sub, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("wide", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func wideArithmeticExpected(sub byte, args []uint64) (lo, hi uint64) {
	switch sub {
	case 19:
		var carry uint64
		lo, carry = bits.Add64(args[0], args[2], 0)
		hi, _ = bits.Add64(args[1], args[3], carry)
	case 20:
		var borrow uint64
		lo, borrow = bits.Sub64(args[0], args[2], 0)
		hi, _ = bits.Sub64(args[1], args[3], borrow)
	case 21, 22:
		hi, lo = bits.Mul64(args[0], args[1])
		if sub == 21 {
			if int64(args[0]) < 0 {
				hi -= args[1]
			}
			if int64(args[1]) < 0 {
				hi -= args[0]
			}
		}
	}
	return
}

func TestWideArithmeticExecutionAndCodec(t *testing.T) {
	edge := []uint64{0, 1, 2, 1<<32 - 1, 1 << 32, 1<<63 - 1, 1 << 63, ^uint64(0) - 1, ^uint64(0)}
	for sub := byte(19); sub <= 22; sub++ {
		for _, pressure := range []bool{false, true} {
			name := []string{"add128", "sub128", "mul_wide_s", "mul_wide_u"}[sub-19]
			if pressure {
				name += "/pressure"
			}
			t.Run(name, func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureWideArithmetic).WithBoundsChecks(BoundsChecksExplicit)
				compiled, err := cfg.Compile(wideArithmeticModule(sub, pressure))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				if !compiled.requiredFeatures.IsEnabled(CoreFeatureWideArithmetic) {
					t.Fatalf("unrecorded wide arithmetic: %s", compiled.requiredFeatures)
				}
				blob, err := compiled.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				var loaded Compiled
				if err := loaded.UnmarshalBinary(blob); err != nil {
					t.Fatal(err)
				}
				defer loaded.Close()
				for _, product := range []*Compiled{compiled, &loaded} {
					in, err := Instantiate(product, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					rng := rand.New(rand.NewSource(37))
					for i := 0; i < 500; i++ {
						args := []uint64{rng.Uint64(), rng.Uint64()}
						if sub < 21 {
							args = append(args, rng.Uint64(), rng.Uint64())
						}
						if i < len(edge)*len(edge) {
							args[0], args[1] = edge[i%len(edge)], edge[i/len(edge)]
							if sub < 21 {
								args[2], args[3] = args[1], args[0]
							}
						}
						lo, hi := wideArithmeticExpected(sub, args)
						result, err := in.Invoke("wide", args...)
						if err != nil {
							t.Fatal(err)
						}
						off := 0
						if pressure {
							off = 8
							for j := 0; j < off; j++ {
								if result[j] != args[j%len(args)] {
									t.Fatalf("clobbered live operand %d", j)
								}
							}
						}
						if len(result) != off+2 || result[off] != lo || result[off+1] != hi {
							t.Fatalf("args %x: got %x, want %x:%x", args, result, hi, lo)
						}
					}
				}
			})
		}
	}
}

func TestWideArithmeticFeatureAdmission(t *testing.T) {
	if CoreFeaturesV3.IsEnabled(CoreFeatureWideArithmetic) {
		t.Fatal("wide arithmetic is outside Release 3")
	}
	for sub := byte(19); sub <= 22; sub++ {
		_, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(wideArithmeticModule(sub, false))
		if err == nil || !strings.Contains(err.Error(), "wide-arithmetic disabled") {
			t.Fatalf("disabled opcode %d: %v", sub, err)
		}
	}
}

func TestWideArithmeticPreservesOperandTrapOrder(t *testing.T) {
	// The first operand overflows signed division; the second divides by zero.
	// Deferred expression materialization must keep the first observable trap.
	body, err := wasm.EncodeExpr(wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrI64Const, I64: -1 << 63}, {Kind: wasm.InstrI64Const, I64: -1}, {Kind: wasm.InstrI64DivS},
		{Kind: wasm.InstrI64Const}, {Kind: wasm.InstrI64Const}, {Kind: wasm.InstrI64DivU},
		{Kind: wasm.InstrI64MulWideU},
	}})
	if err != nil {
		t.Fatal(err)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("wide", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := NewRuntimeConfig().Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	_, err = instance.Invoke("wide")
	trap, ok := err.(*TrapError)
	if !ok || trap.Code != TrapDivOverflow {
		t.Fatalf("trap order = %v, want division overflow", err)
	}
}
