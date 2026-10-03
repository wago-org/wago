//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
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
// The spec evaluates a table initializer once and stores that reference in
// every slot. An allocating initializer on a multi-entry table was rejected;
// it must yield one object shared by all slots, also after an artifact round
// trip.
func TestGCTableAllocatingInitializerSharesOneObject(t *testing.T) {
	data, err := hex.DecodeString(gcTableAllocMultiWasm)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	reloaded := publicArtifactRoundTrip(t, c)
	defer reloaded.Close()
	for _, compiled := range []*Compiled{c, reloaded} {
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for export, want := range map[string]int32{"same": 1, "shared": 41} {
			got, err := in.InvokeValues(context.Background(), export)
			if err != nil || len(got) != 1 || AsI32(got[0].Bits()) != want {
				in.Close()
				t.Fatalf("%s = %v, %v; want %d", export, got, err, want)
			}
		}
		in.Close()
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
//	  (type $s (struct (field (mut i32))))
//	  (table $many 3 eqref (struct.new $s (i32.const 6)))
//	  (func (export "same") (result i32)
//	    (ref.eq (table.get $many (i32.const 0)) (table.get $many (i32.const 2))))
//	  (func (export "shared") (result i32)
//	    (struct.set $s 0 (ref.cast (ref $s) (table.get $many (i32.const 0))) (i32.const 41))
//	    (struct.get $s 0 (ref.cast (ref $s) (table.get $many (i32.const 1))))))
const gcTableAllocMultiWasm = "0061736d010000000109025f017f016000017f0303020101040c0140006d00034106fb00000b0711020473616d6500000673686172656400010a28020b004100250041022500d30b1a0041002500fb16004129fb05000041012500fb1600fb0200000b0014046e616d6504040100017305070100046d616e79"
