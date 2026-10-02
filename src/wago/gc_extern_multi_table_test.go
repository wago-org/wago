//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"testing"
)

// Extern conversions only instantiated with one reference table (or one fixed
// three-table layout): any other layout failed with "invalid mixed-table
// layout". Converted objects and host externrefs must stay reachable through
// any mix of anyref and externref tables across collections.
func TestGCExternConversionAcrossReferenceTables(t *testing.T) {
	data, err := hex.DecodeString(gcExternMultiTableWasm)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	inst, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	ctx := context.Background()
	host, err := inst.NewExternRef("hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.InvokeValues(ctx, "store", ValueExternRef(host)); err != nil {
		t.Fatalf("store: %v", err)
	}
	if got := invokeI32(t, inst, "sum"); got != 545 {
		t.Fatalf("sum = %d, want 545", got)
	}
	out, err := inst.InvokeValues(ctx, "host")
	if err != nil || len(out) != 1 {
		t.Fatalf("host = %v, %v", out, err)
	}
	if value, ok := inst.ExternRefValue(out[0].ExternRef()); !ok || value != "hello" {
		t.Fatalf("host externref = %v, %v; want hello", value, ok)
	}
	if stats := inst.gc.Stats(); stats.FullCollections == 0 {
		t.Fatal("no collection ran")
	}
}

// gcExternMultiTableWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (table $e 4 externref)
//	  (table $a 4 anyref)
//	  (table $b 4 anyref)
//	  (func $churn (local $i i32)
//	    (loop $l
//	      (drop (struct.new $s (local.get $i)))
//	      (local.set $i (i32.add (local.get $i) (i32.const 1)))
//	      (br_if $l (i32.lt_u (local.get $i) (i32.const 300)))))
//	  (func (export "store") (param $host externref)
//	    ;; one object reachable only through the externref table
//	    (table.set $e (i32.const 0) (extern.convert_any (struct.new $s (i32.const 7))))
//	    ;; the same object, converted back, in the second anyref table
//	    (table.set $b (i32.const 2) (any.convert_extern (table.get $e (i32.const 0))))
//	    ;; objects reachable only through one anyref table each
//	    (table.set $a (i32.const 1) (struct.new $s (i32.const 30)))
//	    (table.set $b (i32.const 0) (struct.new $s (i32.const 500)))
//	    ;; a host externref carried as an anyref
//	    (table.set $a (i32.const 3) (any.convert_extern (local.get $host)))
//	    (call $churn))
//	  (func (export "sum") (result i32)
//	    (call $churn)
//	    (i32.add
//	      (i32.add
//	        (struct.get $s 0 (ref.cast (ref $s) (table.get $b (i32.const 2))))
//	        (struct.get $s 0 (ref.cast (ref $s) (any.convert_extern (table.get $e (i32.const 0))))))
//	      (i32.add
//	        (i32.add
//	          (struct.get $s 0 (ref.cast (ref $s) (table.get $a (i32.const 1))))
//	          (struct.get $s 0 (ref.cast (ref $s) (table.get $b (i32.const 0)))))
//	        (i32.add
//	          (ref.test (ref $s) (table.get $a (i32.const 3)))
//	          (ref.eq (ref.cast (ref eq) (table.get $b (i32.const 2)))
//	                  (ref.cast (ref eq) (any.convert_extern (table.get $e (i32.const 0)))))))))
//	  (func (export "host") (result externref)
//	    (extern.convert_any (table.get $a (i32.const 3)))))
const gcExternMultiTableWasm = "0061736d010000000114055f017f0060000060016f006000017f6000016f03050401020304040a036f00046e00046e00040716030573746f726500010373756d000204686f737400030aac01041c01017f03402000fb00001a200041016a2100200041ac02490d000b0b340041004107fb0000fb1b2600410241002500fb1a26024101411efb00002601410041f403fb0000260241032000fb1a260110000b4f00100041022502fb1600fb02000041002500fb1afb1600fb0200006a41012501fb1600fb02000041002502fb1600fb0200006a41032501fb140041022502fb166d41002500fb1afb166dd36a6a6a0b080041032501fb1b0b0039046e616d650108010005636875726e020e02000100016901010004686f7374030601000100016c040401000173050a03000165010161020162"
