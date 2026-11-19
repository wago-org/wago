//go:build (((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage) || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage)

package wago

import "testing"

// Exception and nullexternref tables are instantiated with 8-byte entries, but
// the amd64/arm64 table lowering used the 32-byte funcref descriptor stride for
// them. table.grow then wrote past the table storage: amd64 silently zeroed the
// following funcref table (size 0, call_indirect out of bounds) and arm64
// faulted. Non-null exnrefs stored through table.set/fill also went through the
// funcref descriptor snapshot and could not be read back.
func TestExnrefTableGrowKeepsAdjacentTable(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, exnrefTableGrowAdjacentWasm), "f"); got != 100011 {
		t.Fatalf("f = %d, want 100011", got)
	}
}

func TestExnrefTableBulkOperationsRoundTrip(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, exnrefTableBulkOpsWasm), "ops"); got != 58 {
		t.Fatalf("ops = %d, want 58", got)
	}
}

// exnrefTableGrowAdjacentWasm encodes:
//
//	(module
//	  (table $e 0 exnref)
//	  (table $f 4 funcref)
//	  (func $k (result i32) (i32.const 7))
//	  (elem (table $f) (i32.const 0) func $k $k $k $k)
//	  (func (export "f") (result i32)
//	    (drop (table.grow $e (ref.null exn) (i32.const 1000)))
//	    (i32.add
//	      (i32.mul (table.size $e) (i32.const 100))
//	      (i32.add (table.size $f) (call_indirect $f (result i32) (i32.const 3))))))
const exnrefTableGrowAdjacentWasm = "0061736d010000000105016000017f030302000004070269000070000407050101660001090c01020141000b0004000000000a2302040041070b1c00d06941e807fc0f001afc100041e4006cfc100141031100016a6a0b0014046e616d6501040100016b050702000165010166"

// exnrefTableBulkOpsWasm encodes:
//
//	(module
//	  (tag $t (param i32))
//	  (table $e 8 exnref)
//	  (table $f 2 funcref)
//	  (func $k (result i32) i32.const 9)
//	  (elem (table $f) (i32.const 0) func $k $k)
//	  (elem $pe exnref (ref.null exn) (ref.null exn))
//	  (func (export "ops") (result i32)
//	    (local $x exnref)
//	    (block $h (result i32 exnref)
//	      (try_table (catch_ref $t $h)
//	        i32.const 42
//	        throw $t)
//	      unreachable)
//	    local.set $x
//	    drop
//	    (table.fill $e (i32.const 0) (local.get $x) (i32.const 4))
//	    (table.copy $e $e (i32.const 5) (i32.const 1) (i32.const 3))
//	    (table.init $e $pe (i32.const 2) (i32.const 0) (i32.const 2))
//	    (drop (table.grow $e (local.get $x) (i32.const 600)))
//	    ;; e[0,1]=x e[2,3]=null e[4]=null e[5..7]=x e[8..607]=x
//	    (i32.add
//	      (i32.add
//	        (i32.add (ref.is_null (table.get $e (i32.const 2))) (i32.mul (i32.const 2) (ref.is_null (table.get $e (i32.const 1)))))
//	        (i32.add (i32.mul (i32.const 4) (ref.is_null (table.get $e (i32.const 4)))) (i32.mul (i32.const 8) (ref.is_null (table.get $e (i32.const 607))))))
//	      (i32.add
//	        (block $h2 (result i32)
//	          (try_table (catch $t $h2)
//	            (throw_ref (table.get $e (i32.const 7))))
//	          unreachable)
//	        (i32.add (table.size $f) (call_indirect $f (result i32) (i32.const 1)))))))
const exnrefTableBulkOpsWasm = "0061736d01000000010e0360017f006000017f6000027f6903030201010407026900087000020d03010000070701036f70730001091302020141000b00020000056902d0690bd0690b0a7f02040041090b7801016902021f4001010000412a08000b000b21001a410020004104fc1100410541014103fc0e0000410241004102fc0c0100200041d804fc0f001a41022500d1410241012500d16c6a410441042500d16c410841df042500d16c6a6a027f1f4001000000410725000a0b000bfc100141011101016a6a6a0b0035046e616d6501040100016b0206010101000178030a01010200016802026832050702000165010166080501010270650b0401000174"
