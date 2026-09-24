//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedMixedTrapModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.F64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x00, // unreachable
			0x0b, // end if
			0x20, 0x01, 0x0b,
		}))),
	)
}

func preparedMixedVoidModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func TestPreparedDirectMixedRegisterEntry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		params      []wasm.ValType
		resultIndex int
		args        []uint64
	}{
		{"i32-f64-to-f64", []wasm.ValType{wasm.I32, wasm.F64}, 1, []uint64{I32(7), F64(2.5)}},
		{"i32-f64-to-i32", []wasm.ValType{wasm.I32, wasm.F64}, 0, []uint64{^uint64(0), F64(2.5)}},
		{"f64-i32-to-i32", []wasm.ValType{wasm.F64, wasm.I32}, 1, []uint64{F64(2.5), I32(7)}},
		{"i64-f32-to-f32", []wasm.ValType{wasm.I64, wasm.F32}, 1, []uint64{0x1122334455667788, 0xdeadbeef00000000 | F32(1.5)}},
		{"i32-f32-f64-i64-to-f64", []wasm.ValType{wasm.I32, wasm.F32, wasm.F64, wasm.I64}, 2,
			[]uint64{I32(7), F32(1.5), F64(2.5), 0x1122334455667788}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := MustCompile(hostToWasmMixedIdentityModule(tc.params, tc.resultIndex))
			defer compiled.Close()
			if !compiled.directPreparedBoundedAt(0) {
				t.Fatal("mixed register entry is not bounded")
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
			if fn.directMixedInfo == 0 || !fn.directIsolated {
				t.Fatal("mixed signature did not select isolated direct entry")
			}
			if cache := in.findInvokeCache("f"); cache == nil || !cache.directFloatFast || cache.scalarWideMask&directMixedEnabled == 0 {
				t.Fatal("instance cache did not select mixed direct entry")
			}
			want := tc.args[tc.resultIndex]
			if tc.params[tc.resultIndex] == wasm.I32 || tc.params[tc.resultIndex] == wasm.F32 {
				want = uint64(uint32(want))
			}
			check := func(label string, got []uint64, err error) {
				t.Helper()
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("%s = %v, %v; want %x", label, got, err, want)
				}
			}
			got, err := fn.Invoke(tc.args...)
			check("prepared", got, err)
			got, err = in.Invoke("f", tc.args...)
			check("instance", got, err)
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if !session.state.fast {
				t.Fatal("mixed session did not reserve direct entry")
			}
			got, err = session.Invoke(tc.args...)
			check("session", got, err)
			session.Close()
			if _, err := in.ExportedFunc("f"); err != nil {
				t.Fatal(err)
			}
			got, err = fn.Invoke(tc.args...)
			check("shared fallback", got, err)
			got, err = in.Invoke("f", tc.args...)
			check("instance shared fallback", got, err)
		})
	}
}

func TestPreparedDirectMixedTrapReset(t *testing.T) {
	compiled := MustCompile(preparedMixedTrapModule())
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("mixed trap entry is not bounded")
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
	if fn.directMixedInfo == 0 {
		t.Fatal("mixed trap entry did not select direct path")
	}
	if out, err := fn.Invoke(0, F64(2.5)); err == nil {
		t.Fatalf("prepared trap returned %v", out)
	}
	if out, err := fn.Invoke(1, F64(2.5)); err != nil || len(out) != 1 || out[0] != F64(2.5) {
		t.Fatalf("prepared after trap = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", 0, F64(2.5)); err == nil {
		t.Fatalf("instance trap returned %v", out)
	}
	if out, err := in.Invoke("f", 1, F64(2.5)); err != nil || len(out) != 1 || out[0] != F64(2.5) {
		t.Fatalf("instance after trap = %v, %v", out, err)
	}
}

func TestPreparedDirectMixedVoid(t *testing.T) {
	compiled := MustCompile(preparedMixedVoidModule())
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	if fn.directMixedInfo == 0 {
		t.Fatal("void mixed entry did not select direct path")
	}
	if out, err := fn.Invoke(^uint64(0), F64(2.5)); err != nil || len(out) != 0 {
		t.Fatalf("prepared = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", ^uint64(0), F64(2.5)); err != nil || len(out) != 0 {
		t.Fatalf("instance = %v, %v", out, err)
	}
}
