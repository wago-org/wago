package wasm

import (
	"encoding/hex"
	"testing"
)

func TestIfWithoutElseReferenceWidening(t *testing.T) {
	data, err := hex.DecodeString("0061736d01000000010e0360016470017060000060000170030302010207050101660001090501030001000a0e0202000b0900d200410004000b0b0015046e616d650104010001660408010005626c6f636b")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("bytebacked", func(t *testing.T) {
		if err := ValidateByteBackedModule(data); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ast", func(t *testing.T) {
		m, err := decodeModuleASTForTest(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateModule(m); err != nil {
			t.Fatal(err)
		}
	})
}

func TestIfWithoutElseRetainsDeclaredResultType(t *testing.T) {
	data := module(
		section(secType, 3, 0x60, 1, 0x64, 0x70, 1, 0x70, 0x60, 0, 0, 0x60, 0, 1, 0x64, 0x70),
		section(secFunction, 2, 1, 2),
		section(secElement, 1, 3, 0, 1, 0),
		section(secCode, 2, 2, 0, 0x0b, 9, 0, 0xd2, 0, 0x41, 0, 0x04, 0, 0x0b, 0x0b),
	)
	t.Run("bytebacked", func(t *testing.T) {
		if err := ValidateByteBackedModule(data); err == nil {
			t.Fatal("nullable if result accepted as non-null function result")
		}
	})
	t.Run("ast", func(t *testing.T) {
		m, err := decodeModuleASTForTest(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateModule(m); err == nil {
			t.Fatal("nullable if result accepted as non-null function result")
		}
	})
}
