//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func constantPinOwnershipModule(localCount byte) []byte {
	var body []byte
	// Keep twelve local pins and two scalar constant cache entries live. Four
	// vector constants must not claim any of those local registers permanently.
	for i := byte(0); i < localCount; i++ {
		body = append(body, 0x44)
		body = binary.LittleEndian.AppendUint64(body, 0x3ff0000000000000+uint64(i&1)<<52)
		body = append(body, 0x21, i)
	}
	body = append(body, 0x20, 0)
	for i := byte(1); i < localCount; i++ {
		body = append(body, 0x20, i, 0xa0)
	}
	body = append(body, 0x1a)
	for i := uint64(1); i <= 4; i++ {
		body = append(body, 0xfd, 12)
		body = binary.LittleEndian.AppendUint64(body, i)
		body = binary.LittleEndian.AppendUint64(body, i+10)
		if i != 3 {
			body = append(body, 0x1a)
		} else {
			body = append(body, 0xfd, 0x1d, 0)
		} // i64x2.extract_lane 0
	}
	body = append(body, 0x0b)
	code := append([]byte{1, localCount, 0x7c}, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))),
	)
}

func TestConstantCacheDoesNotOwnLocalPin(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache=%v", cache), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithOptimization("ext-fp-pins", true).WithOptimization("v128-const-cache", cache), constantPinOwnershipModule(13))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for i := 0; i < 5; i++ {
				got, err := instance.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 3 {
					t.Fatalf("got %x, %v; want [3]", got, err)
				}
			}
		})
	}
}

func BenchmarkConstantPinOwnershipCompile(b *testing.B) {
	module := constantPinOwnershipModule(13)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(nil, module)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
}

func BenchmarkConstantPinOwnershipInvoke(b *testing.B) {
	// Nine locals keep the cache below saturation; both revisions return 3.
	compiled, err := Compile(nil, constantPinOwnershipModule(9))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 3 {
		b.Fatalf("got %x, %v; want [3]", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := instance.Invoke("run"); err != nil {
			b.Fatal(err)
		}
	}
}
