//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGCTableInitializers(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, gcTableNullInitWasm), "f"); got != 8 {
		t.Fatalf("null-initialized GC tables = %d, want 8", got)
	}
	if got := invokeI32(t, admissionModule(t, gcTableExprInitWasm), "f"); got != 30 {
		t.Fatalf("expression-initialized GC tables = %d, want 30", got)
	}
}

// Re-evaluating an allocating initializer per slot would break reference
// identity, so multi-slot tables still reject it, with an explicit limit.
func TestGCTableAllocatingInitializerRejectedClearly(t *testing.T) {
	data, err := hex.DecodeString(gcTableAllocMultiWasm)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(nil, data)
	if err == nil {
		c.Close()
		t.Fatal("compile succeeded; want explicit allocating-initializer limit")
	}
	if !strings.Contains(err.Error(), "allocating GC constant expressions") {
		t.Fatalf("compile error = %v", err)
	}
}

// gcTableNullInitWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (table $a 3 anyref (ref.null any))
//	  (table $n 2 nullexternref (ref.null noextern))
//	  (table $t 2 (ref null $s) (ref.null $s))
//	  (func (export "f") (result i32)
//	    (table.set $a (i32.const 1) (struct.new $s (i32.const 5)))
//	    (i32.add
//	      (i32.add (ref.is_null (table.get $a (i32.const 0))) (ref.is_null (table.get $n (i32.const 1))))
//	      (i32.add (struct.get $s 0 (ref.cast (ref $s) (table.get $a (i32.const 1)))) (ref.is_null (table.get $t (i32.const 0)))))))
const gcTableNullInitWasm = "0061736d010000000109025f017f006000017f03020101041a0340006e0003d06e0b4000720002d0720b400063000002d0000b070501016600000a2a01280041014105fb0000260041002500d141012501d16a41012500fb1600fb02000041002502d16a6a0b0017046e616d65040401000173050a0300016101016e020174"

// gcTableExprInitWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (table $i 3 i31ref (ref.i31 (i32.const 21)))
//	  (table $a 2 anyref (ref.i31 (i32.add (i32.const 1) (i32.const 2))))
//	  (table $one 1 anyref (struct.new $s (i32.const 6)))
//	  (func (export "f") (result i32)
//	    (i32.add (i32.add
//	      (i31.get_s (table.get $i (i32.const 2)))
//	      (i31.get_s (ref.cast (ref i31) (table.get $a (i32.const 1)))))
//	      (struct.get $s 0 (ref.cast (ref $s) (table.get $one (i32.const 0)))))))
const gcTableExprInitWasm = "0061736d010000000109025f017f006000017f0302010104230340006c00034115fb1c0b40006e0002410141026afb1c0b40006e00014106fb00000b070501016600000a20011e0041022500fb1d41012501fb166cfb1d6a41002502fb1600fb0200006a0b0019046e616d65040401000173050c0300016901016102036f6e65"

// gcTableAllocMultiWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (table $many 2 eqref (struct.new $s (i32.const 6)))
//	  (func (export "f") (result i32) (ref.eq (table.get $many (i32.const 0)) (table.get $many (i32.const 1)))))
const gcTableAllocMultiWasm = "0061736d010000000109025f017f006000017f03020101040c0140006d00024106fb00000b070501016600000a0d010b004100250041012500d30b0014046e616d6504040100017305070100046d616e79"
