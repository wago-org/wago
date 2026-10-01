//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func branchConditionPressureModule(n int, branch, wide, condition bool) []byte {
	typ, valueType, constant, shift, add, load := wasm.I32, byte(0x7f), byte(0x41), byte(0x74), byte(0x6a), byte(0x28)
	if wide {
		typ, valueType, constant, shift, add, load = wasm.I64, 0x7e, 0x42, 0x86, 0x7c, 0x29
	}
	var body []byte
	if branch {
		body = append(body, 0x02, valueType)
	}
	body = append(body, 0x20, 0, 0x20, 1, shift)
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 0, load, 0, 0)
	}
	cond := byte(0)
	if condition {
		cond = 1
	}
	body = append(body, 0x41, cond)
	if branch {
		body = append(body, 0x0d, 0, constant, 9)
	} else {
		body = append(body, 0x04, valueType, constant, 5, 0x05, constant, 9, 0x0b)
	}
	for i := 0; i <= n; i++ {
		body = append(body, add)
	}
	if branch {
		body = append(body, 0x0b)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestBranchConditionPressureAMD64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, branch := range []bool{false, true} {
			for _, condition := range []bool{false, true} {
				for _, n := range []int{0, 8, 9, 10, 13, 24} {
					t.Run(fmt.Sprintf("wide=%v/br_if=%v/condition=%v/loads=%d", wide, branch, condition, n), func(t *testing.T) {
						_, instance := compileTrapOrder(t, branchConditionPressureModule(n, branch, wide, condition))
						// The pure shift stays deferred through the condition. Its RCX
						// scratch and any RAX slot copies must preserve that condition.
						want := uint64(11)
						if condition {
							want = 7
							if branch {
								want = 0 // branch returns the last zero load
								if n == 0 {
									want = 2
								}
							}
						}
						got, err := instance.Invoke("run", 1, 1)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("got %v, %v; want %d", got, err, want)
						}
					})
				}
			}
		}
	}
}

func BenchmarkCompileBranchConditionPressureAMD64(b *testing.B) {
	for _, n := range []int{0, 10} {
		b.Run(fmt.Sprintf("loads=%d", n), func(b *testing.B) {
			module := branchConditionPressureModule(n, false, false, false)
			var codeBytes int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				compiled, err := Compile(nil, module)
				if err != nil {
					b.Fatal(err)
				}
				codeBytes = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}
