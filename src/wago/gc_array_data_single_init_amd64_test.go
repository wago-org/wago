//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcArrayDataSingleInitModule(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*17 + 3)
	}
	segment := append([]byte{1}, wasmtest.ULEB(uint32(size))...)
	segment = append(segment, data...)
	body := []byte{0x41, 0, 0x41}
	body = append(body, wasmtest.SLEB32(int32(size))...)
	body = append(body, 0xfb, 9, 0, 0, 0x41)
	body = append(body, wasmtest.SLEB32(int32(size-1))...)
	body = append(body, 0xfb, 13, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x78, 1}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(12, []byte{1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(segment)))
}

func BenchmarkGCArrayDataSingleInitialization(b *testing.B) {
	for _, size := range []int{16, 4096, 65536} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayDataSingleInitModule(size))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			want := uint64(byte((size-1)*17 + 3))
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("run=%v,%v want%d", got, err, want)
				}
			}
		})
	}
}
