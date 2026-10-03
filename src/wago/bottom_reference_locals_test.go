//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"
)

func TestNonNullableBottomReferenceLocals(t *testing.T) {
	inst := admissionModule(t, bottomLocalsWasm)
	if got := invokeI32(t, inst, "locals"); got != 3 {
		t.Fatalf("locals = %d, want 3", got)
	}
	if got := invokeI32(t, inst, "unreach"); got != 5 {
		t.Fatalf("unreach = %d, want 5", got)
	}
	if _, err := inst.Invoke("trap"); err == nil {
		t.Fatal("trap: want null-reference trap")
	}
}

// bottomLocalsWasm encodes:
//
//	(module
//	  (func $takes (param (ref noextern)) (result i32) i32.const 9)
//	  (func (export "locals") (result i32)
//	    (local (ref noextern)) (local (ref none)) (local (ref nofunc))
//	    (local $n (ref null noextern))
//	    (block $done (result i32)
//	      i32.const 3
//	      local.get $n
//	      br_on_null $done
//	      call $takes
//	      drop))
//	  (func (export "unreach") (result i32)
//	    (local $x (ref none))
//	    i32.const 0
//	    if (result i32)
//	      (local.set $x (ref.as_non_null (ref.null none)))
//	      (ref.is_null (local.get $x))
//	    else
//	      i32.const 5
//	    end)
//	  (func (export "trap") (result i32)
//	    (local $x (ref noextern))
//	    (local.set $x (ref.as_non_null (ref.null noextern)))
//	    (ref.is_null (local.get $x))))
const bottomLocalsWasm = "0061736d01000000010b0260016472017f6000017f03050400010101071b03066c6f63616c73000107756e72656163680002047472617000030a4404040041090b19040164720164710164730172027f41032003d50010001a0b0b15010164714100047fd071d421002000d10541050b0b0d01016472d072d421002000d10b002c046e616d65010801000574616b6573021003010103016e0201000178030100017803090101010004646f6e65"
