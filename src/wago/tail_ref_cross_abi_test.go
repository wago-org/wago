//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

// return_call_ref between register-ABI and wrapper-ABI signatures: AMD64
// rejected any caller or target type outside the register ABI. A wrapper
// caller must reach internal, local-wrapper and foreign targets, a register
// caller must reach wrapper-only types, and a million mixed cycles must stay
// stack-bounded.
func TestReturnCallRefAcrossABIs(t *testing.T) {
	for _, module := range []string{returnCallRefABIWasm, returnCallRefABIEscapedWasm} {
		inst := admissionModule(t, module)
		for _, tc := range []struct {
			export string
			args   []Value
			want   int32
		}{
			{"w2r", nil, 106},
			{"r2w", []Value{ValueI32(3)}, 10},
			{"w2w", nil, 12},
			{"loop_r", nil, 1234},
			{"loop_w", nil, 1234},
		} {
			got, err := inst.InvokeValues(nil, tc.export, tc.args...)
			if err != nil || len(got) != 1 || AsI32(got[0].Bits()) != tc.want {
				t.Fatalf("%s = %v, %v; want %d", tc.export, got, err, tc.want)
			}
		}
	}
}

// A wrapper-ABI caller's foreign tail target returns to the caller's caller,
// whose instance registers (linear memory, globals) must be restored.
func TestReturnCallRefWrapperCallerForeignTarget(t *testing.T) {
	producer := admissionModule(t, returnCallRefABIProducerWasm)
	f, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	g, err := producer.ExportedFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	data, err := hex.DecodeString(returnCallRefABIConsumerWasm)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	consumer, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.f", f, "env.g", g)})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	got, err := consumer.InvokeValues(nil, "cross")
	if err != nil || len(got) != 1 || uint32(got[0].Bits()) != 3529314597 {
		t.Fatalf("cross = %v, %v; want 3529314597", got, err)
	}
}

// returnCallRefABIWasm encodes:
//
//	(module
//	  (type $r (func (param i32) (result i32)))
//	  (type $w (func (param v128 i32) (result i32)))
//	  (elem declare func $reg $wrap $wl $rl)
//	  (func $reg (type $r) (i32.add (local.get 0) (i32.const 100)))
//	  (func $wrap (type $w) (i32.add (i32x4.extract_lane 1 (local.get 0)) (local.get 1)))
//	  ;; wrapper-ABI caller -> register-ABI (internal) target
//	  (func $w2r (param v128 i32) (result i32)
//	    (return_call_ref $r (i32.add (i32x4.extract_lane 0 (local.get 0)) (local.get 1)) (ref.func $reg)))
//	  (func (export "w2r") (result i32) (call $w2r (i32x4.splat (i32.const 5)) (i32.const 1)))
//	  ;; register-ABI caller -> wrapper-only target type
//	  (func (export "r2w") (param i32) (result i32)
//	    (return_call_ref $w (i32x4.splat (local.get 0)) (i32.const 7) (ref.func $wrap)))
//	  ;; wrapper-ABI caller -> wrapper-ABI target
//	  (func $w2w (param v128 i32) (result i32)
//	    (return_call_ref $w (local.get 0) (i32.add (local.get 1) (i32.const 1)) (ref.func $wrap)))
//	  (func (export "w2w") (result i32) (call $w2w (i32x4.splat (i32.const 9)) (i32.const 2)))
//	  ;; a million wrapper/register tail cycles must stay stack-bounded
//	  (func $wl (type $w)
//	    (if (result i32) (i32.eqz (local.get 1))
//	      (then (i32.add (i32x4.extract_lane 2 (local.get 0)) (i32.const 1234)))
//	      (else (return_call_ref $r (i32.sub (local.get 1) (i32.const 1)) (ref.func $rl)))))
//	  (func $rl (type $r)
//	    (return_call_ref $w (i32x4.splat (local.get 0)) (local.get 0) (ref.func $wl)))
//	  (func (export "loop_r") (result i32) (call $rl (i32.const 1000000)))
//	  (func (export "loop_w") (result i32) (call $wl (i32x4.splat (i32.const 3)) (i32.const 1000000))))
const returnCallRefABIWasm = "0061736d0100000001100360017f017f60027b7f017f6000017f030c0b0001010200010201000202072505037732720003037232770004037732770006066c6f6f705f720009066c6f6f705f77000a090801030004000107080a94010b0800200041e4006a0b0a002000fd1b0120016a0b0e002000fd1b0020016ad20015000b0a004105fd11410110020b0c002000fd114107d20115010b0d002000200141016ad20115010b0a004109fd11410210050b1b00200145047f2000fd1b0241d2096a05200141016bd20815000b0b0c002000fd112000d20715010b080041c0843d10080b0c004103fd1141c0843d10070b002e046e616d65011e060003726567010477726170020377327205037732770702776c0802726c040702000172010177"

// returnCallRefABIEscapedWasm is returnCallRefABIWasm with the functions in an
// exported table, so every descriptor uses a wrapper entry kind.
const returnCallRefABIEscapedWasm = "0061736d0100000001100360017f017f60027b7f017f6000017f030c0b0001010200010201000202040401700004072b06037461620100037732720003037232770004037732770006066c6f6f705f720009066c6f6f705f77000a090a010041000b04000107080a94010b0800200041e4006a0b0a002000fd1b0120016a0b0e002000fd1b0020016ad20015000b0a004105fd11410110020b0c002000fd114107d20115010b0d002000200141016ad20115010b0a004109fd11410210050b1b00200145047f2000fd1b0241d2096a05200141016bd20815000b0b0c002000fd112000d20715010b080041c0843d10080b0c004103fd1141c0843d10070b002e046e616d65011e060003726567010477726170020377327205037732770702776c0802726c040702000172010177"

// returnCallRefABIProducerWasm encodes:
//
//	(module
//	  (memory 1)
//	  (global $g (mut i32) (i32.const 40))
//	  (func (export "f") (param v128 i32) (result i32)
//	    (global.set $g (i32.add (global.get $g) (i32.const 1)))
//	    (i32.add (i32.add (i32x4.extract_lane 3 (local.get 0)) (local.get 1)) (global.get $g)))
//	  (func (export "g") (param i32) (result i32)
//	    (i32.store (i32.const 0) (local.get 0))
//	    (i32.mul (i32.load (i32.const 0)) (i32.const 3))))
const returnCallRefABIProducerWasm = "0061736d01000000010c0260027b7f017f60017f017f030302000105030100010606017f0141280b07090201660000016700010a28021400230041016a24002000fd1b0320016a23006a0b110041002000360200410028020041036c0b000b046e616d65070401000167"

// returnCallRefABIConsumerWasm encodes:
//
//	(module
//	  (type $r (func (param i32) (result i32)))
//	  (type $w (func (param v128 i32) (result i32)))
//	  (import "env" "f" (func $f (type $w)))
//	  (import "env" "g" (func $g (type $r)))
//	  (memory 1)
//	  (global $mine (mut i32) (i32.const 1000))
//	  (elem declare func $f $g)
//	  (func $w2f (param v128 i32) (result i32)
//	    (return_call_ref $w (local.get 0) (local.get 1) (ref.func $f)))
//	  (func $w2g (param v128 i32) (result i32)
//	    (return_call_ref $r (i32x4.extract_lane 0 (local.get 0)) (ref.func $g)))
//	  ;; after each foreign tail the caller's own instance state must be intact
//	  (func (export "cross") (result i32) (local $i i32) (local $acc i32)
//	    (i32.store (i32.const 8) (i32.const 77))
//	    (loop $l
//	      (local.set $acc (i32.add (local.get $acc)
//	        (call $w2f (i32x4.splat (local.get $i)) (i32.const 2))))
//	      (local.set $acc (i32.add (local.get $acc)
//	        (call $w2g (i32x4.splat (local.get $i)) (i32.const 0))))
//	      (global.set $mine (i32.add (global.get $mine) (i32.const 1)))
//	      (local.set $i (i32.add (local.get $i) (i32.const 1)))
//	      (br_if $l (i32.lt_u (local.get $i) (i32.const 100000))))
//	    (i32.add (local.get $acc)
//	      (i32.add (global.get $mine) (i32.load (i32.const 8))))))
const returnCallRefABIConsumerWasm = "0061736d0100000001100360017f017f60027b7f017f6000017f02110203656e760166000103656e760167000003040301010205030100010607017f0141e8070b0709010563726f7373000409060103000200010a64030a0020002001d20015010b0b002000fd1b00d20115000b4b01027f410841cd00360200034020012000fd11410210026a210120012000fd11410010036a2101230041016a2400200041016a2100200041a08d06490d000b2001230041082802006a6a0b003f046e616d6501110400016601016702037732660303773267020b0104020001690103616363030601040100016c04070200017201017707070100046d696e65"
