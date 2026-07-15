package wasm

import (
	"errors"
	"reflect"
	"testing"
)

func TestCompactImportsDecodeValidateAndPreserveOrder(t *testing.T) {
	mixed := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secImport,
			0x03,
			0x03, 'e', 'n', 'v', 0x00, 0x7f, 0x03,
			0x01, 'f', byte(ExternFunc), 0x00,
			0x01, 'm', byte(ExternMem), 0x01, 0x01, 0x02,
			0x01, 'g', byte(ExternGlobal), byte(NumI32), 0x00,
		),
	)
	m, err := DecodeModule(mixed)
	if err != nil {
		t.Fatalf("DecodeModule mixed compact imports: %v", err)
	}
	if !m.UsesCompactImports {
		t.Fatal("compact import marker was not retained")
	}
	got := make([]struct {
		module string
		name   string
		kind   ExternKind
	}, len(m.Imports))
	for i := range m.Imports {
		got[i].module = m.Imports[i].Module
		got[i].name = m.Imports[i].Name
		got[i].kind = m.Imports[i].Type.Kind
	}
	want := []struct {
		module string
		name   string
		kind   ExternKind
	}{
		{module: "env", name: "f", kind: ExternFunc},
		{module: "env", name: "m", kind: ExternMem},
		{module: "env", name: "g", kind: ExternGlobal},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded compact imports = %#v, want %#v", got, want)
	}

	var ve *ValidationError
	if err := ValidateModule(m); !errors.As(err, &ve) || ve.Code != ErrUnsupportedFeature || ve.Detail != "compact imports" {
		t.Fatalf("default validation error = %v, want compact-import ErrUnsupportedFeature", err)
	}
	features := ValidationFeatures{CompactImports: true}
	if err := ValidateModuleWithFeatures(m, features); err != nil {
		t.Fatalf("ValidateModuleWithFeatures mixed compact imports: %v", err)
	}
	if err := ValidateByteBackedModuleWithFeatures(mixed, features); err != nil {
		t.Fatalf("ValidateByteBackedModuleWithFeatures mixed compact imports: %v", err)
	}
}

func TestCompactImportsSameKindMemoryGroup(t *testing.T) {
	data := module(section(secImport,
		0x02,
		0x03, 'e', 'n', 'v', 0x00, 0x7e, byte(ExternMem), 0x02,
		0x01, 'a', 0x00, 0x01,
		0x01, 'b', 0x01, 0x02, 0x03,
	))
	m, err := DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule same-kind compact imports: %v", err)
	}
	if len(m.Imports) != 2 || m.Imports[0].Name != "a" || m.Imports[1].Name != "b" {
		t.Fatalf("same-kind imports = %#v", m.Imports)
	}
	if got := m.Imports[1].Type.Mem.Limits; got.Min != 2 || got.Max == nil || *got.Max != 3 {
		t.Fatalf("second compact memory limits = %#v, want 2..3", got)
	}
	features := ValidationFeatures{CompactImports: true, MultiMemory: true}
	if err := ValidateModuleWithFeatures(m, features); err != nil {
		t.Fatalf("ValidateModuleWithFeatures same-kind compact imports: %v", err)
	}
	if err := ValidateByteBackedModuleWithFeatures(data, features); err != nil {
		t.Fatalf("ValidateByteBackedModuleWithFeatures same-kind compact imports: %v", err)
	}
}

func TestCompactImportsRejectMalformedGroupsAndTypes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		code DecodeErrorCode
	}{
		{
			name: "group exceeds section count",
			data: module(section(secImport,
				0x01,
				0x01, 'm', 0x00, 0x7f, 0x02,
			)),
			code: ErrInvalidImport,
		},
		{
			name: "invalid shared kind",
			data: module(section(secImport,
				0x01,
				0x01, 'm', 0x00, 0x7e, 0x05, 0x01,
			)),
			code: ErrInvalidImport,
		},
		{
			name: "invalid per-entry kind",
			data: module(section(secImport,
				0x01,
				0x01, 'm', 0x00, 0x7f, 0x01,
				0x01, 'x', 0x05,
			)),
			code: ErrInvalidImport,
		},
		{
			name: "malformed compact count",
			data: module(section(secImport,
				0x01,
				0x01, 'm', 0x00, 0x7f, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00,
			)),
			code: ErrMalformedLEB,
		},
		{
			name: "invalid memory type",
			data: module(section(secImport,
				0x01,
				0x01, 'm', 0x00, 0x7f, 0x01,
				0x01, 'x', byte(ExternMem), 0x08,
			)),
			code: ErrInvalidLimits,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, decode := range []struct {
				name string
				fn   func([]byte) error
			}{
				{name: "AST", fn: func(data []byte) error { _, err := DecodeModule(data); return err }},
				{name: "byte-backed", fn: func(data []byte) error { _, err := DecodeModuleByteBacked(data); return err }},
			} {
				t.Run(decode.name, func(t *testing.T) {
					var de *DecodeError
					if err := decode.fn(tc.data); !errors.As(err, &de) || de.Code != tc.code {
						t.Fatalf("decode error = %v, want %v", err, tc.code)
					}
				})
			}
		})
	}
}

func TestCompactImportsRejectUnknownFunctionTypeIndex(t *testing.T) {
	data := module(section(secImport,
		0x01,
		0x01, 'm', 0x00, 0x7f, 0x01,
		0x01, 'f', byte(ExternFunc), 0x00,
	))
	features := ValidationFeatures{CompactImports: true}
	for _, validate := range []struct {
		name string
		fn   func() error
	}{
		{name: "AST", fn: func() error {
			m, err := DecodeModule(data)
			if err != nil {
				return err
			}
			return ValidateModuleWithFeatures(m, features)
		}},
		{name: "byte-backed", fn: func() error { return ValidateByteBackedModuleWithFeatures(data, features) }},
	} {
		t.Run(validate.name, func(t *testing.T) {
			var ve *ValidationError
			if err := validate.fn(); !errors.As(err, &ve) || ve.Code != ErrUnknownType {
				t.Fatalf("validation error = %v, want ErrUnknownType", err)
			}
		})
	}
}
