//go:build linux && arm64 && !tinygo

package wago

import "testing"

func TestArm64ExternrefTableFillCanonicalizesHostI32(t *testing.T) {
	module := watToWasm(t, `(module
		(import "env" "index" (func $index (result i32)))
		(table 1 externref)
		(func (export "fill")
			call $index
			ref.null extern
			i32.const 1
			table.fill)
		(func (export "is-null") (result i32)
			i32.const 0
			table.get
			ref.is_null))`)
	compiled, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.index", HostCallFunc(func(call HostCall) {
		// An i32 value occupies a raw uint64 host-result slot. Its upper bits
		// must not affect a table32 bounds check or the subsequent address.
		call.ResultSlots()[0] = 0x1_0000_0000
	}))})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("fill"); err != nil {
		t.Fatalf("fill at table32 index zero: %v", err)
	}
	result, err := instance.Invoke("is-null")
	if err != nil || len(result) != 1 || AsI32(result[0]) != 1 {
		t.Fatalf("table entry after fill = %v, %v; want null", result, err)
	}
}
