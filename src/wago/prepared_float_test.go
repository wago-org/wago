//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
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

func TestPreparedDirectFloatPair(t *testing.T) {
	compiled := MustCompile(hostToWasmF64SignatureModule(2, 2))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("float pair register entry is not bounded")
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
	if !fn.directFloatFast || !fn.directIsolated {
		t.Fatal("float pair did not select bounded direct entry")
	}
	args := []uint64{F64(1.5), F64(-2.5)}
	check := func(label string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != 2 || got[0] != args[0] || got[1] != args[1] {
			t.Fatalf("%s = %v, %v; want %v", label, got, err, args)
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
	if !session.state.fast {
		t.Fatal("float pair session did not reserve direct entry")
	}
	got, err = session.Invoke(args...)
	check("session", got, err)
	session.Close()
	if _, err := in.ExportedFunc("f"); err != nil {
		t.Fatal(err)
	}
	got, err = fn.Invoke(args...)
	check("shared fallback", got, err)
	got, err = in.Invoke("f", args...)
	check("instance shared fallback", got, err)
}

func TestPreparedDirectFloatQuad(t *testing.T) {
	compiled := MustCompile(hostToWasmF64SignatureModule(4, 4))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("float quad register entry is not bounded")
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
	if !fn.directFloatFast || !fn.directIsolated {
		t.Fatal("float quad did not select bounded direct entry")
	}
	args := []uint64{F64(1.5), F64(-2.5), F64(3.5), F64(-4.5)}
	check := func(label string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != 4 {
			t.Fatalf("%s = %v, %v; want %v", label, got, err, args)
		}
		for i := range args {
			if got[i] != args[i] {
				t.Fatalf("%s[%d] = %x; want %x", label, i, got[i], args[i])
			}
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
	got, err = in.Invoke("f", args...)
	check("instance shared fallback", got, err)
}

func TestPreparedDirectFloatPenta(t *testing.T) {
	for _, n := range []int{5, 8} {
		t.Run(fmt.Sprintf("f64x%d", n), func(t *testing.T) {
			testPreparedDirectFloatWide(t, n)
		})
	}
}

func testPreparedDirectFloatWide(t *testing.T, n int) {
	compiled := MustCompile(hostToWasmF64SignatureModule(n, n))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("wide float register entry is not bounded")
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
	if !fn.directFloatFast || !fn.directIsolated {
		t.Fatal("wide float did not select bounded direct entry")
	}
	args := make([]uint64, n)
	for i := range args {
		args[i] = F64(float64(i) + 1.5)
	}
	check := func(label string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != len(args) {
			t.Fatalf("%s = %v, %v; want %v", label, got, err, args)
		}
		for i := range args {
			if got[i] != args[i] {
				t.Fatalf("%s[%d] = %x; want %x", label, i, got[i], args[i])
			}
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
	defer session.Close()
	got, err = session.Invoke(args...)
	check("session", got, err)
}

func TestFloatOctInternalCall(t *testing.T) {
	const n = 8
	types := make([]wasm.ValType, n)
	leaf, caller := make([]byte, 0, 2*n+1), make([]byte, 0, 2*n+3)
	for i := range types {
		types[i] = wasm.F64
		leaf = append(leaf, 0x20, byte(i))
		caller = append(caller, 0x20, byte(i))
	}
	leaf = append(leaf, 0x0b)
	caller = append(caller, 0x10, 0x00, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(leaf), wasmtest.Code(caller))),
	)
	compiled := MustCompile(mod)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := make([]uint64, n)
	for i := range args {
		args[i] = F64(float64(i) + 1.5)
	}
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != n {
		t.Fatalf("internal float-oct call = %v, %v", got, err)
	}
	for i := range got {
		if got[i] != args[i] {
			t.Fatalf("result[%d] = %x, want %x", i, got[i], args[i])
		}
	}
}

func TestPreparedDirectFloatOctWidths(t *testing.T) {
	const n = 8
	types := make([]wasm.ValType, n)
	body := make([]byte, 0, 2*n+1)
	args, want := make([]uint64, n), make([]uint64, n)
	for i := range types {
		body = append(body, 0x20, byte(i))
		if i%2 == 0 {
			types[i] = wasm.F32
			args[i], want[i] = 0xffffffff7fc12345, 0x7fc12345
		} else {
			types[i] = wasm.F64
			args[i], want[i] = F64(float64(i)+0.5), F64(float64(i)+0.5)
		}
	}
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled := MustCompile(mod)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil || !fn.directFloatFast {
		t.Fatalf("wide mixed-width float entry = %v, %v", fn, err)
	}
	for label, invoke := range map[string]func() ([]uint64, error){
		"prepared": func() ([]uint64, error) { return fn.Invoke(args...) },
		"instance": func() ([]uint64, error) { return in.Invoke("f", args...) },
	} {
		got, err := invoke()
		if err != nil || len(got) != n {
			t.Fatalf("%s = %v, %v", label, got, err)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s[%d] = %x, want %x", label, i, got[i], want[i])
			}
		}
	}
}

func preparedFloatQuadWidthsModule(call bool) []byte {
	types := []wasm.ValType{wasm.F32, wasm.F64, wasm.F32, wasm.F64}
	leaf := wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x0b})
	if !call {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(leaf)),
		)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			leaf,
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x10, 0x00, 0x0b}),
		)),
	)
}

func TestPreparedDirectFloatQuadWidths(t *testing.T) {
	compiled := MustCompile(preparedFloatQuadWidthsModule(false))
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
	if !fn.directFloatFast || !fn.directIsolated {
		t.Fatal("mixed-width float quad did not select bounded direct entry")
	}
	args := []uint64{0xffffffff7fc12345, 0x7ff8000000001234, 0xffffffff3f800000, F64(-2.5)}
	want := []uint64{0x7fc12345, 0x7ff8000000001234, 0x3f800000, F64(-2.5)}
	for label, invoke := range map[string]func() ([]uint64, error){
		"prepared": func() ([]uint64, error) { return fn.Invoke(args...) },
		"instance": func() ([]uint64, error) { return in.Invoke("f", args...) },
	} {
		got, err := invoke()
		if err != nil || len(got) != 4 {
			t.Fatalf("%s = %v, %v; want %v", label, got, err, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s[%d] = %x; want %x", label, i, got[i], want[i])
			}
		}
	}
}

func TestFloatQuadRegisterCallBetweenWasmFunctions(t *testing.T) {
	compiled := MustCompile(preparedFloatQuadWidthsModule(true))
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := []uint64{0x7fc12345, 0x7ff8000000001234, 0x3f800000, F64(-2.5)}
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != 4 {
		t.Fatalf("Wasm register call = %v, %v; want %v", got, err, args)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf("Wasm register call[%d] = %x; want %x", i, got[i], args[i])
		}
	}
}

func TestFloatPairRegisterCallBetweenWasmFunctions(t *testing.T) {
	types := []wasm.ValType{wasm.F32, wasm.F64}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("leaf", 0, 0), wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}),
		)),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := []uint64{0xffffffff7fc12345, 0x7ff8000000001234}
	leaf, err := in.Invoke("leaf", args...)
	if err != nil || len(leaf) != 2 || leaf[0] != uint64(uint32(args[0])) || leaf[1] != args[1] {
		t.Fatalf("leaf = %v, %v", leaf, err)
	}
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != 2 || got[0] != uint64(uint32(args[0])) || got[1] != args[1] {
		t.Fatalf("Wasm register call = %v, %v; want [%x %x]", got, err, uint32(args[0]), args[1])
	}
}

func TestPreparedDirectFloatPairWidths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []wasm.ValType
		args  []uint64
		want  []uint64
	}{
		{"f32-f64", []wasm.ValType{wasm.F32, wasm.F64}, []uint64{0xffffffff7fc12345, 0x7ff8000000001234}, []uint64{0x7fc12345, 0x7ff8000000001234}},
		{"f64-f32", []wasm.ValType{wasm.F64, wasm.F32}, []uint64{0x7ff8000000001234, 0xffffffff7fc12345}, []uint64{0x7ff8000000001234, 0x7fc12345}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(tc.types, tc.types))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}))),
			)
			compiled := MustCompile(module)
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
			if !fn.directFloatFast || !fn.directIsolated {
				t.Fatal("mixed-width pair did not select bounded direct entry")
			}
			for label, invoke := range map[string]func() ([]uint64, error){
				"prepared": func() ([]uint64, error) { return fn.Invoke(tc.args...) },
				"instance": func() ([]uint64, error) { return in.Invoke("f", tc.args...) },
			} {
				got, err := invoke()
				if err != nil || len(got) != 2 || got[0] != tc.want[0] || got[1] != tc.want[1] {
					t.Fatalf("%s = %v, %v; want %v", label, got, err, tc.want)
				}
			}
		})
	}
}

func TestPreparedDirectFloatPairTrapReset(t *testing.T) {
	body := []byte{0x20, 0x00, 0x44} // local.get 0; f64.const 0
	body = append(body, make([]byte, 8)...)
	body = append(body,
		0x61,       // f64.eq
		0x04, 0x40, // if
		0x00, // unreachable
		0x0b, // end if
		0x20, 0x00, 0x20, 0x01, 0x0b,
	)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.F64, wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled := MustCompile(module)
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
	if !fn.directFloatFast || !fn.directIsolated {
		t.Fatal("trapping float pair did not select bounded direct entry")
	}
	if _, err := fn.Invoke(F64(0), F64(2.5)); err == nil {
		t.Fatal("expected float pair trap")
	}
	got, err := fn.Invoke(F64(1.5), F64(2.5))
	if err != nil || len(got) != 2 || got[0] != F64(1.5) || got[1] != F64(2.5) {
		t.Fatalf("after trap = %v, %v", got, err)
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
