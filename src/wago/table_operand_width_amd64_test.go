//go:build amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func dirtyTableGrowModule() []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("index")...), 0x00, 0x00)
	// funcref table 2..4.
	table := wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04}))
	// ref.null func; call host index; table.grow 0; end.
	body := []byte{0x00, 0xd0, 0x70, 0x10, 0x00, 0xfc, 0x0f, 0x00, 0x0b}
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		table,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
}

func dirtyTableOperationsModule() []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("index")...), 0x00, 0x00)
	table := wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04}))
	// Active element at slot 1 containing local target function index 1.
	elem := wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x01, 0x0b, 0x01, 0x01}))
	bodies := [][]byte{
		{0x41, 0x2a, 0x0b},                   // target => 42
		{0x10, 0x00, 0x25, 0x00, 0xd1, 0x0b}, // get => ref.is_null
		{0x10, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x41, 0x01, 0x25, 0x00, 0xd1, 0x0b},                   // set then read slot 1
		{0x10, 0x00, 0xd0, 0x70, 0x41, 0x01, 0xfc, 0x11, 0x00, 0x41, 0x01, 0x25, 0x00, 0xd1, 0x0b}, // fill one then read
		{0x10, 0x00, 0x11, 0x00, 0x00, 0x0b},                                                       // call_indirect
	}
	codes := make([][]byte, len(bodies))
	for i := range bodies {
		codes[i] = wasmtest.Code(bodies[i])
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		table,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", 0, 2),
			wasmtest.ExportEntry("set", 0, 3),
			wasmtest.ExportEntry("fill", 0, 4),
			wasmtest.ExportEntry("indirect", 0, 5),
		)),
		elem,
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func TestTable32OperationsCanonicalizeDirtySynchronousHostResult(t *testing.T) {
	compiled := MustCompile(dirtyTableOperationsModule())
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		want int32
	}{
		{name: "get", want: 0},
		{name: "set", want: 1},
		{name: "fill", want: 1},
		{name: "indirect", want: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.index": HostFunc(func(_ HostModule, _, results []uint64) {
				results[0] = 0xdead_beef_0000_0001
			})}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke(tc.name)
			if err != nil || AsI32(got[0]) != tc.want {
				t.Fatalf("%s = %v, %v; want %d", tc.name, got, err, tc.want)
			}
		})
	}

	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.index": HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = 0xdead_beef_0000_0005
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("get"); err == nil {
		t.Fatal("dirty out-of-range table.get did not trap")
	}
}

func dirtyReturnCallIndirectModule() []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("index")...), 0x00, 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("tail", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x01, 0x0b, 0x01, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x00, 0x13, 0x00, 0x00, 0x0b}),
		)),
	)
}

func TestReturnCallIndirectCanonicalizesDirtySynchronousHostResult(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), dirtyReturnCallIndirectModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.index": HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = 0xdead_beef_0000_0001
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("tail")
	if err != nil || AsI32(got[0]) != 42 {
		t.Fatalf("tail() = %v, %v; want 42", got, err)
	}
}

func TestTable32GrowCanonicalizesDirtySynchronousHostResult(t *testing.T) {
	c := MustCompile(dirtyTableGrowModule())
	for _, tc := range []struct {
		name  string
		dirty uint64
		want  int32
	}{
		{name: "low-bits-one", dirty: 0xdead_beef_0000_0001, want: 2},
		{name: "low-bits-out-of-capacity", dirty: 0xdead_beef_ffff_ffff, want: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.index": HostFunc(func(_ HostModule, _, results []uint64) {
				results[0] = tc.dirty
			})}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("grow")
			if err != nil {
				t.Fatal(err)
			}
			if value := AsI32(got[0]); value != tc.want {
				t.Fatalf("table.grow result = %d, want %d", value, tc.want)
			}
		})
	}
}
