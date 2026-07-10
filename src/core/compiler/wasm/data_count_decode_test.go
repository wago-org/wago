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
		{name: "no data instruction", data: plain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeModule(tc.data)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("DecodeModule rejected valid module: %v", err)
				}
				return
			}
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != ErrInvalidModule {
				t.Fatalf("DecodeModule error = %v, want ErrInvalidModule", err)
			}
		})
	}
}
