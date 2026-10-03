//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

// Exception payloads were limited to scalars and non-null function
// references. GC references must survive collections while they sit in a
// caught payload, in a catch_ref root slot, and in a catch_all_ref root slot
// whose tags disagree on which payload words are references.
func TestGCReferenceExceptionPayloads(t *testing.T) {
	data, err := hex.DecodeString(gcExceptionPayloadWasm)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if status := compiled.GCNativeRootAdmission(); !status.Required || !status.Exact {
		t.Fatalf("root admission = %+v", status)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for export, want := range map[string]uint64{"direct": 42, "held": 7, "mixed": 105} {
		got, err := in.Invoke(export)
		if err != nil || len(got) != 1 || uint32(got[0]) != uint32(want) {
			t.Fatalf("%s = %v, %v; want %d", export, got, err, want)
		}
		if stats := in.gc.Stats(); stats.FullCollections == 0 {
			t.Fatalf("%s ran without a collection", export)
		}
	}
}

// gcExceptionPayloadWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (tag $t (param (ref $s) i32))
//	  (tag $n (param i32))
//	  (tag $a (param anyref))
//	  (func $churn (local $i i32)
//	    (loop $l
//	      (drop (struct.new $s (local.get $i)))
//	      (local.set $i (i32.add (local.get $i) (i32.const 1)))
//	      (br_if $l (i32.lt_u (local.get $i) (i32.const 200)))))
//	  (func $thrower (param $v i32)
//	    (call $churn)
//	    (throw $t (struct.new $s (local.get $v)) (i32.const 1)))
//	  ;; payload delivered on the operand stack, then a collection
//	  (func (export "direct") (result i32) (local $r (ref null $s))
//	    (block $h (result (ref $s) i32)
//	      (try_table (catch $t $h) (call $thrower (i32.const 41)))
//	      (unreachable))
//	    (drop)
//	    (local.set $r)
//	    (call $churn)
//	    (i32.add (struct.get $s 0 (local.get $r)) (i32.const 1)))
//	  ;; payload held only in a catch_ref root slot across collections
//	  (func (export "held") (result i32) (local $e exnref)
//	    (block $r (result (ref $s) i32 exnref)
//	      (try_table (catch_ref $t $r) (call $thrower (i32.const 7)))
//	      (unreachable))
//	    (local.set $e) (drop) (drop)
//	    (call $churn)
//	    (block $h (result (ref $s) i32)
//	      (try_table (catch $t $h) (throw_ref (local.get $e)))
//	      (unreachable))
//	    (drop)
//	    (struct.get $s 0))
//	  ;; catch_all_ref root slot sees both a scalar tag and a reference tag
//	  (func $mixed (param $k i32) (result i32) (local $e exnref)
//	    (block $r (result exnref)
//	      (try_table (catch_all_ref $r)
//	        (if (local.get $k)
//	          (then (throw $a (struct.new $s (i32.const 100))))
//	          (else (throw $n (i32.const 5)))))
//	      (unreachable))
//	    (local.set $e)
//	    (call $churn)
//	    (block $hn (result i32)
//	      (block $ha (result anyref)
//	        (try_table (catch $a $ha) (catch $n $hn) (throw_ref (local.get $e)))
//	        (unreachable))
//	      (struct.get $s 0 (ref.cast (ref $s))))
//	    )
//	  (func (export "mixed") (result i32)
//	    (i32.add (call $mixed (i32.const 0)) (call $mixed (i32.const 1)))))
const gcExceptionPayloadWasm = "0061736d01000000012c095f017f00600264007f0060017f0060016e006000006000017f60000264007f60000364007f6960017f017f0307060402050508050d07030001000200030719030664697265637400020468656c640003056d6978656400050ac801061c01017f03402000fb00001a200041016a2100200041c801490d000b0b0d0010002000fb0000410108000b220101630002061f4001000000412910010b000b1a210010002000fb02000041016a0b2c01016902071f4001010000410710010b000b21001a1a100002061f400100000020000a0b000b1afb0200000b3f01016902691f400103002000044041e400fb0000080205410508010b0b000b21011000027f026e1f400200020000010120010a0b000bfb1600fb0200000b0b0b0041001004410110046a0b0072046e616d650118030005636875726e01077468726f77657204056d69786564021d050001000169010100017602010001720301000165040200016b010165032004000100016c0201000168030200017202016804030001720302686e040268610404010001730b0a0300017401016e020161"
