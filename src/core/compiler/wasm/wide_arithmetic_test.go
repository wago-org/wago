package wasm

import (
	"bytes"
	"testing"
)

func wideValidationModule(params, results []byte, body []byte) []byte {
	typ := append([]byte{1, 0x60, byte(len(params))}, params...)
	typ = append(typ, byte(len(results)))
	typ = append(typ, results...)
	code := append([]byte{1, byte(len(body) + 1), 0}, body...)
	return module(section(secType, typ...), section(secFunction, 1, 0), section(secCode, code...))
}

func TestWideArithmeticDecodeEncodeAndClassify(t *testing.T) {
	for sub := byte(19); sub <= 22; sub++ {
		data := []byte{0xfc, sub, 0x0b}
		expr, err := decodeExpr(&reader{data: data}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(expr.Instrs) != 1 || expr.Instrs[0].Kind != InstrI64Add128+InstrKind(sub-19) {
			t.Fatalf("opcode %d: %+v", sub, expr)
		}
		encoded, err := EncodeExpr(expr)
		if err != nil || !bytes.Equal(data, encoded) {
			t.Fatalf("round trip %d: %x, %v", sub, encoded, err)
		}
		r := NewReader(data[1:])
		imm, err := ClassifyInstructionImmediate(r, 0xfc)
		if err != nil || imm.Kind != expr.Instrs[0].Kind || r.Offset() != 1 {
			t.Fatalf("classification %d: %+v, %v", sub, imm, err)
		}
	}
	for _, data := range [][]byte{{0xfc}, {0xfc, 18}, {0xfc, 23}, {0xfc, 0x93}} {
		if _, err := decodeExpr(&reader{data: data}, 0); err == nil {
			t.Fatalf("accepted malformed opcode %x", data)
		}
	}
}

func TestWideArithmeticValidationSignatures(t *testing.T) {
	for sub := byte(19); sub <= 22; sub++ {
		count := 2
		if sub < 21 {
			count = 4
		}
		params := bytes.Repeat([]byte{0x7e}, count)
		results := []byte{0x7e, 0x7e}
		body := []byte{}
		for i := 0; i < count; i++ {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0xfc, sub, 0x0b)
		check := func(data []byte, valid bool) {
			t.Helper()
			m, err := DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateModule(m)
			if (err == nil) != valid {
				t.Fatalf("opcode %d validation = %v, valid %v", sub, err, valid)
			}
		}
		check(wideValidationModule(params, results, body), true)
		wrong := append([]byte(nil), params...)
		wrong[count-1] = 0x7f
		check(wideValidationModule(wrong, results, body), false)
		check(wideValidationModule(params, results, body[2:]), false)
		check(wideValidationModule(params, results[:1], body), false)
		check(wideValidationModule(params, []byte{0x7e, 0x7f}, body), false)
		check(wideValidationModule(nil, results, []byte{0, 0xfc, sub, 0x0b}), true)
	}
}
