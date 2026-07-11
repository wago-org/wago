//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"math"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// regABIMixedCallModule exercises the mixed GP/FP register-ABI call staging:
//   - f(x:f64, n:i32)->f64        mixed params, one float result
//   - g calls f with computed (register-resident) f64 and i32 args
//   - dm(x:f64, a:i64, b:i64)->(i64,i64)  float param + two int results
//   - cdm calls dm with computed register-resident args
//
// The computed args force the parallel-move staging path (not the const/slot
// deferred path), so a bank mix-up or a dropped second result miscompiles.
func regABIMixedCallModule() []byte {
	f64 := []wasm.ValType{wasm.F64}
	twoI64 := []wasm.ValType{wasm.I64, wasm.I64}
	le1p0 := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f} // 1.0
	le2p0 := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40} // 2.0

	gBody := []byte{0x20, 0x00, 0x44}
	gBody = append(gBody, le1p0...)
	gBody = append(gBody, 0xa0, 0x20, 0x01, 0x41, 0x01, 0x6a, 0x10, 0x00, 0x0b) // (x+1.0); (n+1); call f; end

	cdmBody := []byte{0x20, 0x00, 0x44}
	cdmBody = append(cdmBody, le2p0...)
	cdmBody = append(cdmBody, 0xa0, // (x+2.0)
		0x20, 0x01, 0x42, 0x0a, 0x7c, // (a+10)
		0x20, 0x02, 0x42, 0x14, 0x7c, // (b+20)
		0x10, 0x02, 0x0b) // call dm; end

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I32}, f64),              // type0: (f64,i32)->f64
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I64, wasm.I64}, twoI64), // type1: (f64,i64,i64)->(i64,i64)
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), // func0 f
			wasmtest.ULEB(0), // func1 g
			wasmtest.ULEB(1), // func2 dm
			wasmtest.ULEB(1), // func3 cdm
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("g", 0, 1),
			wasmtest.ExportEntry("dm", 0, 2),
			wasmtest.ExportEntry("cdm", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0xb7, 0xa0, 0x0b}), // f: x + f64(n); end
			wasmtest.Code(gBody),
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x0b}), // dm: (a, b); end
			wasmtest.Code(cdmBody),
		)),
	)
}

func TestRegABIMixedCall(t *testing.T) {
	in, err := Instantiate(MustCompile(regABIMixedCallModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	// f(3.0, 5) = 3 + 5 = 8 (adapter path, mixed params, float result).
	if got, err := in.Invoke("f", math.Float64bits(3.0), I32(5)); err != nil {
		t.Fatalf("Invoke f: %v", err)
	} else if math.Float64frombits(got[0]) != 8.0 {
		t.Fatalf("f(3,5) = %v, want 8", math.Float64frombits(got[0]))
	}

	// g(3.0, 5) = (3+1) + (5+1) = 10 (direct mixed call, register-resident args).
	if got, err := in.Invoke("g", math.Float64bits(3.0), I32(5)); err != nil {
		t.Fatalf("Invoke g: %v", err)
	} else if math.Float64frombits(got[0]) != 10.0 {
		t.Fatalf("g(3,5) = %v, want 10", math.Float64frombits(got[0]))
	}

	// dm(1.0, 100, 200) = (100, 200) (adapter, float param + two int results).
	if got, err := in.Invoke("dm", math.Float64bits(1.0), 100, 200); err != nil {
		t.Fatalf("Invoke dm: %v", err)
	} else if want := []uint64{100, 200}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dm = %v, want %v", got, want)
	}

	// cdm(1.0, 100, 200) = dm(x+2, 110, 220) = (110, 220) (mixed call, two int results).
	if got, err := in.Invoke("cdm", math.Float64bits(1.0), 100, 200); err != nil {
		t.Fatalf("Invoke cdm: %v", err)
	} else if want := []uint64{110, 220}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cdm = %v, want %v", got, want)
	}
}
