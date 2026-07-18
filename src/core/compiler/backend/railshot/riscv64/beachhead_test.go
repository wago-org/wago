package riscv64

import "testing"

func TestCompileBeachheadSupportedBodies(t *testing.T) {
	cases := []struct {
		name   string
		params int
		body   []byte
	}{
		{"const", 0, []byte{0x00, 0x41, 0x2a, 0x0b}},
		{"add", 2, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}},
		{"local", 1, []byte{0x01, 0x01, 0x7f, 0x41, 0x07, 0x21, 0x01, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}},
		{"if", 1, []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x04, 0x40, 0x41, 0x07, 0x21, 0x01, 0x05, 0x41, 0x09, 0x21, 0x01, 0x0b, 0x20, 0x01, 0x0b}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := CompileBeachhead(tc.params, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if len(code) == 0 || len(code)%4 != 0 {
				t.Fatalf("emitted %d bytes", len(code))
			}
		})
	}
}

func TestCompileBeachheadRejectsUnsupportedShape(t *testing.T) {
	cases := []struct {
		name   string
		params int
		body   []byte
	}{
		{"too-many-params", 9, []byte{0x00, 0x0b}},
		{"i64-local", 0, []byte{0x01, 0x01, 0x7e, 0x0b}},
		{"result-block", 0, []byte{0x00, 0x02, 0x7f, 0x0b, 0x0b}},
		{"unsupported-op", 0, []byte{0x00, 0x01, 0x0b}},
		{"missing-end", 0, []byte{0x00, 0x41, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CompileBeachhead(tc.params, tc.body); err == nil {
				t.Fatal("compile unexpectedly succeeded")
			}
		})
	}
}
