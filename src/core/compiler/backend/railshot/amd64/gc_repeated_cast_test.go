//go:build amd64

package amd64

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func repeatedCastAccessBytes(kind string, nullable, exact, padded bool, target uint32, copies int) []byte {
	storage, resultType, read := byte(0x7f), wasm.I32, byte(2)
	switch kind {
	case "struct-i64":
		storage, resultType = 0x7e, wasm.I64
	case "struct-f32":
		storage, resultType = 0x7d, wasm.F32
	case "struct-f64":
		storage, resultType = 0x7c, wasm.F64
	case "struct-v128", "array-v128":
		storage, resultType = 0x7b, wasm.V128
	case "struct-ref", "array-ref":
		storage, resultType = 0x6e, wasm.AnyRef
	case "struct-i8-s":
		storage, read = 0x78, 3
	case "struct-i16-u":
		storage, read = 0x77, 4
	}
	object := []byte{0x5f, 1, storage, 0}
	if kind == "nonfinal-struct" {
		object = append([]byte{0x50, 0}, object...)
	}
	if strings.HasPrefix(kind, "array-") {
		object = []byte{0x5e, storage, 1}
	}
	var types [][]byte
	for i := uint32(0); i <= target; i++ {
		types = append(types, object)
	}
	types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{resultType}))
	cast := byte(22)
	if nullable {
		cast = 23
	}
	body := []byte{0x20, 0}
	for i := 0; i < copies; i++ {
		body = append(body, 0xfb)
		if padded && i != 0 {
			body = append(body, cast|0x80, 0)
		} else {
			body = append(body, cast)
		}
		if exact {
			body = append(body, 0x62)
		}
		heap := wasmtest.SLEB32(int32(target))
		if padded && i != 0 {
			heap[len(heap)-1] |= 0x80
			heap = append(heap, 0)
		}
		body = append(body, heap...)
	}
	if kind == "array-len" {
		body = append(body, 0xfb, 15)
	} else if strings.HasPrefix(kind, "array-") {
		body = append(body, 0x41, 0, 0xfb, 11)
		body = append(body, wasmtest.ULEB(target)...)
	} else {
		body = append(body, 0xfb, read)
		body = append(body, wasmtest.ULEB(target)...)
		body = append(body, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(target+1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCRepeatedCastsResolveOnce(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, kind := range []string{"struct", "struct-i64", "struct-f32", "struct-f64", "struct-v128", "struct-ref", "struct-i8-s", "struct-i16-u", "nonfinal-struct", "array-get", "array-ref", "array-v128", "array-len"} {
		for _, nullable := range []bool{false, true} {
			for _, encoding := range []string{"ordinary", "exact", "padded", "multi-byte"} {
				target := uint32(0)
				if encoding == "multi-byte" {
					target = 106
				}
				t.Run(fmt.Sprintf("%s/nullable=%t/%s", kind, nullable, encoding), func(t *testing.T) {
					compileCalls := func(copies int) map[string]int {
						t.Helper()
						m, err := wasm.DecodeModule(repeatedCastAccessBytes(kind, nullable, encoding == "exact", encoding == "padded", target, copies))
						if err != nil {
							t.Fatal(err)
						}
						if err := wasm.ValidateModule(m); err != nil {
							t.Fatal(err)
						}
						var stats ModuleStats
						compiled, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
						if err != nil {
							t.Fatal(err)
						}
						defer compiled.CodeImage.Close()
						return stats.Funcs[0].Calls
					}
					one := compileCalls(1)
					if one["gcnative"]+one["gcnative-leaf"]+one[callKindHostSync] == 0 {
						t.Fatalf("fixture emitted no checked cast/access: %v", one)
					}
					for copies := 2; copies <= 4; copies++ {
						got := compileCalls(copies)
						if !maps.Equal(got, one) {
							t.Fatalf("%d identical casts/access calls=%v; want the single-cast checks %v", copies, got, one)
						}
					}
				})
			}
		}
	}
}
