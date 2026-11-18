//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"
)

// ref.cast and br_on_cast against the abstract func, extern, and exn
// hierarchies reached the GC helper, which only resolved any-hierarchy heap
// types and failed valid casts with "gc dynamic reference heap type -17 is
// unavailable". Top types admit every non-null value, bottom types only null.
func TestGCCastNonAnyAbstractHierarchies(t *testing.T) {
	inst := admissionModule(t, gcNonAnyHierarchyCastWasm)
	for _, tc := range []struct {
		export string
		want   int32 // -1 means a cast-failure trap
	}{
		{"cast_null_extern_nn", 0},
		{"cast_null_extern_null", 1},
		{"cast_extern_null", -1},
		{"cast_extern_nn", 0},
		{"cast_noextern_null", 1},
		{"cast_noextern_nn", -1},
		{"cast_func_nn", 0},
		{"cast_nofunc_nn", -1},
		{"brcast_extern", 6},
		{"brcastfail_noextern", 6},
		{"test_extern", 1},
		{"cast_exn_nn", 0},
		{"cast_noexn_nn", -1},
		{"cast_noexn_null", 1},
	} {
		got, err := inst.Invoke(tc.export)
		if tc.want < 0 {
			if err == nil || !strings.Contains(err.Error(), "cast failure") {
				t.Errorf("%s: err = %v, want cast failure", tc.export, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.export, err)
			continue
		}
		if AsI32(got[0]) != tc.want {
			t.Errorf("%s = %d, want %d", tc.export, AsI32(got[0]), tc.want)
		}
	}
}

// gcNonAnyHierarchyCastWasm encodes (each export first allocates a struct so
// the module keeps a live collector):
//
//	(module
//	  (type $s (struct))
//	  (global $e (mut externref) (ref.null extern))
//	  (func $mk (result externref) (extern.convert_any (struct.new $s)))
//	  (func (export "cast_null_extern_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null extern) (call $mk))))
//	  (func (export "cast_null_extern_null") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null extern) (ref.null extern))))
//	  (func (export "cast_extern_null") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref extern) (ref.null extern))))
//	  (func (export "cast_extern_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref extern) (call $mk))))
//	  (func (export "cast_noextern_null") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null noextern) (ref.null extern))))
//	  (func (export "cast_noextern_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null noextern) (call $mk))))
//	  (func (export "cast_func_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref func) (ref.func $mk))))
//	  (func (export "cast_nofunc_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null nofunc) (ref.func $mk))))
//	  (func (export "brcast_extern") (result i32) (drop (struct.new $s))
//	    (block $b (result externref) (drop (br_on_cast $b externref (ref extern) (call $mk))) (return (i32.const 5))) drop (i32.const 6))
//	  (func (export "brcastfail_noextern") (result i32) (drop (struct.new $s))
//	    (block $b (result externref) (drop (br_on_cast_fail $b externref (ref null noextern) (call $mk))) (return (i32.const 5))) drop (i32.const 6))
//	  (func (export "test_extern") (result i32) (drop (struct.new $s)) (ref.test (ref extern) (call $mk)))
//	  (tag $t)
//	  (func $ex (result exnref) (block $h (result exnref) (try_table (catch_all_ref $h) (throw $t)) (unreachable)))
//	  (func (export "cast_exn_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref exn) (call $ex))))
//	  (func (export "cast_noexn_nn") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null noexn) (call $ex))))
//	  (func (export "cast_noexn_null") (result i32) (drop (struct.new $s)) (ref.is_null (ref.cast (ref null noexn) (ref.null exn))))
//	  (elem declare func $mk)
//	)
const gcNonAnyHierarchyCastWasm = "0061736d010000000112055f006000016f6000017f60000060000169031110010202020202020202020202040202020d030100030606016f01d06f0b07ff010e13636173745f6e756c6c5f65787465726e5f6e6e000115636173745f6e756c6c5f65787465726e5f6e756c6c000210636173745f65787465726e5f6e756c6c00030e636173745f65787465726e5f6e6e000412636173745f6e6f65787465726e5f6e756c6c000510636173745f6e6f65787465726e5f6e6e00060c636173745f66756e635f6e6e00070e636173745f6e6f66756e635f6e6e00080d6272636173745f65787465726e0009136272636173746661696c5f6e6f65787465726e000a0b746573745f65787465726e000b0b636173745f65786e5f6e6e000d0d636173745f6e6f65786e5f6e6e000e0f636173745f6e6f65786e5f6e756c6c000f090501030001000ae501100700fb0000fb1b0b0c00fb00001a1000fb176fd10b0c00fb00001ad06ffb176fd10b0c00fb00001ad06ffb166fd10b0c00fb00001a1000fb166fd10b0c00fb00001ad06ffb1772d10b0c00fb00001a1000fb1772d10b0c00fb00001ad200fb1670d10b0c00fb00001ad200fb1773d10b1800fb00001a026f1000fb1801006f6f1a41050f0b1a41060b1800fb00001a026f1000fb1903006f721a41050f0b1a41060b0b00fb00001a1000fb146f0b0e0002691f4001030008000b000b0b0c00fb00001a100cfb1669d10b0c00fb00001a100cfb1774d10b0c00fb00001ad069fb1774d10b"
