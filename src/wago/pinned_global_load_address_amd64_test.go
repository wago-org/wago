//go:build amd64 && !tinygo

package wago

import "testing"

func TestDeferredLoadFromPinnedGlobalKeepsOwnedAddress(t *testing.T) {
	// Go's export wrapper moves its stack global around a call. A deferred
	// load address must not borrow that global as though it were a local.
	module := watToWasmCA(t, `(module
 (memory (export "memory") 1)
 (global $sp (mut i32) (i32.const 65536))
 (func $inner (param i32) (result i32) (local $sp i32) (local $result i64)
  global.get $sp local.set $sp
  block block local.get 0 br_table 0 0 0 0 1 end
   local.get $sp i64.load32_u offset=8
   local.get $sp i64.load32_u offset=12 i64.add local.set $result
   local.get $sp i64.extend_i32_u i64.const 16 i64.add i32.wrap_i64
   local.get $result i64.store32
   local.get $sp i32.const 8 i32.add local.tee $sp global.set $sp
   i32.const 0 return
  end unreachable)
 (func (export "add") (param i32 i32) (result i32)
  global.get $sp i32.const 16 i32.sub global.set $sp
  global.get $sp local.get 0 i32.store
  global.get $sp local.get 1 i32.store offset=4
  global.get $sp i32.const 8 i32.sub global.set $sp
  global.get $sp i64.const 386334721 i64.store
  i32.const 0 call $inner if unreachable end
  global.get $sp i32.load offset=8
  global.get $sp i32.const 16 i32.add global.set $sp
  return unreachable))
`)
	for _, inline := range []bool{false, true} {
		name := "no-inline"
		if inline {
			name = "inline"
		}
		t.Run(name, func(t *testing.T) {
			config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("inline", inline)
			compiled, err := Compile(config, module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, pair := range [][2]uint32{{20, 22}, {1, 41}, {0, 42}, {42, 0}, {0xffffffff, 2}} {
				result, err := in.Invoke("add", I32(int32(pair[0])), I32(int32(pair[1])))
				if err != nil || len(result) != 1 || uint32(AsI32(result[0])) != pair[0]+pair[1] {
					t.Fatalf("add(%d,%d) = %v, %v; want %d", pair[0], pair[1], result, err, pair[0]+pair[1])
				}
			}
		})
	}
}
