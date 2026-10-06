//go:build amd64 || arm64

package wago

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

func TestCachedHostEntryAcrossExportEviction(t *testing.T) {
	if !cachedHostEntryEnabled || codeProfileEnabled {
		t.Skip("entry cache disabled")
	}
	var wat strings.Builder
	wat.WriteString(`(module (import "env" "step" (func $step (param i32)(result i32)))`)
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&wat, `(func (export "f%d") (param i32 i32)(result i32) local.get 0 call $step i32.const %d i32.add)`, i, 100+i)
	}
	wat.WriteString(`(func (export "leaf") (param i32 i32)(result i32) local.get 0 i32.const 99 i32.add))`)
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, wat.String()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	sawCachedHostEntry := false
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 { calls++; return v + 1 }).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for pass := 0; pass < 4; pass++ {
		for j := 0; j < 9; j++ {
			i := (j*5 + pass) % 9
			v := uint64(pass*10 + j)
			name := fmt.Sprintf("f%d", i)
			_, err := in.Invoke(name, v, 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke(name, v, 0)
			if err != nil || len(got) != 1 || got[0] != v+uint64(101+i) {
				t.Fatalf("%s got %v %v", name, got, err)
			}
			ic := in.findInvokeCache(name)
			if ic == nil {
				t.Fatal("export was not cached")
			}
			sawCachedHostEntry = sawCachedHostEntry || ic.cachedHostEntry

		}
		got, err := in.Invoke("leaf", 7, 0)
		if err != nil || len(got) != 1 || got[0] != 106 {
			t.Fatalf("leaf entry %v %v", got, err)
		}
	}
	if !sawCachedHostEntry {
		t.Fatal("cached host path was not prepared")
	}
	if calls != 72 {
		t.Fatalf("wrong callback count %d", calls)
	}
	// The discriminator occupies the previous byte of padding; this experiment
	// must not grow each inline cache record or the four-record instance cache.
	if unsafe.Sizeof(invokeCache{}) != 80 {
		t.Fatalf("cache footprint %d", unsafe.Sizeof(invokeCache{}))
	}
}
