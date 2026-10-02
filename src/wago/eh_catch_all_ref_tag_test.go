//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import "testing"

// AMD64 dispatched catch_all_ref as a tag match against its unset tag field
// (tag 0), so it caught only exceptions of tag 0 and let others unwind as
// unhandled. catch_all_ref must catch every tag, like catch_all.
func TestCatchAllRefCatchesEveryTag(t *testing.T) {
	inst := admissionModule(t, catchAllRefTagIndexWasm)
	for export, want := range map[string]int32{"t0": 5, "t1": 6} {
		if got := invokeI32(t, inst, export); got != want {
			t.Fatalf("%s = %d, want %d", export, got, want)
		}
	}
}

// catchAllRefTagIndexWasm encodes:
//
//	(module
//	  (tag $a (param i32))
//	  (tag $b (param i32))
//	  (func (export "t0") (result i32) (local $e exnref)
//	    (block $r (result exnref)
//	      (try_table (catch_all_ref $r) (throw $a (i32.const 5)))
//	      (unreachable))
//	    (local.set $e)
//	    (block $h (result i32)
//	      (try_table (catch $a $h) (throw_ref (local.get $e)))
//	      (unreachable)))
//	  (func (export "t1") (result i32) (local $e exnref)
//	    (block $r (result exnref)
//	      (try_table (catch_all_ref $r) (throw $b (i32.const 6)))
//	      (unreachable))
//	    (local.set $e)
//	    (block $h (result i32)
//	      (try_table (catch $b $h) (throw_ref (local.get $e)))
//	      (unreachable))))
const catchAllRefTagIndexWasm = "0061736d0100000001090260017f006000017f03030201010d050200000000070b02027430000002743100010a47022201016902691f40010300410508000b000b2100027f1f400100000020000a0b000b0b2201016902691f40010300410608010b000b2100027f1f400100010020000a0b000b0b002e046e616d65020b0200010001650101000165031102000200017202016801020001720201680b0702000161010162"
