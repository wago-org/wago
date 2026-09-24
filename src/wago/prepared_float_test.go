//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedFloatConstModule() []byte {
	body := make([]byte, 10)
	body[0] = 0x44 // f64.const
	binary.LittleEndian.PutUint64(body[1:], F64(1.5))
	body[9] = 0x0b
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func preparedFloatMixedModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.F32, wasm.F64, wasm.F32, wasm.F64}, []wasm.ValType{wasm.F32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x02, 0x0b}))),
	)
}

func preparedFloatTrapModule() []byte {
	body := []byte{0x20, 0x00, 0x44} // local.get 0; f64.const 0
	body = append(body, make([]byte, 8)...)
	body = append(body,
		0x61,       // f64.eq
		0x04, 0x40, // if
		0x00, // unreachable
		0x0b, // end if
		0x20, 0x00, 0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func preparedFloatVoidModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F32, wasm.F64}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func TestPreparedDirectFloatRegisterEntry(t *testing.T) {
	for _, params := range []int{1, 2, 4} {
		compiled := MustCompile(hostToWasmF64SignatureModule(params, 1))
		if !compiled.directPreparedBoundedAt(0) {
			compiled.Close()
			t.Fatalf("f64x%d register entry is not bounded", params)
		}
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("f")
		if err != nil {
			in.Close()
			compiled.Close()
			t.Fatal(err)
		}
		if !fn.directFloatFast || !fn.directIsolated {
			in.Close()
			compiled.Close()
			t.Fatalf("f64x%d did not select bounded direct float entry", params)
		}
		if cache := in.findInvokeCache("f"); cache == nil || !cache.directFloatFast {
			in.Close()
			compiled.Close()
			t.Fatal("instance cache did not select bounded direct float entry")
		}
		args := make([]uint64, params)
		for i := range args {
			args[i] = F64(float64(i) + 1.5)
		}
		check := func(label string, out []uint64, err error) {
			t.Helper()
			if err != nil || len(out) != 1 || out[0] != args[0] {
				t.Fatalf("%s = %v, %v; want %x", label, out, err, args[0])
			}
		}
		out, err := fn.Invoke(args...)
		check("prepared", out, err)
		out, err = in.Invoke("f", args...)
		check("instance", out, err)
		session, err := fn.OpenSession()
		if err != nil {
			in.Close()
			compiled.Close()
			t.Fatal(err)
		}
		if !session.state.fast {
			session.Close()
			in.Close()
			compiled.Close()
			t.Fatal("float session did not reserve direct entry")
		}
		out, err = session.Invoke(args...)
		check("session", out, err)
		session.Close()
		if _, err := in.ExportedFunc("f"); err != nil {
			in.Close()
			compiled.Close()
			t.Fatal(err)
		}
		out, err = fn.Invoke(args...)
		check("shared fallback", out, err)
		out, err = in.Invoke("f", args...)
		check("instance shared fallback", out, err)
		in.Close()
		compiled.Close()
	}
}

func TestPreparedDirectFloatMixedWidths(t *testing.T) {
	compiled := MustCompile(preparedFloatMixedModule())
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
	if !fn.directFloatFast {
		t.Fatal("mixed-width float did not select direct entry")
	}
	args := []uint64{^uint64(0), F64(2.5), ^uint64(0), F64(4.5)}
	want := uint64(uint32(args[2]))
	if out, err := fn.Invoke(args...); err != nil || len(out) != 1 || out[0] != want {
		t.Fatalf("prepared = %v, %v; want %x", out, err, want)
	}
	if out, err := in.Invoke("f", args...); err != nil || len(out) != 1 || out[0] != want {
		t.Fatalf("instance = %v, %v; want %x", out, err, want)
	}
}

func TestPreparedDirectFloatTrapReset(t *testing.T) {
	compiled := MustCompile(preparedFloatTrapModule())
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("float trapping entry is not bounded")
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
	if !fn.directFloatFast {
		t.Fatal("float trapping entry did not select direct path")
	}
	if out, err := fn.Invoke(F64(0)); err == nil {
		t.Fatalf("prepared trap returned %v", out)
	}
	if out, err := fn.Invoke(F64(1.5)); err != nil || len(out) != 1 || out[0] != F64(1.5) {
		t.Fatalf("prepared after trap = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", F64(0)); err == nil {
		t.Fatalf("instance trap returned %v", out)
	}
	if out, err := in.Invoke("f", F64(1.5)); err != nil || len(out) != 1 || out[0] != F64(1.5) {
		t.Fatalf("instance after trap = %v, %v", out, err)
	}
}

func TestPreparedDirectFloatVoidAndNaNPayload(t *testing.T) {
	voidCompiled := MustCompile(preparedFloatVoidModule())
	defer voidCompiled.Close()
	voidInstance, err := Instantiate(voidCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer voidInstance.Close()
	voidFn, err := voidInstance.WasmFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	if !voidFn.directFloatFast {
		t.Fatal("void float entry did not select direct path")
	}
	if out, err := voidFn.Invoke(^uint64(0), F64(2.5)); err != nil || len(out) != 0 {
		t.Fatalf("void prepared = %v, %v", out, err)
	}
	if out, err := voidInstance.Invoke("f", ^uint64(0), F64(2.5)); err != nil || len(out) != 0 {
		t.Fatalf("void instance = %v, %v", out, err)
	}

	nanCompiled := MustCompile(hostToWasmF64SignatureModule(1, 1))
	defer nanCompiled.Close()
	nanInstance, err := Instantiate(nanCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer nanInstance.Close()
	nanFn, err := nanInstance.WasmFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	const payload = uint64(0x7ff80000abcd1234)
	if out, err := nanFn.Invoke(payload); err != nil || len(out) != 1 || out[0] != payload {
		t.Fatalf("NaN payload prepared = %v, %v", out, err)
	}
	if out, err := nanInstance.Invoke("f", payload); err != nil || len(out) != 1 || out[0] != payload {
		t.Fatalf("NaN payload instance = %v, %v", out, err)
	}
}

func TestPreparedDirectFloatNoArguments(t *testing.T) {
	compiled := MustCompile(preparedFloatConstModule())
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("zero-argument float entry is not bounded")
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
	if !fn.directFloatFast {
		t.Fatal("zero-argument float did not select direct entry")
	}
	if out, err := fn.Invoke(); err != nil || len(out) != 1 || out[0] != F64(1.5) {
		t.Fatalf("prepared = %v, %v", out, err)
	}
	if out, err := in.Invoke("f"); err != nil || len(out) != 1 || out[0] != F64(1.5) {
		t.Fatalf("instance = %v, %v", out, err)
	}
}
