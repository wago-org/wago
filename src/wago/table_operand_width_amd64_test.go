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
