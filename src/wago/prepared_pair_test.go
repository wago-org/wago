//go:build (amd64 || arm64) && (linux || darwin || windows) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedPairMixedModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32, wasm.I64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}))),
	)
}

func preparedPairTrapModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32, wasm.I64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x00,       // unreachable
			0x0b,       // end if
			0x20, 0x00, // local.get 0
			0x20, 0x01, // local.get 1
			0x0b, // end function
		}))),
	)
}

func preparedPairFourArgsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I64, wasm.I32, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64, wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x02, 0x20, 0x03, 0x0b}))),
	)
}

func TestPreparedDirectTwoIntegerResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module []byte
		args   []uint64
		want   []uint64
	}{
		{"i32-pair", hostToWasmI32SignatureModule(2, 2), []uint64{^uint64(0), I32(42)}, []uint64{I32(-1), I32(42)}},
		{"mixed-pair", preparedPairMixedModule(), []uint64{^uint64(0), 0x1122334455667788}, []uint64{I32(-1), 0x1122334455667788}},
		{"four-args", preparedPairFourArgsModule(), []uint64{9, I32(-5), 0x1122334455667788, ^uint64(0)}, []uint64{0x1122334455667788, I32(-1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := MustCompile(tc.module)
			defer compiled.Close()
			if !compiled.directPreparedBoundedAt(0) {
				t.Fatal("compiler did not prove the two-result entry bounded")
			}
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("f")
			if err != nil {
				t.Fatal(err)
			}
			if !fn.directIntFast || !fn.directIsolated {
				t.Fatal("two-result function did not select direct entry")
			}
			check := func(label string, out []uint64, err error) {
				t.Helper()
				if err != nil || len(out) != 2 || out[0] != tc.want[0] || out[1] != tc.want[1] {
					t.Fatalf("%s = %v, %v; want %v", label, out, err, tc.want)
				}
			}
			out, err := fn.Invoke(tc.args...)
			check("ordinary", out, err)
			out, err = in.Invoke("f", tc.args...)
			check("instance", out, err)
			if cache := in.findInvokeCache("f"); cache == nil || !cache.directIntFast || !cache.directIntBounded {
				t.Fatal("instance invocation did not select bounded pair entry")
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			out, err = session.Invoke(tc.args...)
			check("session", out, err)
			session.Close()
			if _, err := in.ExportedFunc("f"); err != nil {
				t.Fatal(err)
			}
			out, err = fn.Invoke(tc.args...)
			check("shared fallback", out, err)
			out, err = in.Invoke("f", tc.args...)
			check("instance shared fallback", out, err)
		})
	}
}

func TestPreparedDirectTwoResultTrapReset(t *testing.T) {
	compiled := MustCompile(preparedPairTrapModule())
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("compiler did not prove the trapping pair entry bounded")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.directIntFast {
		t.Fatal("trapping pair did not select direct entry")
	}
	if out, err := fn.Invoke(0, 9); err == nil {
		t.Fatalf("trapping pair returned %v", out)
	}
	if out, err := fn.Invoke(7, 0x1122334455667788); err != nil || len(out) != 2 || out[0] != 7 || out[1] != 0x1122334455667788 {
		t.Fatalf("call after trap = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", 0, 9); err == nil {
		t.Fatalf("instance trapping pair returned %v", out)
	}
	if out, err := in.Invoke("f", 7, 0x1122334455667788); err != nil || len(out) != 2 || out[0] != 7 || out[1] != 0x1122334455667788 {
		t.Fatalf("instance call after trap = %v, %v", out, err)
	}
}

func BenchmarkPreparedDirectPair(b *testing.B) {
	compiled := MustCompile(hostToWasmI32SignatureModule(2, 2))
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil {
		b.Fatal(err)
	}
	if !fn.directIntFast {
		b.Fatal("pair benchmark did not select direct entry")
	}
	b.Run("ordinary", func(b *testing.B) {
		b.ReportAllocs()
		var out []uint64
		var err error
		for i := 0; i < b.N; i++ {
			out, err = fn.Invoke(1, 2)
		}
		if err != nil || len(out) != 2 || out[0] != 1 || out[1] != 2 {
			b.Fatalf("result = %v, %v", out, err)
		}
	})
	session, err := fn.OpenSession()
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()
	b.Run("session", func(b *testing.B) {
		b.ReportAllocs()
		var out []uint64
		var err error
		for i := 0; i < b.N; i++ {
			out, err = session.Invoke2(1, 2)
		}
		if err != nil || len(out) != 2 || out[0] != 1 || out[1] != 2 {
			b.Fatalf("result = %v, %v", out, err)
		}
	})
}
