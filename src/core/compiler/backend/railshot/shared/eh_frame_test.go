package shared

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScanEHFrameShapeConsumesCompleteBlockTypes(t *testing.T) {
	blockTypes := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{0x40}},
		{"scalar", []byte{0x7f}},
		{"vector", []byte{0x7b}},
		{"nullable abstract reference", []byte{0x63, 0x6e}},
		{"non-null abstract reference", []byte{0x64, 0x70}},
		{"nullable indexed reference", []byte{0x63, 0xe0, 0x07}},
		{"non-null indexed reference", []byte{0x64, 0xe0, 0x07}},
		{"function type index", []byte{0xe0, 0x07}},
	}
	opcodes := []struct {
		name string
		op   byte
	}{
		{"block", 0x02},
		{"loop", 0x03},
		{"if", 0x04},
		{"try_table", 0x1f},
	}
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	for _, opcode := range opcodes {
		for _, blockType := range blockTypes {
			t.Run(opcode.name+"/"+blockType.name, func(t *testing.T) {
				body := append([]byte{opcode.op}, blockType.data...)
				if opcode.op == 0x1f {
					body = append(body, 0x00) // no catches
				}
				body = append(body, 0x01, 0x0b) // nop, end
				shape, err := ScanEHFrameShape(&classifier, body)
				if err != nil {
					t.Fatal(err)
				}
				wantTries := 0
				if opcode.op == 0x1f {
					wantTries = 1
				}
				if shape.TryRecords != wantTries || shape.RootRecords != 0 {
					t.Fatalf("shape = %+v, want %d try records and no roots", shape, wantTries)
				}
			})
		}
	}
}

func TestScanEHFrameShapeRejectsMalformedBlockTypes(t *testing.T) {
	// Truncated explicit references are tested directly at SkipBlockType.
	// In try_table, a later catch-count read can produce the same truncation
	// error, so those cases cannot prove which parser rejected the bytes.
	malformed := []struct {
		name     string
		data     []byte
		code     wasm.DecodeErrorCode
		offset   int
		complete bool
	}{
		{"missing", nil, wasm.ErrInvalidBlockType, 1, false},
		{"truncated type index", []byte{0x80}, wasm.ErrInvalidBlockType, 2, false},
		{"nullable invalid negative heap type", []byte{0x63, 0x7f}, wasm.ErrInvalidType, 3, true},
		{"non-null invalid negative heap type", []byte{0x64, 0x7f}, wasm.ErrInvalidType, 3, true},
		{"nullable exact heap type overflow", []byte{0x63, 0x62, 0x80, 0x80, 0x80, 0x80, 0x20}, wasm.ErrMalformedLEB, 8, true},
		{"non-null exact heap type overflow", []byte{0x64, 0x62, 0x80, 0x80, 0x80, 0x80, 0x20}, wasm.ErrMalformedLEB, 8, true},
	}
	classifier := wasm.NewModuleInstructionClassifier(nil, true)
	for _, opcode := range []struct {
		name string
		op   byte
	}{
		{"block", 0x02}, {"loop", 0x03}, {"if", 0x04}, {"try_table", 0x1f},
	} {
		for _, encoding := range malformed {
			t.Run(opcode.name+"/"+encoding.name, func(t *testing.T) {
				body := append([]byte{opcode.op}, encoding.data...)
				if encoding.complete {
					if opcode.op == 0x1f {
						body = append(body, 0x00) // complete empty catch vector
					}
					body = append(body, 0x01, 0x0b) // nop, end
				}
				_, err := ScanEHFrameShape(&classifier, body)
				var decodeErr *wasm.DecodeError
				if !errors.As(err, &decodeErr) || decodeErr.Code != encoding.code || decodeErr.Offset != encoding.offset {
					t.Fatalf("scan %x: got %v, want block-type decode error %v at offset %d", body, err, encoding.code, encoding.offset)
				}
			})
		}
	}
}
