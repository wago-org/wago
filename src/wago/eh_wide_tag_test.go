//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"math"
	"testing"
)

// Exception records held two payload words, so tags with more parameters were
// rejected. Payloads now hold up to eight words through throw, catch routing,
// unwinding across non-matching records, catch_ref/catch_all_ref and throw_ref.
func TestWideExceptionTags(t *testing.T) {
	inst := admissionModule(t, wideExceptionTagWasm)
	for _, export := range []string{"direct", "unwind", "rethrow"} {
		got, err := inst.Invoke(export)
		if err != nil {
			t.Fatalf("%s: %v", export, err)
		}
		if v := AsF64(got[0]); math.Abs(v-54328.875) > 1e-9 {
			t.Fatalf("%s = %v, want 54328.875", export, v)
		}
	}
	got, err := inst.Invoke("all5")
	if err != nil {
		t.Fatal(err)
	}
	if AsI64(got[0]) != 165 {
		t.Fatalf("all5 = %d, want 165", AsI64(got[0]))
	}

	// One word past the record capacity is still rejected at compile time.
	data, err := hex.DecodeString(nineWordTagWasm)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(nil, data); err == nil {
		t.Fatal("nine-word tag compiled, want a bounded exception handling error")
	}
}

// nineWordTagWasm encodes:
//
//	(module
//	  (tag $t (param i32 i32 i32 i32 i32 i32 i32 i32 i32))
//	  (func (export "f")
//	    (throw $t (i32.const 1) (i32.const 2) (i32.const 3) (i32.const 4) (i32.const 5)
//	              (i32.const 6) (i32.const 7) (i32.const 8) (i32.const 9))))
const nineWordTagWasm = "0061736d0100000001100260097f7f7f7f7f7f7f7f7f00600000030201010d03010000070501016600000a1801160041014102410341044105410641074108410908000b000b046e616d650b0401000174"

// wideExceptionTagWasm encodes:
//
//	(module
//	  (tag $w8 (param i32 i64 f32 f64 i32 i64 f64 i32))
//	  (tag $w5 (param i64 i32 f64 i32 i32))
//	  (tag $other (param i32))
//	  ;; sum of the eight payloads as f64
//	  (func $sum8 (param i32 i64 f32 f64 i32 i64 f64 i32) (result f64)
//	    (f64.add (f64.add (f64.add (f64.convert_i32_s (local.get 0)) (f64.convert_i64_s (local.get 1)))
//	                      (f64.add (f64.promote_f32 (local.get 2)) (local.get 3)))
//	             (f64.add (f64.add (f64.convert_i32_s (local.get 4)) (f64.convert_i64_s (local.get 5)))
//	                      (f64.add (local.get 6) (f64.convert_i32_s (local.get 7))))))
//	  (func $thrower
//	    (throw $w8 (i32.const 1) (i64.const 20) (f32.const 0.5) (f64.const 300.25)
//	               (i32.const 4000) (i64.const 50000) (f64.const 0.125) (i32.const 7)))
//	  (func (export "direct") (result f64)
//	    (block $h (result i32 i64 f32 f64 i32 i64 f64 i32)
//	      (try_table (catch $w8 $h) (call $thrower))
//	      (unreachable))
//	    (call $sum8))
//	  ;; unwinds through two non-matching levels before the matching outer catch
//	  (func (export "unwind") (result f64)
//	    (block $h (result i32 i64 f32 f64 i32 i64 f64 i32)
//	      (try_table (catch $w8 $h)
//	        (block $x (result i32)
//	          (try_table (catch $other $x)
//	            (drop (block $y (result i32)
//	              (try_table (catch $other $y) (call $thrower))
//	              (unreachable))))
//	          (unreachable))
//	        (drop))
//	      (unreachable))
//	    (call $sum8))
//	  ;; catch_ref keeps the exception, throw_ref rethrows it later
//	  (func (export "rethrow") (result f64) (local $e exnref)
//	    (block $r (result i32 i64 f32 f64 i32 i64 f64 i32 exnref)
//	      (try_table (catch_ref $w8 $r) (call $thrower))
//	      (unreachable))
//	    (local.set $e) (drop) (drop) (drop) (drop) (drop) (drop) (drop) (drop)
//	    (block $h (result i32 i64 f32 f64 i32 i64 f64 i32)
//	      (try_table (catch $w8 $h) (throw_ref (local.get $e)))
//	      (unreachable))
//	    (call $sum8))
//	  ;; catch_all_ref over a five-payload tag, rethrown and caught precisely
//	  (func (export "all5") (result i64) (local $e exnref) (local $a i32) (local $b i32) (local $c f64) (local $d i32)
//	    (block $r (result exnref)
//	      (try_table (catch_all_ref $r)
//	        (throw $w5 (i64.const 11) (i32.const 22) (f64.const 33.0) (i32.const 44) (i32.const 55)))
//	      (unreachable))
//	    (local.set $e)
//	    (block $h (result i64 i32 f64 i32 i32)
//	      (try_table (catch $w5 $h) (throw_ref (local.get $e)))
//	      (unreachable))
//	    (local.set $a) (local.set $b) (local.set $c) (local.set $d)
//	    (i64.add (i64.extend_i32_s (local.get $a)))
//	    (i64.add (i64.extend_i32_s (local.get $b)))
//	    (i64.add (i64.trunc_f64_s (local.get $c)))
//	    (i64.add (i64.extend_i32_s (local.get $d)))))
const wideExceptionTagWasm = "0061736d01000000014e0a60087f7e7d7c7f7e7c7f0060057e7f7c7f7f0060017f0060087f7e7d7c7f7e7c7f017c6000006000017c6000087f7e7d7c7f7e7c7f6000097f7e7d7c7f7e7c7f696000017e6000057e7f7c7f7f0307060304050505080d070300000001000207240406646972656374000206756e77696e6400030772657468726f77000404616c6c3500050a8202061f002000b72001b9a02002bb2003a0a02004b72005b9a020062007b7a0a0a00b280041014214430000003f440000000000c4724041a01f42d0860344000000000000c03f410708000b110002061f400100000010010b000b10000b290002061f4001000000027f1f4001000200027f1f400100020010010b000b1a0b000b1a0b000b10000b2b01016902071f400101000010010b000b21001a1a1a1a1a1a1a1a02061f400100000020000a0b000b10000b4f040169027f017c017f02691f40010300420b4116440000000000804040412c413708010b000b210002091f400100010020000a0b000b21012102210321042001ac7c2002ac7c2003b07c2004ac7c0b0065046e616d65011002000473756d3801077468726f7765720217020401000165050500016501016102016203016304016403210402010001680303000168020178040179040200017202016805020001720201680b1003000277380102773502056f74686572"
