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

func preparedMixedPairModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64, wasm.I64}, []wasm.ValType{wasm.I32, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x02, 0x0b}))),
	)
}

func preparedMixedNumericPairModule(params []wasm.ValType, first, second int) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{params[first], params[second]}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, byte(first), 0x20, byte(second), 0x0b}))),
	)
}

func preparedMixedWideResultsModule(call bool) []byte {
	return preparedMixedReorderedResultsModule([]int{0, 1, 2, 3}, call)
}

func preparedMixedReorderedResultsModule(order []int, call bool) []byte {
	types := []wasm.ValType{wasm.I32, wasm.F64, wasm.I64, wasm.F32}
	results := make([]wasm.ValType, len(order))
	body := make([]byte, 0, len(order)*2+1)
	for i, index := range order {
		results[i] = types[index]
		body = append(body, 0x20, byte(index))
	}
	leaf := wasmtest.Code(append(body, 0x0b))
	if call {
		// A conditional in the callee prevents straight-line inlining, so this
		// exercises the native mixed-result call/return bank transfer.
		guarded := append([]byte{0x20, 0x00, 0x45, 0x04, 0x40, 0x00, 0x0b}, body...)
		leaf = wasmtest.Code(append(guarded, 0x0b))
		caller := []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x10, 0x00, 0x0b}
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, results))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(leaf, wasmtest.Code(caller))),
		)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(leaf)),
	)
}

func TestPreparedMixedWideResultOrders(t *testing.T) {
	args := []uint64{7, F64(-2.5), 0x1122334455667788, F32(1.5)}
	for _, order := range [][]int{
		{0, 1, 2}, {1, 0, 3}, {0, 2, 1},
		{0, 1, 2, 3}, {1, 0, 3, 2}, {0, 2, 1, 3}, {3, 2, 1, 0},
	} {
		compiled := MustCompile(preparedMixedReorderedResultsModule(order, false))
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("f")
		if err != nil || !fn.directIsolated || fn.directMixedInfo == 0 {
			t.Fatalf("order %v did not select direct mixed entry: %v", order, err)
		}
		got, err := fn.Invoke(args...)
		if err != nil || len(got) != len(order) {
			t.Fatalf("order %v: got %v, %v", order, got, err)
		}
		for i, index := range order {
			if got[i] != args[index] {
				t.Fatalf("order %v result[%d] = %x, want %x", order, i, got[i], args[index])
			}
		}
		in.Close()
		compiled.Close()
	}
}

func TestPreparedDirectMixedWideResults(t *testing.T) {
	compiled := MustCompile(preparedMixedWideResultsModule(false))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("mixed four-result register entry is not bounded")
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
	if !fn.directIsolated || fn.directMixedInfo == 0 {
		t.Fatal("mixed four-result call did not select the direct entry")
	}
	args := []uint64{0xffffffff00000007, F64(-2.5), 0x1122334455667788, 0xffffffff7fc12345}
	want := []uint64{7, F64(-2.5), 0x1122334455667788, 0x7fc12345}
	for label, invoke := range map[string]func() ([]uint64, error){
		"prepared": func() ([]uint64, error) { return fn.Invoke(args...) },
		"instance": func() ([]uint64, error) { return in.Invoke("f", args...) },
	} {
		got, err := invoke()
		if err != nil || len(got) != len(want) {
			t.Fatalf("%s = %v, %v", label, got, err)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s[%d] = %x, want %x", label, i, got[i], want[i])
			}
		}
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	got, err := session.Invoke(args...)
	if err != nil || len(got) != len(want) {
		t.Fatalf("session = %v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("session[%d] = %x, want %x", i, got[i], want[i])
		}
	}
	session.Close()
	if _, err := in.ExportedFunc("f"); err != nil {
		t.Fatal(err)
	}
	got, err = fn.Invoke(args...)
	if err != nil || len(got) != len(want) {
		t.Fatalf("shared fallback = %v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shared fallback[%d] = %x, want %x", i, got[i], want[i])
		}
	}
}

func TestMixedWideResultsInternalCall(t *testing.T) {
	args := []uint64{7, F64(-2.5), 0x1122334455667788, F32(1.5)}
	for _, order := range [][]int{{0, 1, 2}, {3, 2, 1, 0}} {
		compiled := MustCompile(preparedMixedReorderedResultsModule(order, true))
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("f", args...)
		if err != nil || len(got) != len(order) {
			t.Fatalf("mixed wide internal call %v = %v, %v", order, got, err)
		}
		for i, index := range order {
			if got[i] != args[index] {
				t.Fatalf("order %v result[%d] = %x, want %x", order, i, got[i], args[index])
			}
		}
		in.Close()
		compiled.Close()
	}
}

func TestPreparedDirectMixedNumericPair(t *testing.T) {
	for _, tc := range []struct {
		name          string
		params        []wasm.ValType
		first, second int
		args          []uint64
		want          []uint64
	}{
		{"int-float", []wasm.ValType{wasm.I32, wasm.F64}, 0, 1,
			[]uint64{0xffffffff00000007, F64(-2.5)}, []uint64{7, F64(-2.5)}},
		{"float-int", []wasm.ValType{wasm.F32, wasm.I64}, 0, 1,
			[]uint64{0xffffffff7fc12345, 0x1122334455667788}, []uint64{0x7fc12345, 0x1122334455667788}},
		{"mixed-params-float-float", []wasm.ValType{wasm.I32, wasm.F32, wasm.F64}, 1, 2,
			[]uint64{7, 0xffffffff7fc12345, 0x7ff8000000001234}, []uint64{0x7fc12345, 0x7ff8000000001234}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := MustCompile(preparedMixedNumericPairModule(tc.params, tc.first, tc.second))
			defer compiled.Close()
			if !compiled.directPreparedBoundedAt(0) {
				t.Fatal("mixed numeric pair register entry is not bounded")
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
				t.Fatal("mixed numeric pair did not select isolated direct entry")
			}
			check := func(label string, got []uint64, err error) {
				t.Helper()
				if err != nil || len(got) != 2 || got[0] != tc.want[0] || got[1] != tc.want[1] {
					t.Fatalf("%s = %v, %v; want %v", label, got, err, tc.want)
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

func TestMixedNumericPairRegisterCallBetweenWasmFunctions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []wasm.ValType
		args  []uint64
	}{
		{"int-float", []wasm.ValType{wasm.I32, wasm.F64}, []uint64{I32(7), F64(-2.5)}},
		{"float-int", []wasm.ValType{wasm.F64, wasm.I32}, []uint64{F64(-2.5), I32(7)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(tc.types, tc.types))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
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
			got, err := in.Invoke("f", tc.args...)
			if err != nil || len(got) != 2 || got[0] != tc.args[0] || got[1] != tc.args[1] {
				t.Fatalf("Wasm register call = %v, %v; want %v", got, err, tc.args)
			}
		})
	}
}

func TestPreparedDirectMixedNumericPairTrapReset(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.I32, wasm.F64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x00, // unreachable
			0x0b, // end if
			0x20, 0x00, 0x20, 0x01, 0x0b,
		}))),
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
	if !fn.directIsolated || fn.directMixedInfo == 0 {
		t.Fatal("trapping mixed pair did not select bounded direct entry")
	}
	if _, err := fn.Invoke(0, F64(2.5)); err == nil {
		t.Fatal("expected mixed pair trap")
	}
	got, err := fn.Invoke(7, F64(2.5))
	if err != nil || len(got) != 2 || got[0] != 7 || got[1] != F64(2.5) {
		t.Fatalf("after trap = %v, %v", got, err)
	}
}

func TestPreparedDirectMixedPair(t *testing.T) {
	compiled := MustCompile(preparedMixedPairModule())
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("mixed pair register entry is not bounded")
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
		t.Fatal("mixed pair did not select isolated direct entry")
	}
	args := []uint64{0xffffffff00000007, F64(2.5), 0x1122334455667788}
	want := []uint64{7, 0x1122334455667788}
	check := func(label string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("%s = %v, %v; want %v", label, got, err, want)
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
	if !session.state.fast {
		t.Fatal("mixed pair session did not reserve direct entry")
	}
	got, err = session.Invoke(args...)
	check("session", got, err)
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
