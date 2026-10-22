//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicSpecInvalidDiagnosticGate(t *testing.T) {
	cases := []struct {
		name, diagnostic string
		err              error
		valid            bool
	}{
		{"type", "type mismatch", &wasm.ValidationError{Code: wasm.ErrTypeMismatch}, true},
		{"alignment", "alignment must not be larger than natural", &wasm.ValidationError{Code: wasm.ErrInvalidAlignment}, true},
		{"local", "unknown local 2", &wasm.ValidationError{Code: wasm.ErrUnknownLocal}, true},
		{"local-unspecified", "unknown local", &wasm.ValidationError{Code: wasm.ErrUnknownLocal}, true},
		{"label", "unknown label", &wasm.ValidationError{Code: wasm.ErrUnknownLabel}, true},
		{"function", "unknown function", &wasm.ValidationError{Code: wasm.ErrUnknownFunc}, true},
		{"function-type-mismatch", "unknown function", &wasm.ValidationError{Code: wasm.ErrTypeMismatch}, false},
		{"function-unsupported", "unknown function", railshot.ErrUnsupported, false},
		{"label-type-mismatch", "unknown label", &wasm.ValidationError{Code: wasm.ErrTypeMismatch}, false},
		{"label-unsupported", "unknown label", railshot.ErrUnsupported, false},
		{"lane", "invalid lane index", &wasm.ValidationError{Code: wasm.ErrTypeMismatch, Detail: "simd lane out of range"}, true},
		{"shuffle-decode-stage", "invalid lane index", &wasm.DecodeError{Code: wasm.ErrInvalidInstruction, Offset: 54, SectionID: 10, SectionStart: 21, SectionEnd: 79}, false},
		{"unrelated-shuffle-offset", "invalid lane index", &wasm.DecodeError{Code: wasm.ErrInvalidInstruction, Offset: 38, SectionID: 10, SectionStart: 21, SectionEnd: 79}, false},
		{"unrelated-shuffle-section", "invalid lane index", &wasm.DecodeError{Code: wasm.ErrInvalidInstruction, Offset: 54, SectionID: 6, SectionStart: 21, SectionEnd: 79}, false},
		{"unrelated-shuffle-source-range", "invalid lane index", &wasm.DecodeError{Code: wasm.ErrInvalidInstruction, Offset: 54, SectionID: 10, SectionStart: 20, SectionEnd: 79}, false},
		{"wrapped", "type mismatch", fmt.Errorf("validate: %w", &wasm.ValidationError{Code: wasm.ErrTypeMismatch}), true},
		{"success", "type mismatch", nil, false},
		{"target-policy", "type mismatch", railshot.ErrUnsupported, false},
		{"unsupported-validation", "type mismatch", &wasm.ValidationError{Code: wasm.ErrUnsupportedValidationOpcode}, false},
		{"resource-limit", "type mismatch", &wasm.ValidationError{Code: wasm.ErrResourceLimitExceeded}, false},
		{"wrong-stage", "type mismatch", &wasm.DecodeError{Code: wasm.ErrInvalidInstruction}, false},
		{"unrelated-lane-type", "invalid lane index", &wasm.ValidationError{Code: wasm.ErrTypeMismatch}, false},
		{"untyped-message", "type mismatch", errors.New("type mismatch"), false},
		{"unknown-diagnostic", "new diagnostic", &wasm.ValidationError{Code: wasm.ErrTypeMismatch}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := rv64PublicSpecInvalidError(tc.diagnostic, tc.err); (err == nil) != tc.valid {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestRV64PublicSpecRegisteredGlobalOwner(t *testing.T) {
	consumer := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("provider", "g4", wasm.V128, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x0b}))),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			provider, err := rv64PublicSpecInstantiate(t, rv64PublicSWARGlobalExportsModule(), artifact, nil)
			if err != nil {
				t.Fatal(err)
			}
			var borrower *rv64PublicSpecInstance
			defer func() {
				if err := borrower.close(); err != nil {
					t.Error(err)
					return
				}
				if err := provider.close(); err != nil {
					t.Error(err)
				}
			}()
			if missing, err := rv64PublicSpecInstantiate(t, consumer, artifact, nil); err == nil || missing != nil {
				if missing != nil {
					if cleanup := missing.close(); cleanup != nil {
						t.Error(cleanup)
					}
				}
				t.Fatalf("missing registration accepted: %v", err)
			}
			borrower, err = rv64PublicSpecInstantiate(t, consumer, artifact, map[string]*rv64PublicSpecInstance{"provider": provider})
			if err != nil {
				t.Fatal(err)
			}
			got, err := borrower.instance.Invoke("get")
			if err != nil || len(got) != 2 || got[0] != 0x8899aabbccddeeff || got[1] != 0x0807060504030201 {
				t.Fatalf("linked vector: %x, %v", got, err)
			}
			if len(borrower.globals) != 1 {
				t.Fatal("global handle owner missing")
			}
		})
	}
}
