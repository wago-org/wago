package wasm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// All real, valid modules in the repo must decode AND validate.
func TestValidateRealModules(t *testing.T) {
	files, _ := filepath.Glob("../../../../warp/wasm_examples/*.wasm")
	extra, _ := filepath.Glob("../../../../warp/scripts/*.wasm")
	files = append(files, extra...)
	if len(files) == 0 {
		t.Skip("no wasm_examples found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		m, err := Decode(data)
		if err != nil {
			t.Errorf("%s: decode: %v", name, err)
			continue
		}
		if err := Validate(m); err != nil {
			t.Errorf("%s: validate: %v", name, err)
		}
	}
}

func TestValidateTinyValid(t *testing.T) {
	// (func (param i32) (result i32) local.get 0) — valid.
	mod := []byte{
		0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7F, 0x01, 0x7F,
		0x03, 0x02, 0x01, 0x00,
		0x0A, 0x06, 0x01, 0x04, 0x00, 0x20, 0x00, 0x0B,
	}
	m, err := Decode(mod)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(m); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateInvalid(t *testing.T) {
	cases := []struct {
		name string
		mod  []byte
		want ErrCode
	}{
		{
			// (func (result i32)) with empty body — missing result on stack.
			"missing result",
			[]byte{
				0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
				0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7F,
				0x03, 0x02, 0x01, 0x00,
				0x0A, 0x04, 0x01, 0x02, 0x00, 0x0B,
			},
			ErrTypeMismatch,
		},
		{
			// (func (result i32) i32.add) — operand stack underflow.
			"stack underflow",
			[]byte{
				0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
				0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7F,
				0x03, 0x02, 0x01, 0x00,
				0x0A, 0x05, 0x01, 0x03, 0x00, 0x6A, 0x0B,
			},
			ErrTypeMismatch,
		},
		{
			// (func local.get 5) with no locals — unknown local.
			"unknown local",
			[]byte{
				0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
				0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
				0x03, 0x02, 0x01, 0x00,
				0x0A, 0x06, 0x01, 0x04, 0x00, 0x20, 0x05, 0x0B,
			},
			ErrUnknownLocal,
		},
		{
			// (func br 10) — branch to nonexistent label.
			"bad branch label",
			[]byte{
				0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00,
				0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
				0x03, 0x02, 0x01, 0x00,
				0x0A, 0x06, 0x01, 0x04, 0x00, 0x0C, 0x0A, 0x0B,
			},
			ErrUnknownLabel,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Decode(c.mod)
			if err != nil {
				t.Fatalf("decode (should succeed): %v", err)
			}
			err = Validate(m)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ValidationError, got %v", err)
			}
			if ve.Code != c.want {
				t.Fatalf("got %v, want %v", ve.Code, c.want)
			}
		})
	}
}
