//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"math"
	"testing"
)

// A register-ABI caller may tail-call a result-bearing wrapper-ABI target
// directly or through a table. AMD64 rejected these at compile time; repeated
// register/wrapper tail cycles must also stay stack-bounded.
func TestRegisterCallerTailCallsWrapperTarget(t *testing.T) {
	for _, tc := range []struct {
		module string
		export string
		want   []float64
		args   []Value
	}{
		{registerToWrapperTailWasm, "a_i32", []float64{14}, []Value{ValueI32(4)}},
		{registerToWrapperTailWasm, "n_mix", []float64{58.5}, nil},
		{registerToWrapperTailWasm, "n_fi", []float64{4.5}, nil},
		{registerToWrapperTailWasm, "n_ff", []float64{4.25}, nil},
		{registerToWrapperTailWasm, "loop", []float64{1000001}, nil},
		{registerToWrapperTailWasm, "loop_adapter", []float64{1000001}, []Value{ValueI32(0)}},
		{registerToWrapperTailWasm, "loop_nested", []float64{1000002}, nil},
		{registerToWrapperIndirectTailWasm, "nested", []float64{29}, nil},
		{registerToWrapperIndirectTailWasm, "entry", []float64{105, 0.5}, nil},
		{registerToWrapperIndirectTailWasm, "loop", []float64{1000001}, nil},
	} {
		inst := admissionModule(t, tc.module)
		vals, err := inst.InvokeValues(context.Background(), tc.export, tc.args...)
		if err != nil {
			t.Fatalf("%s: %v", tc.export, err)
		}
		if len(vals) != len(tc.want) {
			t.Fatalf("%s returned %d values, want %d", tc.export, len(vals), len(tc.want))
		}
		for i, v := range vals {
			var got float64
			switch v.Type() {
			case ValI32:
				got = float64(AsI32(v.Bits()))
			case ValI64:
				got = float64(AsI64(v.Bits()))
			case ValF32:
				got = float64(AsF32(v.Bits()))
			case ValF64:
				got = AsF64(v.Bits())
			}
			if math.Abs(got-tc.want[i]) > 1e-9 {
				t.Fatalf("%s result %d = %v, want %v", tc.export, i, got, tc.want[i])
			}
		}
	}
}

// registerToWrapperTailWasm encodes:
//
//	(module
//	  (global $g (mut i32) (i32.const 0))
//	  ;; wrapper-ABI targets (v128 param), various result shapes
//	  (func $w_i32 (param i32 v128) (result i32) (i32.add (local.get 0) (i32x4.extract_lane 1 (local.get 1))))
//	  (func $w_i64 (param i32 v128) (result i64) (i64.extend_i32_s (i32.mul (local.get 0) (i32.const 3))))
//	  (func $w_f64 (param i32 v128) (result f64) (f64.convert_i32_s (local.get 0)))
//	  (func $w_if (param i32 v128) (result i32 f64) (local.get 0) (f64.const 2.5))
//	  (func $w_fi (param i32 v128) (result f32 i32) (f32.const 1.5) (local.get 0))
//	  (func $w_ff (param i32 v128) (result f64 f64) (f64.const 0.25) (f64.const 4.0))
//	  ;; register-ABI tail callers
//	  (func $t_i32 (param i32) (result i32) (return_call $w_i32 (local.get 0) (i32x4.splat (i32.const 10))))
//	  (func $t_i64 (param i32) (result i64) (return_call $w_i64 (local.get 0) (v128.const i64x2 0 0)))
//	  (func $t_f64 (param i32) (result f64) (return_call $w_f64 (local.get 0) (v128.const i64x2 0 0)))
//	  (func $t_if (param i32) (result i32 f64) (return_call $w_if (local.get 0) (v128.const i64x2 0 0)))
//	  (func $t_fi (param i32) (result f32 i32) (return_call $w_fi (local.get 0) (v128.const i64x2 0 0)))
//	  (func $t_ff (param i32) (result f64 f64) (return_call $w_ff (local.get 0) (v128.const i64x2 0 0)))
//	  ;; exports: adapter-entered path
//	  (func (export "a_i32") (param i32) (result i32) (return_call $t_i32 (local.get 0)))
//	  ;; nested path: called from another register function, results consumed in registers
//	  (func (export "n_mix") (result f64)
//	    (local $x i32) (local $y f64)
//	    (f64.add (f64.add
//	      (f64.convert_i32_s (call $t_i32 (i32.const 5)))
//	      (f64.convert_i64_s (call $t_i64 (i32.const 7))))
//	      (f64.add (call $t_f64 (i32.const 9))
//	        (block (result f64)
//	          (call $t_if (i32.const 11)) (local.set $y) (local.set $x)
//	          (f64.add (local.get $y) (f64.convert_i32_s (local.get $x)))))))
//	  (func (export "n_fi") (result f32) (local $i i32)
//	    (call $t_fi (i32.const 3)) (local.set $i) (f32.add (f32.convert_i32_s (local.get $i))))
//	  (func (export "n_ff") (result f64) (call $t_ff (i32.const 0)) f64.add)
//	  ;; mutual tail loop register <-> wrapper: must stay stack-bounded
//	  (func $ra (param i32) (result i32)
//	    (global.set $g (i32.add (global.get $g) (i32.const 1)))
//	    (if (result i32) (i32.eqz (local.get 0)) (then (global.get $g))
//	      (else (return_call $wb (i32.sub (local.get 0) (i32.const 1)) (v128.const i64x2 0 0)))))
//	  (func $wb (param i32 v128) (result i32) (return_call $ra (local.get 0)))
//	  (func (export "loop") (result i32) (call $ra (i32.const 1000000)))
//	  (func $ra_entry (export "loop_adapter") (param i32) (result i32) (return_call $ra (i32.const 1000000)))
//	  (func (export "loop_nested") (result i32) (i32.add (call $ra_entry (i32.const 0)) (i32.const 1))))
const registerToWrapperTailWasm = "0061736d0100000001550f60027f7b017f60027f7b017e60027f7b017c60027f7b027f7c60027f7b027d7f60027f7b027c7c60017f017f60017f017e60017f017c60017f027f7c60017f027d7f60017f027c7c6000017c6000017d6000017f031615000102030405060708090a0b060c0d0c06000e060e0606017f0141000b07430705615f693332000c056e5f6d6978000d046e5f6669000e046e5f6666000f046c6f6f7000120c6c6f6f705f6164617074657200130b6c6f6f705f6e657374656400140ae602150a0020002001fd1b016a0b0800200041036cac0b05002000b70b0d0020004400000000000004400b0900430000c03f20000b140044000000000000d03f4400000000000010400b0a002000410afd1112000b18002000fd0c0000000000000000000000000000000012010b18002000fd0c0000000000000000000000000000000012020b18002000fd0c0000000000000000000000000000000012030b18002000fd0c0000000000000000000000000000000012040b18002000fd0c0000000000000000000000000000000012050b0600200012060b2802017f017c41051006b741071007b9a041091008027c410b10092101210020012000b7a00ba0a00b0e01017f4103100a21002000b2920b07004100100ba00b2b00230041016a2400200045047f230005200041016bfd0c0000000000000000000000000000000012110b0b0600200012100b080041c0843d10100b080041c0843d12100b09004100101341016a0b007e046e616d6501610f0005775f6933320105775f6936340205775f6636340304775f69660404775f66690504775f66660605745f6933320705745f6936340805745f6636340904745f69660a04745f66690b04745f66661002726111027762130872615f656e747279020e020d020001780101790e01000169070401000167"

// registerToWrapperIndirectTailWasm encodes:
//
//	(module
//	  (type $wt (func (param i32 v128) (result i32 f64)))
//	  (type $wl (func (param i32 v128) (result i32)))
//	  (table $t 4 funcref)
//	  (elem (table $t) (i32.const 0) func $w0 $w1 $wb)
//	  (global $g (mut i32) (i32.const 0))
//	  (func $w0 (param i32 v128) (result i32 f64) (i32.add (local.get 0) (i32.const 100)) (f64.const 0.5))
//	  (func $w1 (param i32 v128) (result i32 f64) (i32.mul (local.get 0) (i32.const 3)) (f64.const 8.0))
//	  (func $sel (param i32 i32) (result i32 f64)
//	    (return_call_indirect $t (type $wt) (local.get 1) (v128.const i64x2 0 0) (local.get 0)))
//	  (func (export "nested") (result f64) (local $x i32) (local $y f64)
//	    (call $sel (i32.const 1) (i32.const 7)) (local.set $y) (local.set $x)
//	    (f64.add (local.get $y) (f64.convert_i32_s (local.get $x))))
//	  (func (export "entry") (result i32 f64) (return_call $sel (i32.const 0) (i32.const 5)))
//	  (func $ra (param i32) (result i32)
//	    (global.set $g (i32.add (global.get $g) (i32.const 1)))
//	    (if (result i32) (i32.eqz (local.get 0)) (then (global.get $g))
//	      (else (return_call_indirect $t (type $wl) (i32.sub (local.get 0) (i32.const 1)) (v128.const i64x2 0 0) (i32.const 2)))))
//	  (func $wb (param i32 v128) (result i32) (return_call $ra (local.get 0)))
//	  (func (export "loop") (result i32) (call $ra (i32.const 1000000))))
const registerToWrapperIndirectTailWasm = "0061736d0100000001270760027f7b027f7c60027f7b017f60027f7f027f7c6000017c6000027f7c60017f017f6000017f03090800000203040501060404017000040606017f0141000b071903066e6573746564000305656e7472790004046c6f6f700007090b01020041000b00030001060a9f01081100200041e4006a44000000000000e03f0b1000200041036c4400000000000020400b1b002001fd0c0000000000000000000000000000000020001300000b1602017f017c4101410710022101210020012000b7a00b08004100410512020b2e00230041016a2400200045047f230005200041016bfd0c0000000000000000000000000000000041021301000b0b0600200012050b080041c0843d10050b003f046e616d650116050002773001027731020373656c05027261060277620209010302000178010179040902000277740102776c050401000174070401000167"
