package wasm

import (
	"errors"
	"testing"
)

func TestDecodeRejectsSharedMemoryWithoutMaximum(t *testing.T) {
	paths := []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "AST", decode: func(b []byte) error { _, err := decodeModuleASTForTest(b); return err }},
		{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
		{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
	}
	for _, addr64 := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			name := "memory32/local"
			wantSection := byte(secMemory)
			if addr64 {
				name = "memory64/local"
			}
			if imported {
				name = name[:len(name)-len("local")] + "imported"
				wantSection = secImport
			}
			data := sharedMemoryModule(addr64, imported, false)
			t.Run(name, func(t *testing.T) {
				for _, path := range paths {
					t.Run(path.name, func(t *testing.T) {
						err := path.decode(data)
						var de *DecodeError
						if !errors.As(err, &de) || de.Code != ErrInvalidLimits {
							t.Fatalf("decode error=%#v / %v, want ErrInvalidLimits", de, err)
						}
						if de.SectionID != wantSection || de.SectionStart <= 0 || de.SectionEnd <= de.SectionStart {
							t.Fatalf("decode section diagnostics=%#v, want section %d span", de, wantSection)
						}
					})
				}
			})
		}
	}
}

func TestDecodePreservesValidMemoryLimitForms(t *testing.T) {
	for _, addr64 := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, tc := range []struct {
				name    string
				shared  bool
				withMax bool
			}{
				{name: "unshared-min", shared: false, withMax: false},
				{name: "unshared-max", shared: false, withMax: true},
				{name: "shared-max", shared: true, withMax: true},
			} {
				name := tc.name + "/memory32/local"
				if addr64 {
					name = tc.name + "/memory64/local"
				}
				if imported {
					name = name[:len(name)-len("local")] + "imported"
				}
				data := memoryLimitModule(addr64, imported, tc.shared, tc.withMax)
				t.Run(name, func(t *testing.T) {
					for _, path := range []struct {
						name   string
						decode func([]byte) error
					}{
						{name: "AST", decode: func(b []byte) error { _, err := decodeModuleASTForTest(b); return err }},
						{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
						{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
					} {
						t.Run(path.name, func(t *testing.T) {
							if err := path.decode(data); err != nil {
								t.Fatalf("decode valid memory limits: %v", err)
							}
						})
					}
				})
			}
		}
	}
}

func sharedMemoryModule(addr64, imported, withMax bool) []byte {
	return memoryLimitModule(addr64, imported, true, withMax)
}

func memoryLimitModule(addr64, imported, shared, withMax bool) []byte {
	flag := byte(0)
	if withMax {
		flag |= 1
	}
	if shared {
		flag |= 2
	}
	if addr64 {
		flag |= 4
	}
	memType := []byte{flag, 0x01}
	if withMax {
		memType = append(memType, 0x02)
	}
	if !imported {
		return module(section(secMemory, append([]byte{0x01}, memType...)...))
	}
	entry := []byte{0x01, 'm', 0x01, 'n', byte(ExternMem)}
	entry = append(entry, memType...)
	return module(section(secImport, append([]byte{0x01}, entry...)...))
}
