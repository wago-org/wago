//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestTailSelfFrameResetsDeclaredLocals(t *testing.T) {
	body := []byte{1, 1, 0x7f, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x20, 2, 0x6a,
		0x05, 0x41, 0xe3, 0, 0x21, 2, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 0, 0x0b, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithOptimization("tail-self-loop", enabled).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		if c.directPreparedBoundedAt(0) || c.boundedNativeScalarLeaf() {
			t.Fatalf("enabled=%t: bounded=%t native=%t for unbounded self tails", enabled, c.directPreparedBoundedAt(0), c.boundedNativeScalarLeaf())
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for repeat := 0; repeat < 3; repeat++ {
			for _, n := range []uint64{0, 1, 2, 31, 4096, 100000} {
				a := uint64(0xfffffff0)
				got, err := fn.Invoke(n|0xdeadbeef00000000, a|0x1234567800000000)
				want := uint64(uint32(a + n*17))
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("enabled=%t n=%d: %v,%v want %d", enabled, n, got, err, want)
				}
			}
		}
		in.Close()
		c.Close()
	}
}

func TestTailSelfFrameParallelArguments(t *testing.T) {
	for _, values := range []int{1, 2, 3, 4, 7} {
		params := make([]wasm.ValType, values+1)
		for i := range params {
			params[i] = wasm.I32
		}
		body := []byte{0x20, 0, 0x45, 0x04, 0x7f}
		for i := 1; i <= values; i++ {
			body = append(body, 0x20, byte(i), 0x41, byte(i+1), 0x6c)
			if i > 1 {
				body = append(body, 0x6a)
			}
		}
		body = append(body, 0x05, 0x20, 0, 0x41, 1, 0x6b)
		for i := 0; i < values; i++ {
			body = append(body, 0x20, byte((i+1)%values+1))
			if i == values-1 {
				body = append(body, 0x41, 7, 0x6a)
			}
		}
		body = append(body, 0x12, 0, 0x0b, 0x0b)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		for _, enabled := range []bool{false, true} {
			c, err := NewRuntimeConfig().WithOptimization("tail-self-loop", enabled).Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 3, 31, 127, 4096} {
				args := make([]uint64, values+1)
				args[0] = n | 0x8765432100000000
				model := make([]uint32, values)
				for i := range model {
					model[i] = 0xfedcba98 + uint32(i)*0x13579bdf
					args[i+1] = uint64(model[i]) | 0xdeadbeef00000000
				}
				for range n {
					first := model[0]
					copy(model, model[1:])
					model[values-1] = first + 7
				}
				var want uint32
				for i, v := range model {
					want += v * uint32(i+2)
				}
				got, err := fn.Invoke(args...)
				if err != nil || len(got) != 1 || got[0] != uint64(want) {
					t.Fatalf("values=%d enabled=%t n=%d: %v,%v want %d", values, enabled, n, got, err, want)
				}
			}
			in.Close()
			c.Close()
		}
	}
}

func TestTailSelfFrameComplexArgumentPressure(t *testing.T) {
	const values = 7
	params := make([]wasm.ValType, values+1)
	for i := range params {
		params[i] = wasm.I32
	}
	body := []byte{0x20, 0, 0x45, 0x04, 0x7f}
	for i := 1; i <= values; i++ {
		body = append(body, 0x20, byte(i))
		if i > 1 {
			body = append(body, 0x6a)
		}
	}
	body = append(body, 0x05, 0x20, 0, 0x41, 1, 0x6b)
	for i := 0; i < values; i++ {
		body = append(body, 0x20, byte((i+1)%values+1), 0x41, byte(i+2), 0x6c, 0x41, 7, 0x6a)
	}
	body = append(body, 0x12, 0, 0x0b, 0x0b)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithOptimization("tail-self-loop", enabled).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 7, 31, 511, 4096} {
			args := make([]uint64, values+1)
			args[0] = n | 0x8765432100000000
			var model, next [values]uint32
			for i := range model {
				model[i] = 0xfedcba98 + uint32(i)*0x13579bdf
				args[i+1] = uint64(model[i]) | 0xdeadbeef00000000
			}
			for range n {
				for i := range model {
					next[i] = model[(i+1)%values]*uint32(i+2) + 7
				}
				model = next
			}
			var want uint32
			for _, v := range model {
				want += v
			}
			got, err := fn.Invoke(args...)
			if err != nil || len(got) != 1 || got[0] != uint64(want) {
				t.Fatalf("enabled=%t n=%d: %v,%v want %d", enabled, n, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}

func TestTailMutualFrameParallelArguments(t *testing.T) {
	for _, values := range []int{1, 2, 3, 4, 7} {
		params := make([]wasm.ValType, values+1)
		for i := range params {
			params[i] = wasm.I32
		}
		body := []byte{0x20, 0, 0x45, 0x04, 0x7f}
		for i := 1; i <= values; i++ {
			body = append(body, 0x20, byte(i), 0x41, byte(i+1), 0x6c)
			if i > 1 {
				body = append(body, 0x6a)
			}
		}
		body = append(body, 0x05, 0x20, 0, 0x41, 1, 0x6b)
		for i := 0; i < values; i++ {
			body = append(body, 0x20, byte((i+1)%values+1))
			if i == values-1 {
				body = append(body, 0x41, 7, 0x6a)
			}
		}
		body = append(body, 0x12, 1, 0x0b, 0x0b)
		other := append([]byte(nil), body...)
		other[len(other)-3] = 0
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(other))))
		for _, enabled := range []bool{false, true} {
			c, err := NewRuntimeConfig().WithOptimization("tail-self-loop", enabled).Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			for index := range 2 {
				if c.directPreparedBoundedAt(index) {
					t.Fatalf("bounded mutual cycle at %d", index)
				}
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 3, 31, 127, 4096} {
				args := make([]uint64, values+1)
				args[0] = n | 0x8765432100000000
				model := make([]uint32, values)
				for i := range model {
					model[i] = 0xfedcba98 + uint32(i)*0x13579bdf
					args[i+1] = uint64(model[i]) | 0xdeadbeef00000000
				}
				for range n {
					first := model[0]
					copy(model, model[1:])
					model[values-1] = first + 7
				}
				var want uint32
				for i, v := range model {
					want += v * uint32(i+2)
				}
				got, err := fn.Invoke(args...)
				if err != nil || len(got) != 1 || got[0] != uint64(want) {
					t.Fatalf("values=%d enabled=%t n=%d: %v,%v want %d", values, enabled, n, got, err, want)
				}
			}
			in.Close()
			c.Close()
		}
	}
}

func TestTailLocalDifferentIntegerSignatures(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0xad, 0x20, 1, 0xa7, 0x12, 1, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x50, 0x04, 0x7e, 0x20, 1, 0xad, 0x05, 0x20, 0, 0x42, 1, 0x7d, 0xa7, 0x20, 1, 0xad, 0x12, 0, 0x0b, 0x0b}))))
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithOptimization("tail-self-loop", enabled).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		const value = uint64(0x12345678fedcba98)
		for _, n := range []uint64{0, 1, 2, 3, 127, 100000} {
			got, err := fn.Invoke(n|0xdeadbeef00000000, value)
			want := value
			if n > 0 {
				want = uint64(uint32(value & 0xffffffff))
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("enabled=%t n=%d: %v,%v want %#x", enabled, n, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}
