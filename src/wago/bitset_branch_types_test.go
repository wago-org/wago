//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import "testing"

func TestBitsetBranchTypedValues(t *testing.T) {
	for _, tc := range []struct {
		name, wat                 string
		defaultValue, memberValue uint64
	}{
		{"v128", `(module (func (export "f") (param i32) (result i64) (local v128)
   (block $outer (result v128)
    i64.const 123
    (block $inner (result v128)
     v128.const i64x2 11 22 local.get 0
     br_table $inner $outer $inner $outer $inner $outer $inner $outer $inner)
    v128.const i64x2 1 2 i64x2.add
    local.set 1 drop local.get 1)
   local.tee 1 i64x2.extract_lane 0
   local.get 1 i64x2.extract_lane 1 i64.add))`, 36, 33},
		{"funcref", `(module (func $id) (elem declare func $id)
  (func (export "f") (param i32) (result i32)
   (block $outer (result funcref)
    (block $inner (result funcref)
     ref.null func local.get 0
     br_table $inner $outer $inner $outer $inner $outer $inner $outer $inner)
    drop ref.func $id)
   ref.is_null))`, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e := Compile(nil, watToWasm(t, tc.wat))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			in, e := Instantiate(c, InstantiateOptions{})
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			for _, idx := range []uint64{0, 1, 7, 8, 31, 32, 63, 64, 0xffffffff} {
				want := tc.defaultValue
				if idx < 8 && idx%2 == 1 {
					want = tc.memberValue
				}
				got, e := in.Invoke("f", idx)
				if e != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("index=%x got=%v want=%x err=%v", idx, got, want, e)
				}
			}
		})
	}
}
