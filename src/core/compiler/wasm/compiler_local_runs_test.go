package wasm_test

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCompilerLocalRunsPreserveTypesAndOffsets(t *testing.T) {
	for _, test := range []struct {
		name         string
		declarations []byte
		wantRuns     int
	}{
		{"uniform", []byte{4, 1, 0x7f, 2, 0x7f, 0, 0x7f, 3, 0x7f}, 1},
		{"mixed", []byte{4, 1, 0x7f, 2, 0x7e, 0, 0x7e, 3, 0x7f}, 3},
		{"zero invalid ref", []byte{3, 1, 0x7f, 0, 0x63, 0xe3, 0, 1, 0x7f}, 3},
		{"duplicate invalid ref", []byte{2, 0, 0x63, 0xe3, 0, 0, 0x63, 0xe3, 0}, 1},
		{"maximum", []byte{2, 0xfe, 0xff, 0xff, 0xff, 0x0f, 0x7f, 1, 0x7f}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := append(bytes.Clone(test.declarations), 0x0b)
			code := append(wasmtest.ULEB(uint32(len(body))), body...)
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(10, wasmtest.Vec(code)))
			ordinary, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			compact, err := wasm.DecodeModuleForValidationWithFeatures(data, wasm.ValidationFeatures{})
			if err != nil {
				t.Fatal(err)
			}
			got, want := compact.Code[0], ordinary.Code[0]
			if len(got.Locals.Runs) != test.wantRuns {
				t.Fatalf("runs=%v", got.Locals.Runs)
			}
			if got.LocalDeclBytes != want.LocalDeclBytes || got.LocalDeclBytes != uint32(len(test.declarations)) || !bytes.Equal(got.BodyBytes, want.BodyBytes) {
				t.Fatal("changed source offsets or body")
			}
			gc, gover := wasm.LocalCount(nil, got.Locals.Runs)
			wc, wover := wasm.LocalCount(nil, want.Locals.Runs)
			if gc != wc || gover != wover {
				t.Fatal("changed local count")
			}
			for _, idx := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, ^uint32(0) - 1, ^uint32(0)} {
				gt, gok := wasm.LocalType(nil, got.Locals.Runs, idx)
				wt, wok := wasm.LocalType(nil, want.Locals.Runs, idx)
				if gt != wt || gok != wok {
					t.Fatalf("index%d types differ", idx)
				}
			}
			validate := func(m *wasm.Module) string {
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 100}); err != nil {
					return err.Error()
				}
				return ""
			}
			if ge, we := validate(compact), validate(ordinary); ge != we {
				t.Fatalf("validation changed: compact=%q ordinary=%q", ge, we)
			}
			if bytes.Contains([]byte(test.name), []byte("invalid")) && validate(compact) == "" {
				t.Fatal("discarded invalid zero-count type")
			}
		})
	}
}
