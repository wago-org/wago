//go:build amd64 && !tinygo

package wago

import (
	"fmt"
	"testing"
)

func TestHintedBinarySinkPreservesDeferredLoadAddress(t *testing.T) {
	wat := `(module
  (memory 1)
  (global $sp (mut i32) (i32.const 65536))
  (func $add (param i32 i32) (result i32) (local i32)
   global.get $sp i32.const 16 i32.sub local.set 2
   local.get 2 local.get 0 i32.store offset=12
   local.get 2 local.get 1 i32.store offset=8
   local.get 2 i32.load offset=12
   local.get 2 i32.load offset=8
   %s return)
  (func (export "add") (param i32 i32) (result i32) (local i32 i32)
   global.get $sp i32.const 16 i32.sub local.set 2
   local.get 2 global.set $sp
   local.get 2 local.get 0 i32.store offset=12
   local.get 2 local.get 1 i32.store offset=8
   local.get 2 i32.load offset=12
   local.get 2 i32.load offset=8
   call $add local.set 3
   local.get 2 i32.const 16 i32.add global.set $sp
   local.get 3 return))`
	for _, operation := range []struct {
		name  string
		apply func(uint32, uint32) uint32
	}{
		{"i32.add", func(a, b uint32) uint32 { return a + b }},
		{"i32.sub", func(a, b uint32) uint32 { return a - b }},
		{"i32.mul", func(a, b uint32) uint32 { return a * b }},
		{"i32.and", func(a, b uint32) uint32 { return a & b }},
		{"i32.or", func(a, b uint32) uint32 { return a | b }},
		{"i32.xor", func(a, b uint32) uint32 { return a ^ b }},
	} {
		module := watToWasmCA(t, fmt.Sprintf(wat, operation.name))
		for _, inline := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/inline=%t/reg-abi=%t", operation.name, inline, regABI), func(t *testing.T) {
					config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("inline", inline).WithOptimization("reg-abi", regABI)
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
					for _, pair := range [][2]uint32{{20, 1}, {1, 20}, {0, 42}, {42, 0}, {0xffffffff, 2}} {
						result, err := in.Invoke("add", I32(int32(pair[0])), I32(int32(pair[1])))
						if err != nil || len(result) != 1 || uint32(AsI32(result[0])) != operation.apply(pair[0], pair[1]) {
							t.Fatalf("add(%d,%d) = %v, %v; want %d", pair[0], pair[1], result, err, operation.apply(pair[0], pair[1]))
						}
					}
				})
			}
		}
	}
}

func TestHintedBinarySinkPreservesI64DeferredLoadAddress(t *testing.T) {
	wat := `(module
  (memory 1)
  (global $sp (mut i32) (i32.const 65536))
  (func $add (param i64 i64) (result i64) (local i32)
   global.get $sp i32.const 32 i32.sub local.set 2
   local.get 2 local.get 0 i64.store offset=24
   local.get 2 local.get 1 i64.store offset=16
   local.get 2 i64.load offset=24
   local.get 2 i64.load offset=16
   %s return)
  (func (export "add") (param i64 i64) (result i64) (local i32 i64)
   global.get $sp i32.const 32 i32.sub local.set 2
   local.get 2 global.set $sp
   local.get 2 local.get 0 i64.store offset=24
   local.get 2 local.get 1 i64.store offset=16
   local.get 2 i64.load offset=24
   local.get 2 i64.load offset=16
   call $add local.set 3
   local.get 2 i32.const 32 i32.add global.set $sp
   local.get 3 return))`
	for _, operation := range []struct {
		name  string
		apply func(uint64, uint64) uint64
	}{
		{"i64.add", func(a, b uint64) uint64 { return a + b }},
		{"i64.sub", func(a, b uint64) uint64 { return a - b }},
		{"i64.mul", func(a, b uint64) uint64 { return a * b }},
		{"i64.and", func(a, b uint64) uint64 { return a & b }},
		{"i64.or", func(a, b uint64) uint64 { return a | b }},
		{"i64.xor", func(a, b uint64) uint64 { return a ^ b }},
	} {
		module := watToWasmCA(t, fmt.Sprintf(wat, operation.name))
		for _, inline := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/inline=%t/reg-abi=%t", operation.name, inline, regABI), func(t *testing.T) {
					config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("inline", inline).WithOptimization("reg-abi", regABI)
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
					for _, pair := range [][2]uint64{{20, 1}, {1<<40 | 20, 1<<48 | 22}, {1 << 63, 1<<32 | 1}, {^uint64(0), 2}, {0, ^uint64(0)}} {
						result, err := in.Invoke("add", pair[:]...)
						if err != nil || len(result) != 1 || result[0] != operation.apply(pair[0], pair[1]) {
							t.Fatalf("add(%d,%d) = %v, %v; want %d", pair[0], pair[1], result, err, operation.apply(pair[0], pair[1]))
						}
					}
				})
			}
		}
	}
}
