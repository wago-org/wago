//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

// any.convert_extern of a host externref yields an opaque 64-bit anyref word.
// AMD64's native defined-type test/cast stubs inspected only its low half, so
// ref.test randomly trapped with "cast failure" (and the low half could read
// as null). Foreign anyrefs never match a defined struct/array type: tests
// return 0 and casts trap, for every opaque word.
func TestForeignAnyrefDefinedTypeChecks(t *testing.T) {
	data, err := hex.DecodeString(foreignAnyrefDefinedTypeWasm)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	ctx := context.Background()
	for range 64 {
		inst, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		host, err := inst.NewExternRef("host")
		if err != nil {
			t.Fatal(err)
		}
		arg := ValueExternRef(host)
		for _, export := range []string{"test", "test_null"} {
			got, err := inst.InvokeValues(ctx, export, arg)
			if err != nil || AsI32(got[0].Bits()) != 0 {
				inst.Close()
				t.Fatalf("%s = %v, %v; want 0", export, got, err)
			}
		}
		for _, export := range []string{"cast", "cast_null", "alen"} {
			if _, err := inst.InvokeValues(ctx, export, arg); err == nil || !strings.Contains(err.Error(), "cast failure") {
				inst.Close()
				t.Fatalf("%s error = %v, want cast failure", export, err)
			}
		}
		inst.Close()
	}
}

// foreignAnyrefDefinedTypeWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (type $a (array i32))
//	  (func (export "test") (param externref) (result i32)
//	    (ref.test (ref $s) (any.convert_extern (local.get 0))))
//	  (func (export "test_null") (param externref) (result i32)
//	    (ref.test (ref null $s) (any.convert_extern (local.get 0))))
//	  (func (export "cast") (param externref) (result i32)
//	    (struct.get $s 0 (ref.cast (ref $s) (any.convert_extern (local.get 0)))))
//	  (func (export "cast_null") (param externref) (result i32)
//	    (ref.is_null (ref.cast (ref null $s) (any.convert_extern (local.get 0)))))
//	  (func (export "alen") (param externref) (result i32)
//	    (array.len (ref.cast (ref $a) (any.convert_extern (local.get 0)))))
//	  (func (export "mk") (result i32)
//	    (i32.add (struct.get $s 0 (struct.new $s (i32.const 1)))
//	             (array.len (array.new_default $a (i32.const 2))))))
const foreignAnyrefDefinedTypeWasm = "0061736d010000000111045f017f005e7f0060016f017f6000017f0307060202020202030733060474657374000009746573745f6e756c6c00010463617374000209636173745f6e756c6c000304616c656e0004026d6b00050a4e0609002000fb1afb14000b09002000fb1afb15000b0d002000fb1afb1600fb0200000b0a002000fb1afb1700d10b0b002000fb1afb1601fb0f0b13004101fb0000fb0200004102fb0701fb0f6a0b000e046e616d65040702000173010161"
