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
