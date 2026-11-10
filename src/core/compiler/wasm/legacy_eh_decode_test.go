package wasm

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestLegacyExceptionHandlingDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   byte
	}{
		{"try", 0x06}, {"catch", 0x07}, {"rethrow", 0x09}, {"delegate", 0x18}, {"catch_all", 0x19},
	} {
		for _, path := range []string{"instruction", "bytecode", "module"} {
			t.Run(tc.name+"/"+path, func(t *testing.T) {
				var err error
				switch path {
				case "instruction":
					_, err = decodeInstruction(newReader([]byte{tc.op}), 0)
				case "bytecode":
					_, err = skipExprOp(newReader([]byte{tc.op}))
				case "module":
					_, err = DecodeModule(module(section(secType, 1, 0x60, 0, 0), section(secFunction, 1, 0), section(secCode, 1, 3, 0, tc.op, 0x0b)))
				}
				var de *DecodeError
				if !errors.As(err, &de) || !strings.Contains(err.Error(), "legacy exception handling is not supported") || !strings.Contains(err.Error(), "try_table") {
					t.Fatalf("%s: got %v; want targeted legacy-EH diagnostic with migration hint", tc.name, err)
				}
				if path == "module" && de.SectionID != secCode {
					t.Fatalf("lost code-section context: %+v", de)
				}
				if path != "module" && de.Offset != 0 {
					t.Fatalf("offset=%d, want 0", de.Offset)
				}
			})
		}
	}
}

func TestLegacyExceptionDiagnosticDoesNotCatchCurrentOrReservedOpcodes(t *testing.T) {
	for _, body := range [][]byte{{0x08, 0}, {0x0a}, {0x1f, 0x40, 0, 0x0b}} {
		if _, err := decodeInstruction(newReader(body), 0); err != nil {
			t.Fatalf("current opcode %x: %v", body[0], err)
		}
		if _, err := skipExprOp(newReader(body)); err != nil {
			t.Fatalf("current bytecode %x: %v", body[0], err)
		}
	}
	for _, op := range []byte{0x16, 0x17, 0xff} {
		t.Run(fmt.Sprintf("reserved_%x", op), func(t *testing.T) {
			_, err := decodeInstruction(newReader([]byte{op}), 0)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != ErrInvalidInstruction {
				t.Fatalf("reserved opcode changed classification: %v", err)
			}
		})
	}
}
