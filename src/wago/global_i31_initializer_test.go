//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

// A global ref.i31 initializer whose operand is extended-constant arithmetic
// compiled, then failed instantiation as "compiled metadata invalid": the
// instance-side evaluator only accepted `global.get; ref.i31`, and an eqref or
// anyref global in a collector-free module was validated as a scalar. Both
// collector-free and collector-backed modules must evaluate these, also after
// an artifact round trip.
func TestGlobalI31InitializerArithmetic(t *testing.T) {
	for name, module := range map[string]string{"collector-free": globalI31ArithmeticWasm, "collector-backed": globalI31ArithmeticGCWasm} {
		data, err := hex.DecodeString(module)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := Compile(nil, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		reloaded := publicArtifactRoundTrip(t, compiled)
		for _, c := range []*Compiled{compiled, reloaded} {
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatalf("%s: instantiate: %v", name, err)
			}
			if got := invokeI32(t, in, "f"); got != 400703 {
				t.Fatalf("%s: f = %d, want 400703", name, got)
			}
			in.Close()
		}
		reloaded.Close()
		compiled.Close()
	}
}

// globalI31ArithmeticWasm encodes:
//
//	(module
//	  (global $k i32 (i32.const 5))
//	  (global $a i31ref (ref.i31 (i32.add (i32.const 1) (i32.const 2))))
//	  (global $b eqref (ref.i31 (i32.sub (i32.const 10) (i32.const 3))))
//	  (global $c anyref (ref.i31 (i32.mul (global.get $k) (i32.const 8))))
//	  (func (export "f") (result i32)
//	    (i32.add (i32.add
//	      (i31.get_s (ref.as_non_null (global.get $a)))
//	      (i32.mul (i31.get_s (ref.cast (ref i31) (global.get $b))) (i32.const 100)))
//	      (i32.mul (i31.get_s (ref.cast (ref i31) (global.get $c))) (i32.const 10000)))))
const globalI31ArithmeticWasm = "0061736d010000000105016000017f030201000624047f0041050b6c00410141026afb1c0b6d00410a41036bfb1c0b6e00230041086cfb1c0b070501016600000a220120002301d4fb1d2302fb166cfb1d41e4006c6a2303fb166cfb1d4190ce006c6a0b0014046e616d65070d0400016b010161020162030163"

// globalI31ArithmeticGCWasm is globalI31ArithmeticWasm plus a struct type and
// a struct global, which selects collector-backed GC execution.
const globalI31ArithmeticGCWasm = "0061736d010000000109025f017f006000017f03020101062d056400004101fb00000b7f0041050b6c00410141026afb1c0b6d00410a41036bfb1c0b6e00230141086cfb1c0b070501016600000a220120002302d4fb1d2303fb166cfb1d41e4006c6a2304fb166cfb1d4190ce006c6a0b001d046e616d6504040100017307100500016f01016b020161030162040163"
