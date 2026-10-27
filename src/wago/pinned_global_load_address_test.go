//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func pinnedGlobalLoadModule(t testing.TB, result, load, data string) []byte {
	t.Helper()
	return watToWasm(t, fmt.Sprintf(`(module
		(memory 1)
		(global $base (export "base") (mut i32) (i32.const 1024))
		(data (i32.const 1024) "%s")
		(func (export "load") (result %s)
			i32.const 1024 global.set $base
			global.get $base %s
			global.get $base i32.const 16 i32.add global.set $base)
		(func (export "trap") (param i32) (result i32)
			i32.const 1 local.get 0 i32.div_u
			global.get $base i32.load offset=65536 i32.add
			i32.const 2048 global.set $base)
		(func (export "get") (result i32) global.get $base)
		;; Loop-weighted reads make the compiler pin $base across the module.
		;; This function is never called.
		(func (loop (loop (loop global.get $base drop)))))`, data, result, load))
}

func TestDeferredLoadKeepsPinnedGlobalAddress(t *testing.T) {
	for _, tc := range []struct {
		name, result, load, data string
		want                     uint64
	}{
		{"i32", "i32", "i32.load", `\15\00\00\00`, I32(21)},
		{"expression", "i32", "i32.load i32.const 2 i32.mul", `\15\00\00\00`, I32(42)},
		{"i64", "i64", "i64.load", `\15\00\00\00\00\00\00\00`, I64(21)},
		{"f32", "f32", "f32.load", `\00\00\a8\41`, F32(21)},
		{"f64", "f64", "f64.load", `\00\00\00\00\00\00\35\40`, F64(21)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := pinnedGlobalLoadModule(t, tc.result, tc.load, tc.data)
			compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("load")
				if err != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("load = %v, %v; want %#x", got, err, tc.want)
				}
			}
			for _, trap := range []struct {
				divisor uint64
				message string
			}{{0, "division by zero"}, {1, "out of bounds"}} {
				if _, err := in.Invoke("trap", trap.divisor); err == nil || !strings.Contains(err.Error(), trap.message) {
					t.Fatalf("trap(%d) = %v; want %s", trap.divisor, err, trap.message)
				}
				got, err := in.Invoke("get")
				if err != nil || len(got) != 1 || got[0] != 1040 {
					t.Fatalf("global after trap = %v, %v; want 1040", got, err)
				}
			}
		})
	}
}

func BenchmarkCompilePinnedGlobalLoad(b *testing.B) {
	module := pinnedGlobalLoadModule(b, "i32", "i32.load", `\15\00\00\00`)
	compiled, err := Compile(nil, module)
	if err != nil {
		b.Fatal(err)
	}
	var code bytes.Buffer
	if _, err := compiled.WriteCodeTo(&code); err != nil {
		b.Fatal(err)
	}
	compiled.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiled, err := Compile(nil, module)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
	b.ReportMetric(float64(code.Len()), "native-B")
}
