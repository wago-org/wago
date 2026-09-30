package wasm

import (
	"errors"
	"testing"
)

func TestDecodeDataCountConsistency(t *testing.T) {
	matching := module(
		section(secDataCount, 0x01),
		section(secData, 0x01, 0x01, 0x00),
	)
	noCount := module(section(secData, 0x01, 0x01, 0x00))
	zeroCount := module(section(secDataCount, 0x00))
	zeroSections := module(
		section(secFunction, 0x00),
		section(secCode, 0x00),
		section(secData, 0x00),
	)
	mismatchLow := module(
		section(secDataCount, 0x01),
		section(secData, 0x02, 0x01, 0x00, 0x01, 0x00),
	)
	mismatchHigh := module(
		section(secDataCount, 0x02),
		section(secData, 0x01, 0x01, 0x00),
	)
	countWithoutData := module(section(secDataCount, 0x01))

	cases := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{name: "matching", data: matching},
		{name: "data without count", data: noCount},
		{name: "zero count without data", data: zeroCount},
		{name: "zero function code and data sections", data: zeroSections},
		{name: "count below data length", data: mismatchLow, wantErr: true},
		{name: "count above data length", data: mismatchHigh, wantErr: true},
		{name: "nonzero count without data section", data: countWithoutData, wantErr: true},
	}
	paths := []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
		{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
	}
	for _, tc := range cases {
		for _, path := range paths {
			t.Run(tc.name+"/"+path.name, func(t *testing.T) {
				err := path.decode(tc.data)
				if !tc.wantErr {
					if err != nil {
						t.Fatalf("decode rejected valid module: %v", err)
					}
					return
				}
				var de *DecodeError
				if !errors.As(err, &de) || de.Code != ErrInvalidModule {
					t.Fatalf("decode error = %v, want ErrInvalidModule", err)
				}
			})
		}
	}
}

func TestDecodeDataInstructionsRequireDataCount(t *testing.T) {
	gcDataModule := func(body ...byte) []byte {
		code := append([]byte{0x00}, body...)
		return module(
			section(secType,
				0x02,
				0x5e, 0x78, 0x01, // type 0: (array (mut i8))
				0x60, 0x00, 0x00, // type 1: () -> ()
			),
			section(secFunction, 0x01, 0x01),
			section(secCode, append([]byte{0x01}, append(u32(uint32(len(code))), code...)...)...),
			section(secData, 0x01, 0x01, 0x01, 0x00),
		)
	}
	memoryInit := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secMemory, 0x01, 0x00, 0x01),
		section(secCode, 0x01,
			0x0c, 0x00,
			0x41, 0x00,
			0x41, 0x00,
			0x41, 0x00,
			0xfc, 0x08, 0x00, 0x00,
			0x0b,
		),
		section(secData, 0x01, 0x01, 0x00),
	)
	dataDrop := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x05, 0x00, 0xfc, 0x09, 0x00, 0x0b),
		section(secData, 0x01, 0x01, 0x00),
	)
	arrayNewData := gcDataModule(
		0x41, 0x00, // i32.const 0: source offset
		0x41, 0x01, // i32.const 1: length
		0xfb, 0x09, 0x00, 0x00, // array.new_data type 0, data 0
		0x1a, // drop
		0x0b, // end
	)
	arrayInitData := gcDataModule(
		0x41, 0x01, // i32.const 1: array length
		0xfb, 0x07, 0x00, // array.new_default type 0
		0x41, 0x00, // i32.const 0: destination offset
		0x41, 0x00, // i32.const 0: source offset
		0x41, 0x01, // i32.const 1: length
		0xfb, 0x12, 0x00, 0x00, // array.init_data type 0, data 0
		0x0b, // end
	)
	plain := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x02, 0x00, 0x0b),
		section(secData, 0x01, 0x01, 0x00),
	)

	cases := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{name: "memory.init", data: memoryInit, wantErr: true},
		{name: "data.drop", data: dataDrop, wantErr: true},
		{name: "array.new_data", data: arrayNewData, wantErr: true},
		{name: "array.init_data", data: arrayInitData, wantErr: true},
		{name: "no data instruction", data: plain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, validate := range []struct {
				name string
				fn   func() error
			}{
				{name: "DecodeModule", fn: func() error {
					_, err := DecodeModule(tc.data)
					return err
				}},
				{name: "ValidateByteBackedModule", fn: func() error { return ValidateByteBackedModule(tc.data) }},
			} {
				t.Run(validate.name, func(t *testing.T) {
					err := validate.fn()
					if !tc.wantErr {
						if err != nil {
							t.Fatalf("rejected valid module: %v", err)
						}
						return
					}
					var de *DecodeError
					if !errors.As(err, &de) || de.Code != ErrInvalidModule {
						t.Fatalf("error = %v, want ErrInvalidModule", err)
					}
				})
			}
		})
	}
}
