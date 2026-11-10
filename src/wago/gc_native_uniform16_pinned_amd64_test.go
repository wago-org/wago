//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNativeUniform16PinnedLocalsModule(floating bool) []byte {
	child := []byte{0x5f, 0x01, 0x7f, 0x00} // struct with one immutable i32
	aggregate := []byte{0x5e, 0x7f, 0x01}   // mutable i32 array
	if floating {
		aggregate[1] = 0x7d // f32; transport the initializer's bits without arithmetic
	}
	params := make([]wasm.ValType, 19)
	for i := range params {
		params[i] = wasm.F64
		if i < 8 {
			params[i] = wasm.I32
		}
	}
	// Locals 19 and 20 hold a live child and a lazy null. Local 21 receives
	// each new aggregate. Keep both register banks dirty across allocations.
	body := []byte{0x02, 0x02, 0x63, 0x00, 0x01, 0x63, 0x01}
	for i := byte(0); i < 19; i++ {
		body = append(body, 0x20, i)
		if i < 8 {
			body = append(body, 0x41, 0x01, 0x6a)
		} else {
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0xa0)
		}
		body = append(body, 0x21, i)
	}
	body = append(body, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x13)
	for range 96 {
		body = append(body, 0x20, 0x00)
		if floating {
			body = append(body, 0xbe) // f32.reinterpret_i32
		}
		body = append(body, 0x41, 0x10, 0xfb, 0x06, 0x01, 0x21, 0x15)
	}
	body = append(body, 0x20, 0x15, 0x24, 0x00) // publish last array for separate reads
	body = append(body, 0x20, 0x13, 0xfb, 0x02, 0x00, 0x00,
		0x20, 0x14, 0xd1, 0x6a, 0xb7) // child field + ref.is_null, as f64
	for i := byte(0); i < 19; i++ {
		body = append(body, 0x20, i)
		if i < 8 {
			body = append(body, 0xb7)
		}
		body = append(body, 0xa0)
	}
	body = append(body, 0x0b)
	read := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0xfb, 0x0b, 0x01}
	if floating {
		read = append(read, 0xbc) // i32.reinterpret_f32
	}
	read = append(read, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(child, aggregate, wasmtest.FuncType(params, []wasm.ValType{wasm.F64}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("read", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), append(wasmtest.ULEB(uint32(len(read))), read...))),
	)
}

func TestGCNativeUniform16PreservesPinnedLocalsAndPayload(t *testing.T) {
	t.Run("i32", func(t *testing.T) { checkGCNativeUniform16PinnedLocals(t, false) })
	t.Run("f32", func(t *testing.T) { checkGCNativeUniform16PinnedLocals(t, true) })
}

func checkGCNativeUniform16PinnedLocals(t *testing.T, floating bool) {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeUniform16PinnedLocalsModule(floating))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 4096, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for iteration := range 20 {
		args := make([]uint64, 19)
		want := 43.0
		for i := range args {
			value := float64(iteration + i + 1)
			want += value + 1
			args[i] = math.Float64bits(value)
			if i < 8 {
				args[i] = uint64(value)
			}
		}
		got, err := in.Invoke("run", args...)
		if err != nil || len(got) != 1 || math.Float64frombits(got[0]) != want {
			t.Fatalf("iteration %d = %v, %v; want f64(%g)", iteration, got, err, want)
		}
		for index := range 16 {
			got, err := in.Invoke("read", uint64(index))
			if err != nil || len(got) != 1 || got[0] != uint64(iteration+2) {
				t.Fatalf("iteration %d element %d = %v, %v; want %d", iteration, index, got, err, iteration+2)
			}
		}
	}
	if stats := in.gc.Stats(); stats.Allocations != 20*97 || stats.MinorCollections == 0 {
		t.Fatalf("collector stats = %+v; want 1940 allocations and minor collections", stats)
	}
}
