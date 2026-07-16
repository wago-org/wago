package wasm

import (
	"errors"
	"testing"
)

func branchHintCustom(payload []byte) []byte {
	name := []byte(branchHintSectionName)
	data := append(u32(uint32(len(name))), name...)
	return section(secCustom, append(data, payload...)...)
}

func branchHintPayload(funcIndex, offset uint32, direction byte) []byte {
	p := append([]byte{0x01}, u32(funcIndex)...)
	p = append(p, 0x01)
	p = append(p, u32(offset)...)
	p = append(p, 0x01, direction)
	return p
}

func branchHintModule(custom []byte) []byte {
	// The if begins at function-body offset 3: one local-declaration byte and
	// local.get's two-byte encoding precede it.
	code := []byte{0x01, 0x0c, 0x00, 0x20, 0x00, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b}
	return module(
		section(secType, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		custom,
		section(secCode, code...),
	)
}

func TestDecodeBranchHintSection(t *testing.T) {
	dm, err := DecodeModuleByteBacked(branchHintModule(branchHintCustom(branchHintPayload(0, 3, 1))))
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked: %v", err)
	}
	if got := dm.Module.Code[0].LocalDeclBytes; got != 1 {
		t.Fatalf("LocalDeclBytes = %d, want 1", got)
	}
	if got := dm.Module.BranchHints; len(got) != 1 || got[0].FuncIndex != 0 || len(got[0].Hints) != 1 || got[0].Hints[0] != (BranchHint{Offset: 3, Likely: true}) {
		t.Fatalf("BranchHints = %#v, want function 0 / offset 3 / likely", got)
	}
	if err := ValidateDecodedByteBackedModule(dm); err != nil {
		t.Fatalf("ValidateDecodedByteBackedModule: %v", err)
	}
}

func TestDecodeBranchHintSectionBrIf(t *testing.T) {
	// The br_if begins at function-body offset 5: the local declarations, a
	// block header, and local.get's immediate are all included in the offset.
	mod := module(
		section(secType, 0x01, 0x60, 0x01, 0x7f, 0x00),
		section(secFunction, 0x01, 0x00),
		branchHintCustom(branchHintPayload(0, 5, 0)),
		section(secCode, 0x01, 0x09, 0x00, 0x02, 0x40, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x0b),
	)
	dm, err := DecodeModuleByteBacked(mod)
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked: %v", err)
	}
	if got := dm.Module.BranchHints[0].Hints[0]; got != (BranchHint{Offset: 5}) {
		t.Fatalf("BranchHints[0] = %#v, want unlikely br_if at 5", got)
	}
	if err := ValidateDecodedByteBackedModule(dm); err != nil {
		t.Fatalf("ValidateDecodedByteBackedModule: %v", err)
	}
}

func TestDecodeBranchHintSectionRejectsMalformedMetadata(t *testing.T) {
	badSize := branchHintPayload(0, 3, 1)
	badSize[len(badSize)-2] = 2
	badDirection := branchHintPayload(0, 3, 2)
	duplicate := append([]byte{0x02}, u32(0)...)
	duplicate = append(duplicate, 0x00)
	duplicate = append(duplicate, u32(0)...)
	duplicate = append(duplicate, 0x00)
	badTarget := branchHintPayload(0, 2, 1) // local.get immediate, not an opcode

	cases := []struct {
		name string
		mod  []byte
	}{
		{"payload size", branchHintModule(branchHintCustom(badSize))},
		{"direction", branchHintModule(branchHintCustom(badDirection))},
		{"duplicate function", branchHintModule(branchHintCustom(duplicate))},
		{"non-branch target", branchHintModule(branchHintCustom(badTarget))},
		{"after code", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secCode, 0x01, 0x02, 0x00, 0x0b),
			branchHintCustom(branchHintPayload(0, 0, 1)),
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeModuleByteBacked(tc.mod)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != ErrInvalidSection {
				t.Fatalf("DecodeModuleByteBacked error = %v, want invalid section", err)
			}
		})
	}
}

func TestValidateBranchHintsReportsMalformedOffsetsAsInvalidSection(t *testing.T) {
	cases := []struct {
		name   string
		body   []byte
		offset uint32
	}{
		{"past body", []byte{0x0b}, 2},
		{"truncated skipped immediate", []byte{0x41}, 2},
		{"truncated target immediate", []byte{0x0d}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Module{
				Code: []Func{{BodyBytes: tc.body}},
				BranchHints: []FuncBranchHints{{
					FuncIndex: 0,
					Hints:     []BranchHint{{Offset: tc.offset}},
				}},
			}
			err := validateBranchHints(&m)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != ErrInvalidSection {
				t.Fatalf("validateBranchHints error = %v, want invalid section", err)
			}
		})
	}
}

func BenchmarkDecodeModuleByteBackedBranchHint(b *testing.B) {
	withoutHint := branchHintModule(section(secCustom, 0x01, 'x'))
	withHint := branchHintModule(branchHintCustom(branchHintPayload(0, 3, 1)))
	for _, tc := range []struct {
		name string
		mod  []byte
	}{
		{"none", withoutHint},
		{"one_if", withHint},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := DecodeModuleByteBacked(tc.mod); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
