//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func i16x8BitmaskPressureModule(n int, lanes [8]int16) []byte {
	body := []byte{1, 1, 0x7f} // one i32 local for the bitmask
	// Keep eager float results in XMM registers and deferred float loads holding
	// GP address registers. Allocating the bitmask's GP result can force a load.
	for j := 0; j < n; j++ {
		body = append(body, 0x20, 1, 0x20, 1, 0xa0) // local.get 1 twice; f64.add
	}
	for j := 0; j < n; j++ {
		body = append(body, 0x20, 0, 0x41, byte(j), 0x6a, 0x2b, 0, 0) // f64.load at base+j
	}
	body = append(body, 0xfd, 0x0c) // v128.const
	for _, lane := range lanes {
		body = binary.LittleEndian.AppendUint16(body, uint16(lane))
	}
	body = append(body, 0xfd, 0x84, 1, 0x21, 2) // i16x8.bitmask; local.set 2
	for j := 0; j < 2*n; j++ {
		body = append(body, 0x1a) // drop
	}
	body = append(body, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestI16x8BitmaskPressureAMD64(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lanes [8]int16
		want  uint64
	}{
		{"negative", [8]int16{-1, -2, -3, -4, -5, -6, -7, -8}, 0xff},
		{"mixed", [8]int16{-32768, 32767, -129, 128, 0, -1, 1, -128}, 0xa5},
		{"nonnegative", [8]int16{0, 1, 127, 128, 255, 256, 1024, 32767}, 0},
	} {
		for _, n := range []int{0, 13} {
			t.Run(fmt.Sprintf("%s/live=%d", tc.name, n), func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig(), i16x8BitmaskPressureModule(n, tc.lanes))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				got, err := instance.Invoke("run", 0, F64(3))
				if err != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("result = %v, %v; want %#x", got, err, tc.want)
				}
			})
		}
	}
}

func BenchmarkCompileI16x8BitmaskAMD64(b *testing.B) {
	for _, n := range []int{0, 13} {
		b.Run(fmt.Sprintf("live=%d", n), func(b *testing.B) {
			module := i16x8BitmaskPressureModule(n, [8]int16{-1, -2, -3, -4, -5, -6, -7, -8})
			cfg := NewRuntimeConfig()
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				compiled, err := Compile(cfg, module)
				if err != nil {
					b.Fatal(err)
				}
				size = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(size), "code-B")
		})
	}
}

// Use the ordinary path so the before/after execution comparison is correct on
// both revisions. The pressure case is verified by the regression test above.
func BenchmarkInvokeI16x8BitmaskAMD64(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig(), i16x8BitmaskPressureModule(0, [8]int16{-1, -2, -3, -4, -5, -6, -7, -8}))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", 0, F64(3))
	if err != nil || len(got) != 1 || got[0] != 0xff {
		b.Fatalf("result = %v, %v; want 255", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := instance.Invoke("run", 0, F64(3)); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(compiled.CodeSize()), "code-B")
}
