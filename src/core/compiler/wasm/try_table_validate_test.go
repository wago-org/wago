package wasm

import (
	"errors"
	"testing"
)

func tryTableValidationModule(kind CatchKind, labelDepth byte, labelType byte, nullableLabelRef bool) []byte {
	// type 0: tag (i32) -> (); type 1: function () -> ().
	types := []byte{0x02, 0x60, 0x01, 0x7f, 0x00, 0x60, 0x00, 0x00}
	if labelType >= 2 {
		// Additional block signatures are appended in index order. Reference
		// catches use either (ref exn) or (ref null exn) exactly.
		for idx := byte(2); idx <= labelType; idx++ {
			switch idx {
			case 2:
				ref := []byte{0x64, 0x69}
				if nullableLabelRef {
					ref = []byte{0x69}
				}
				types = append(types, 0x60, 0x00, 0x02, 0x7f)
				types = append(types, ref...)
			case 3:
				ref := []byte{0x64, 0x69}
				if nullableLabelRef {
					ref = []byte{0x69}
				}
				types = append(types, 0x60, 0x00, 0x01)
				types = append(types, ref...)
			}
		}
		types[0] = labelType + 1
	}

	catch := []byte{byte(kind)}
	if kind == CatchTag || kind == CatchRef {
		catch = append(catch, 0x00) // tag 0
	}
	catch = append(catch, labelDepth)

	blockType := labelType
	if labelType == 0 {
		blockType = 0x40
	} else if labelType == 1 {
		blockType = 0x7f
	}
	body := []byte{0x02, blockType}
	if labelDepth != 0 {
		body = append(body, 0x02, 0x40) // inner block; catch depth 1 targets outer block
	}
	body = append(body, 0x1f, 0x40, 0x01)
	body = append(body, catch...)
	body = append(body,
		0x41, 0x01, // i32.const 1
		0x08, 0x00, // throw tag 0
		0x0b, // end try_table
	)
	if labelDepth != 0 {
		body = append(body,
			0x0b,       // end inner block
			0x41, 0x00, // normal path supplies the outer block's i32 result
		)
	}
	body = append(body, 0x0b) // end target block
	switch kind {
	case CatchTag:
		body = append(body, 0x1a) // drop i32 payload
	case CatchRef:
		body = append(body, 0x1a, 0x1a) // drop exnref, payload
	case CatchAllRef:
		body = append(body, 0x1a) // drop exnref
	}
	body = append(body, 0x0b)             // end function
	code := append([]byte{0x00}, body...) // zero local declarations
	codePayload := append([]byte{0x01}, u32(uint32(len(code)))...)
	codePayload = append(codePayload, code...)

	return module(
		section(secType, types...),
		section(secFunction, 0x01, 0x01),
		section(secTag, 0x01, 0x00, 0x00),
		section(secCode, codePayload...),
	)
}

func validateTryTableBoth(t *testing.T, data []byte, wantErr bool, want ValidationErrorCode) {
	t.Helper()
	checks := []struct {
		name string
		fn   func() error
	}{
		{name: "AST", fn: func() error {
			m, err := DecodeModule(data)
			if err != nil {
				return err
			}
			return ValidateModule(m)
		}},
		{name: "byte-backed", fn: func() error { return ValidateByteBackedModule(data) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if !wantErr {
				if err != nil {
					t.Fatalf("validation = %v", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Code != want {
				t.Fatalf("validation = %v, want %v", err, want)
			}
		})
	}
}

func TestValidateTryTableCatchPayloadsAndDepths(t *testing.T) {
	for _, tc := range []struct {
		name        string
		kind        CatchKind
		depth       byte
		labelType   byte
		nullableExn bool
	}{
		{name: "catch", kind: CatchTag, labelType: 1},
		{name: "catch depth one", kind: CatchTag, depth: 1, labelType: 1},
		{name: "catch_ref non-null", kind: CatchRef, labelType: 2},
		{name: "catch_ref nullable target", kind: CatchRef, labelType: 2, nullableExn: true},
		{name: "catch_all", kind: CatchAll},
		{name: "catch_all_ref non-null", kind: CatchAllRef, labelType: 3},
		{name: "catch_all_ref nullable target", kind: CatchAllRef, labelType: 3, nullableExn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validateTryTableBoth(t, tryTableValidationModule(tc.kind, tc.depth, tc.labelType, tc.nullableExn), false, 0)
		})
	}
}

func TestValidateTryTableCatchPayloadMismatches(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      CatchKind
		depth     byte
		labelType byte
		want      ValidationErrorCode
	}{
		{name: "catch missing payload label", kind: CatchTag, labelType: 0, want: ErrTypeMismatch},
		{name: "catch_ref missing exception label", kind: CatchRef, labelType: 1, want: ErrTypeMismatch},
		{name: "catch_all rejects payload label", kind: CatchAll, labelType: 1, want: ErrTypeMismatch},
		{name: "catch_all_ref rejects scalar label", kind: CatchAllRef, labelType: 1, want: ErrTypeMismatch},
		{name: "unknown outer depth", kind: CatchTag, depth: 3, labelType: 1, want: ErrUnknownLabel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validateTryTableBoth(t, tryTableValidationModule(tc.kind, tc.depth, tc.labelType, false), true, tc.want)
		})
	}
}
