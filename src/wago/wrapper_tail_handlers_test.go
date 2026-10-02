//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

func tailEHModule(t *testing.T, encoded string) *Instance {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	c := compileStagedExceptionHandling(t, data)
	t.Cleanup(func() { c.Close() })
	inst, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst
}

// A wrapper-ABI tail call made inside a try_table must discard that frame's
// handlers. AMD64 left the record installed, and an exception thrown by the
// tail target unwound into the released frame (SIGSEGV).
func TestWrapperTailCallDiscardsHandlers(t *testing.T) {
	if got := invokeI32(t, tailEHModule(t, ehWrapperTailWasm), "run"); got != 1 {
		t.Fatalf("run = %d, want 1 from the caller's handler", got)
	}
}

// ehWrapperTailWasm encodes:
//
//	(module
//	  (tag $t)
//	  (func $thrower (param v128) (throw $t))
//	  (func $mid (param v128)
//	    (block $h (try_table (catch $t $h) (return_call $thrower (local.get 0)))))
//	  (func (export "run") (result i32)
//	    (block $outer
//	      (try_table (catch $t $outer) (call $mid (v128.const i64x2 0 0)))
//	      (return (i32.const 0)))
//	    (i32.const 1)))
const ehWrapperTailWasm = "0061736d01000000010c0360000060017b006000017f0304030101020d030100000707010372756e00020a3d03040008000b100002401f4001000000200012000b0b0b250002401f4001000000fd0c0000000000000000000000000000000010010b41000f0b41010b002d046e616d65010f0200077468726f77657201036d6964030f020101000168020100056f757465720b0401000174"
