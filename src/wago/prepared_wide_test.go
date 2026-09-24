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

func TestPreparedIsolatedWideWrapperUsesDirectGate(t *testing.T) {
	compiled := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).MustCompile(hostToWasmI32SignatureModule(16, 16))
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
	if !fn.isolatedFast || fn.directIntFast || fn.directGate == nil {
		t.Fatal("wide wrapper must select isolated direct-gate admission")
	}
	args := make([]uint64, 16)
	for i := range args {
		args[i] = uint64(i + 1)
	}
	before := nextInvocationID.Load()
	got, err := fn.Invoke(args...)
	if err != nil || len(got) != 16 || got[15] != 16 {
		t.Fatalf("direct-gate result = %v, %v", got, err)
	}
	if nextInvocationID.Load() != before {
		t.Fatal("isolated wide wrapper allocated an invocation identity")
	}
	if _, err := in.ExportedFunc("f"); err != nil {
		t.Fatal(err)
	}
	got, err = fn.Invoke(args...)
	if err != nil || len(got) != 16 || got[15] != 16 {
		t.Fatalf("shared fallback result = %v, %v", got, err)
	}
	if nextInvocationID.Load() == before {
		t.Fatal("shared fallback omitted invocation identity")
	}
}

func TestInstanceIsolatedWideWrapperUsesDirectGate(t *testing.T) {
	compiled := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).MustCompile(hostToWasmI32SignatureModule(16, 16))
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := make([]uint64, 16)
	for i := range args {
		args[i] = uint64(i + 1)
	}
	// Warm the cache; its fill is an ordinary admitted operation.
	if _, err := in.Invoke("f", args...); err != nil {
		t.Fatal(err)
	}
	before := nextInvocationID.Load()
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != 16 || got[15] != 16 {
		t.Fatalf("direct-gate result = %v, %v", got, err)
	}
	if nextInvocationID.Load() != before {
		t.Fatal("isolated wide Invoke allocated an invocation identity")
	}
	if _, err := in.ExportedFunc("f"); err != nil {
		t.Fatal(err)
	}
	got, err = in.Invoke("f", args...)
	if err != nil || len(got) != 16 || got[15] != 16 {
		t.Fatalf("shared fallback result = %v, %v", got, err)
	}
	if nextInvocationID.Load() == before {
		t.Fatal("shared fallback omitted invocation identity")
	}
}

func TestIsolatedWideWrapperTrapReset(t *testing.T) {
	compiled := MustCompile(preparedWideTrapModule(16))
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
	args := make([]uint64, 16)
	args[15] = 99
	for _, call := range []struct {
		name string
		fn   func([]uint64) ([]uint64, error)
	}{
		{"prepared", func(a []uint64) ([]uint64, error) { return fn.Invoke(a...) }},
		{"instance", func(a []uint64) ([]uint64, error) { return in.Invoke("f", a...) }},
	} {
		t.Run(call.name, func(t *testing.T) {
			if _, err := call.fn(args); err == nil {
				t.Fatal("expected unreachable trap")
			}
			args[0] = 1
			got, err := call.fn(args)
			if err != nil || len(got) != 1 || got[0] != 99 {
				t.Fatalf("after trap = %v, %v", got, err)
			}
			args[0] = 0
		})
	}
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Invoke(args...); err == nil {
		s.Close()
		t.Fatal("session expected unreachable trap")
	}
	args[0] = 1
	got, err := s.Invoke(args...)
	s.Close()
	if err != nil || len(got) != 1 || got[0] != 99 {
		t.Fatalf("session after trap = %v, %v", got, err)
	}
}

func TestIsolatedWideWrapperSessionUsesFastReservation(t *testing.T) {
	compiled := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).MustCompile(hostToWasmI32SignatureModule(16, 16))
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
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.state.fast {
		t.Fatal("isolated wide wrapper did not reserve the fast gate")
	}
	args := make([]uint64, 16)
	for i := range args {
		args[i] = uint64(i + 1)
	}
	for i := 0; i < 2; i++ {
		got, err := s.Invoke(args...)
		if err != nil || len(got) != 16 {
			t.Fatalf("session result = %v, %v", got, err)
		}
		for j, value := range got {
			if value != uint64(j+1) {
				t.Fatalf("session result[%d] = %d, want %d", j, value, j+1)
			}
		}
	}
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
	max := 8
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
	n := 8
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

func TestPreparedDirectIntegerQuad(t *testing.T) {
	compiled := MustCompile(hostToWasmI32SignatureModule(4, 4))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("integer quad register entry is not bounded")
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
	if !fn.directIntFast || !fn.directIntBounded {
		t.Fatal("integer quad did not select bounded direct entry")
	}
	args := []uint64{1, 2, 3, 4}
	check := func(label string, got []uint64, err error) {
		t.Helper()
		if err != nil || len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
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
	got, err = session.Invoke(args...)
	check("session", got, err)
	session.Close()
}

func preparedIntegerMultiModule(params, results int) []byte {
	paramTypes := make([]wasm.ValType, params)
	resultTypes := make([]wasm.ValType, results)
	body := make([]byte, 0, results*2+1)
	for i := range paramTypes {
		paramTypes[i] = wasm.I32
		if i%2 != 0 {
			paramTypes[i] = wasm.I64
		}
	}
	for i := range resultTypes {
		resultTypes[i] = paramTypes[i]
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(paramTypes, resultTypes))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestPreparedDirectIntegerMultiWidths(t *testing.T) {
	maxParams := 8
	for _, tc := range []struct{ params, results int }{{4, 3}, {maxParams, 4}, {5, 5}, {maxParams, maxParams}} {
		t.Run(fmt.Sprintf("%d-%d", tc.params, tc.results), func(t *testing.T) {
			compiled := MustCompile(preparedIntegerMultiModule(tc.params, tc.results))
			defer compiled.Close()
			if !compiled.directPreparedBoundedAt(0) {
				t.Fatal("multi-result entry is not bounded")
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
			if !fn.directIntFast || !fn.directIntBounded {
				t.Fatal("multi-result call did not select bounded direct entry")
			}
			args := make([]uint64, tc.params)
			want := make([]uint64, tc.results)
			for i := range args {
				args[i] = 0xffffffff00000000 | uint64(i+1)
				if i < tc.results {
					want[i] = args[i]
					if i%2 == 0 {
						want[i] = uint64(uint32(args[i]))
					}
				}
			}
			check := func(label string, got []uint64, err error) {
				t.Helper()
				if err != nil || len(got) != len(want) {
					t.Fatalf("%s = %v, %v; want %v", label, got, err, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("%s[%d] = %x; want %x", label, i, got[i], want[i])
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
		})
	}
}

func TestIntegerQuadRegisterCallBetweenWasmFunctions(t *testing.T) {
	quad := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(quad, quad))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x10, 0x00, 0x0b}),
		)),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f", 1, 2, 3, 4)
	if err != nil || len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("Wasm register call = %v, %v", got, err)
	}
}

func TestIntegerQuintRegisterCallBetweenWasmFunctions(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.I32, wasm.I64, wasm.I32}
	identity := []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x20, 0x04}
	caller := append(append([]byte{}, identity...), 0x10, 0x00, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(append(identity, 0x0b)),
			wasmtest.Code(caller),
		)),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := []uint64{1, 0x1122334455667788, 3, 0x8877665544332211, 5}
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != len(args) {
		t.Fatalf("Wasm register call = %v, %v; want %v", got, err, args)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf("Wasm register call[%d] = %x; want %x", i, got[i], args[i])
		}
	}
}

func TestIntegerMaxRegisterResultsBetweenWasmFunctions(t *testing.T) {
	n := 8
	types := make([]wasm.ValType, n)
	identity := make([]byte, 0, 2*n)
	args := make([]uint64, n)
	for i := range types {
		types[i] = wasm.I32
		args[i] = uint64(i + 1)
		if i%2 != 0 {
			types[i] = wasm.I64
			args[i] |= 0x1122334400000000
		}
		identity = append(identity, 0x20, byte(i))
	}
	caller := append(append([]byte{}, identity...), 0x10, 0x00, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(append(identity, 0x0b)),
			wasmtest.Code(caller),
		)),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != len(args) {
		t.Fatalf("Wasm register call = %v, %v; want %v", got, err, args)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf("Wasm register call[%d] = %x; want %x", i, got[i], args[i])
		}
	}
}

func TestPreparedDirectIntegerQuintTrapReset(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.I32, wasm.I64, wasm.I32}
	body := []byte{0x20, 0x00, 0x45, 0x04, 0x40, 0x00, 0x0b}
	for i := range types {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("integer quint trap entry is not bounded")
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
		t.Fatal("integer quint trap entry did not select direct path")
	}
	if _, err := fn.Invoke(0, 2, 3, 4, 5); err == nil {
		t.Fatal("expected trap")
	}
	args := []uint64{1, 0x1122334455667788, 3, 0x8877665544332211, 5}
	got, err := fn.Invoke(args...)
	if err != nil || len(got) != len(args) {
		t.Fatalf("post-trap call = %v, %v; want %v", got, err, args)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf("post-trap call[%d] = %x; want %x", i, got[i], args[i])
		}
	}
}

func TestPreparedDirectIntegerMaxTrapReset(t *testing.T) {
	n := 8
	types := make([]wasm.ValType, n)
	args := make([]uint64, n)
	trapArgs := make([]uint64, n)
	body := []byte{0x20, 0x00, 0x45, 0x04, 0x40, 0x00, 0x0b}
	for i := range types {
		types[i] = wasm.I32
		args[i] = uint64(i + 1)
		if i%2 != 0 {
			types[i] = wasm.I64
			args[i] |= 0x1122334400000000
		}
		trapArgs[i] = args[i]
		body = append(body, 0x20, byte(i))
	}
	trapArgs[0] = 0
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("max-result trap entry is not bounded")
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
		t.Fatal("max-result trap entry did not select direct path")
	}
	if _, err := fn.Invoke(trapArgs...); err == nil {
		t.Fatal("expected trap")
	}
	got, err := fn.Invoke(args...)
	if err != nil || len(got) != len(args) {
		t.Fatalf("post-trap call = %v, %v; want %v", got, err, args)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf("post-trap call[%d] = %x; want %x", i, got[i], args[i])
		}
	}
}

func TestPreparedDirectIntegerQuadTrapReset(t *testing.T) {
	quad := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(quad, quad))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x00, // unreachable
			0x0b, // end if
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x0b,
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
	if !fn.directIntFast || !fn.directIntBounded {
		t.Fatal("trapping quad did not select bounded direct entry")
	}
	if _, err := fn.Invoke(0, 2, 3, 4); err == nil {
		t.Fatal("expected quad trap")
	}
	got, err := fn.Invoke(1, 2, 3, 4)
	if err != nil || len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("after trap = %v, %v", got, err)
	}
}

func TestPreparedDirectWideTrapReset(t *testing.T) {
	n := 8
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

func TestPreparedDirectWideAmd64EightArguments(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("amd64 register-bank extension")
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
	if !compiled.directPreparedBoundedAt(0) || !fn.directIntFast {
		t.Fatal("eight-argument amd64 entry did not select bounded register path")
	}
	args := make([]uint64, 8)
	args[7] = 0x1122334455667788
	if out, err := fn.Invoke(args...); err != nil || len(out) != 1 || out[0] != args[7] {
		t.Fatalf("prepared = %v, %v", out, err)
	}
	if out, err := in.Invoke("f", args...); err != nil || len(out) != 1 || out[0] != args[7] {
		t.Fatalf("instance = %v, %v", out, err)
	}
}

func TestPreparedDirectAmd64EightIntegerResults(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("amd64 register-bank extension")
	}
	compiled := MustCompile(hostToWasmI32SignatureModule(8, 8))
	defer compiled.Close()
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("eight-result amd64 register entry is not bounded")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil || !fn.directIntFast || !fn.directIsolated {
		t.Fatalf("eight-result amd64 entry = %v, %v", fn, err)
	}
	args := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	for label, invoke := range map[string]func() ([]uint64, error){
		"prepared": func() ([]uint64, error) { return fn.Invoke(args...) },
		"instance": func() ([]uint64, error) { return in.Invoke("f", args...) },
	} {
		got, err := invoke()
		if err != nil || len(got) != len(args) {
			t.Fatalf("%s = %v, %v", label, got, err)
		}
		for i := range args {
			if got[i] != args[i] {
				t.Fatalf("%s[%d] = %x, want %x", label, i, got[i], args[i])
			}
		}
	}
}

func TestAmd64EightIntegerResultsInternalCall(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("amd64 register-bank extension")
	}
	types := make([]wasm.ValType, 8)
	args := make([]uint64, 8)
	leaf := []byte{0x20, 0x00, 0x45, 0x04, 0x40, 0x00, 0x0b}
	caller := make([]byte, 0, 20)
	for i := range types {
		types[i] = wasm.I32
		args[i] = uint64(i + 1)
		if i%2 != 0 {
			types[i] = wasm.I64
			args[i] |= 0x1122334400000000
		}
		leaf = append(leaf, 0x20, byte(i))
		caller = append(caller, 0x20, byte(i))
	}
	leaf = append(leaf, 0x0b)
	caller = append(caller, 0x10, 0x00, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(leaf), wasmtest.Code(caller))),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f", args...)
	if err != nil || len(got) != len(args) {
		t.Fatalf("eight-result internal call = %v, %v", got, err)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf("result[%d] = %x, want %x", i, got[i], args[i])
		}
	}
}
