//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorForwardModule(t *testing.T, computed bool) *wasm.Module {
	t.Helper()
	base := []byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00}
	child := []byte{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00}
	if computed {
		body = append(body, 0x41, 0x01, 0x6a) // local.get 0 + 1 is not a direct local read.
	}
	body = append(body,
		0x41, 0x07,
		0xfb, 0x00, 0x01, // struct.new child
		0xfb, 0x16, 0x00, // ref.cast base
		0xfb, 0x02, 0x00, 0x00, // struct.get base field 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(base, child, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCConstructorGetForwardSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name     string
		computed bool
		want     int
	}{
		{name: "local", want: 1},
		{name: "computed", computed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(gcConstructorForwardModule(t, tc.computed), CompileOptions{
				GCStructHelpers: true,
				Stats:           &stats,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-constructor-get-forward"]; got != tc.want {
				t.Fatalf("constructor forwarding = %d, want %d", got, tc.want)
			}
		})
	}
}
