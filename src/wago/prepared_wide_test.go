//go:build (amd64 || arm64) && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedWideModule(n int) []byte {
	params := make([]wasm.ValType, n)
	for i := range params {
		if i%2 == 0 {
			params[i] = wasm.I32
		} else {
			params[i] = wasm.I64
		}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{params[n-1]}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, byte(n - 1), 0x0b}))),
	)
}

func preparedWidePairModule(n int) []byte {
	params := make([]wasm.ValType, n)
	for i := range params {
		if i%2 == 0 {
			params[i] = wasm.I32
		} else {
			params[i] = wasm.I64
		}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{params[0], params[n-1]}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, byte(n - 1), 0x0b}))),
	)
}

func preparedWideTrapModule(n int) []byte {
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.I32
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x00, // unreachable
			0x0b, // end if
			0x20, byte(n - 1), 0x0b,
		}))),
	)
}

func preparedWideVoidModule(n int) []byte {
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.I32
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func TestPreparedDirectWideIntegerArguments(t *testing.T) {
	max := 7
	if runtime.GOARCH == "arm64" {
		max = 8
	}
	for n := 5; n <= max; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			compiled := MustCompile(preparedWideModule(n))
			defer compiled.Close()
			if !compiled.directPreparedBoundedAt(0) {
				t.Fatal("compiler did not prove wide entry bounded")
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
			if !fn.directIntFast || !fn.directIntBounded || !fn.directIsolated {
				t.Fatal("wide entry did not select bounded direct path")
			}
			args := make([]uint64, n)
			for i := range args {
				args[i] = 0x1122334455667700 | uint64(i+1)
			}
			want := args[n-1]
			if n%2 != 0 {
				want = uint64(uint32(want))
			}
			check := func(label string, got []uint64, err error) {
				t.Helper()
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("%s = %v, %v; want %x", label, got, err, want)
				}
			}
			got, err := fn.Invoke(args...)
			check("prepared", got, err)
			got, err = in.Invoke("f", args...)
			check("instance", got, err)
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			got, err = session.Invoke(args...)
			check("session", got, err)
			session.Close()
			if _, err := in.ExportedFunc("f"); err != nil {
				t.Fatal(err)
			}
			got, err = fn.Invoke(args...)
			check("shared fallback", got, err)
		})
	}
}

func TestPreparedDirectWidePair(t *testing.T) {
	n := 7
	if runtime.GOARCH == "arm64" {
		n = 8
	}
	compiled := MustCompile(preparedWidePairModule(n))
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
	if !fn.directIntFast || !fn.directIntBounded {
		t.Fatal("wide pair did not select bounded direct entry")
	}
	args := make([]uint64, n)
	args[0] = ^uint64(0)
	args[n-1] = 0x1122334455667788
	wantLast := args[n-1]
	if n%2 != 0 {
		wantLast = uint64(uint32(wantLast))
	}
	check := func(label string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != 2 || got[0] != uint64(^uint32(0)) || got[1] != wantLast {
			t.Fatalf("%s = %v, %v; want [%x %x]", label, got, err, uint64(^uint32(0)), wantLast)
		}
	}
	got, err := fn.Invoke(args...)
	check("prepared", got, err)
	got, err = in.Invoke("f", args...)
	check("instance", got, err)
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	got, err = session.Invoke(args...)
	check("session", got, err)
	session.Close()
}

func TestPreparedDirectWideTrapReset(t *testing.T) {
	n := 7
	if runtime.GOARCH == "arm64" {
		n = 8
	}
	compiled := MustCompile(preparedWideTrapModule(n))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("wide trapping entry is not bounded")
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
		t.Fatal("wide trapping entry did not select direct path")
	}
	args := make([]uint64, n)
	args[n-1] = 0x1234
	if out, err := fn.Invoke(args...); err == nil {
		t.Fatalf("prepared trap returned %v", out)
	}
	args[0] = 1
	if out, err := fn.Invoke(args...); err != nil || len(out) != 1 || out[0] != 0x1234 {
		t.Fatalf("prepared call after trap = %v, %v", out, err)
	}
	args[0] = 0
	if out, err := in.Invoke("f", args...); err == nil {
		t.Fatalf("instance trap returned %v", out)
	}
	args[0] = 1
	if out, err := in.Invoke("f", args...); err != nil || len(out) != 1 || out[0] != 0x1234 {
		t.Fatalf("instance call after trap = %v, %v", out, err)
	}
}

func TestPreparedDirectWideVoid(t *testing.T) {
	compiled := MustCompile(preparedWideVoidModule(7))
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
	if !fn.directIntFast {
		t.Fatal("void wide entry did not select direct path")
	}
	args := make([]uint64, 7)
	if out, err := fn.Invoke(args...); err != nil || len(out) != 0 {
		t.Fatalf("prepared = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", args...); err != nil || len(out) != 0 {
		t.Fatalf("instance = %v, %v", out, err)
	}
}

func TestPreparedDirectWideAmd64StackArgumentFallback(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("amd64 uses a stack argument beyond seven integer parameters")
	}
	compiled := MustCompile(preparedWideModule(8))
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
	if fn.directIntFast {
		t.Fatal("eight-argument amd64 entry must retain stack-argument fallback")
	}
	args := make([]uint64, 8)
	args[7] = 0x1122334455667788
	if out, err := fn.Invoke(args...); err != nil || len(out) != 1 || out[0] != args[7] {
		t.Fatalf("prepared fallback = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", args...); err != nil || len(out) != 1 || out[0] != args[7] {
		t.Fatalf("instance fallback = %v, %v", out, err)
	}
}
