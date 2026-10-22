package wasm

import (
	"errors"
	"testing"
)

func simdMemargModule(t *testing.T, loads int) *Module {
	t.Helper()
	body := make([]byte, 0, loads*7+1)
	for range loads {
		body = append(body,
			0x41, 0x00, // i32.const 0
			0xfd, 0x00, 0x00, 0x00, // v128.load align=0 offset=0
			0x1a, // drop
		)
	}
	body = append(body, 0x0b)
	code := append([]byte{0x01}, u32(uint32(1+len(body)))...)
	code = append(code, 0x00) // zero local declarations
	code = append(code, body...)
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secMemory, 0x01, 0x00, 0x01),
		section(secCode, code...),
	)
	m, err := DecodeModule(b)
	if err != nil {
		t.Fatalf("DecodeModule(%d SIMD loads): %v", loads, err)
	}
	return m
}

func validationAllocs(t *testing.T, m *Module) float64 {
	t.Helper()
	var validateErr error
	allocs := testing.AllocsPerRun(100, func() {
		validateErr = ValidateModule(m)
	})
	if validateErr != nil {
		t.Fatalf("ValidateModule: %v", validateErr)
	}
	return allocs
}

func TestValidateSIMDMemoryImmediatesDoNotAllocatePerOpcode(t *testing.T) {
	one := validationAllocs(t, simdMemargModule(t, 1))
	many := validationAllocs(t, simdMemargModule(t, 96))
	t.Logf("ValidateModule allocations: one SIMD load=%.0f, 96 SIMD loads=%.0f", one, many)
	if delta := many - one; delta > 2 {
		t.Fatalf("96 SIMD loads allocate %.0f times versus %.0f for one (delta %.0f), want bounded validation scratch", many, one, delta)
	}
}

func TestValidateSIMDShuffleImmediatesDoNotAllocatePerOpcode(t *testing.T) {
	moduleWithShuffles := func(count int) *Module {
		body := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
		for range count {
			body = append(body, 0xfd, 0x0c)
			body = append(body, make([]byte, 16)...)
			body = append(body, 0xfd, 0x0d)
			body = append(body, make([]byte, 16)...)
		}
		body = append(body, 0x1a, 0x0b)
		m := simdMemargModule(t, 0)
		m.Code[0].BodyBytes = body
		return m
	}
	one := validationAllocs(t, moduleWithShuffles(1))
	many := validationAllocs(t, moduleWithShuffles(96))
	t.Logf("ValidateModule allocations: one SIMD shuffle=%.0f, 96 SIMD shuffles=%.0f", one, many)
	if delta := many - one; delta > 2 {
		t.Fatalf("shuffle lane validation allocations grew by %.0f, want bounded validation scratch", delta)
	}
}

func TestValidateSIMDImmediateStrictness(t *testing.T) {
	truncatedShuffle := append([]byte{0xfd, 0x0d}, make([]byte, 15)...)
	tests := []struct {
		name   string
		body   []byte
		code   DecodeErrorCode
		offset int
	}{
		{
			name:   "truncated v128.const",
			body:   append([]byte{0xfd, 0x0c}, make([]byte, 15)...),
			code:   ErrIndexOutOfBounds,
			offset: 17,
		},
		{
			name:   "truncated i8x16.shuffle lanes",
			body:   truncatedShuffle,
			code:   ErrIndexOutOfBounds,
			offset: 17,
		},
		{
			name:   "truncated v128.load memarg",
			body:   []byte{0x41, 0x00, 0xfd, 0x00, 0x00},
			code:   ErrIndexOutOfBounds,
			offset: 5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := simdMemargModule(t, 1)
			m.Code[0].BodyBytes = tc.body
			err := ValidateModule(m)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != tc.code || de.Offset != tc.offset {
				t.Fatalf("ValidateModule error = %#v / %v, want code %v at offset %d", de, err, tc.code, tc.offset)
			}
		})
	}
}

func TestValidateShuffleLaneBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lane  byte
		valid bool
	}{
		{"last-valid", 31, true},
		{"first-invalid", 32, false},
		{"largest-byte", 255, false},
	} {
		for _, dead := range []bool{false, true} {
			name := tc.name
			if dead {
				name += "-unreachable"
			}
			t.Run(name, func(t *testing.T) {
				var body []byte
				if dead {
					body = append(body, 0x00)
				} else {
					for range 2 {
						body = append(body, 0xfd, 0x0c)
						body = append(body, make([]byte, 16)...)
					}
				}
				body = append(body, 0xfd, 0x0d)
				body = append(body, make([]byte, 15)...)
				body = append(body, tc.lane, 0x1a, 0x0b)
				expr, err := decodeExpr(newReader(body), 0)
				if err != nil {
					t.Fatalf("syntactically valid shuffle must decode: %v", err)
				}
				m := simdMemargModule(t, 0)
				m.Code[0].BodyBytes = body
				byteErr := ValidateModule(m)
				m.Code[0] = Func{Body: expr}
				treeErr := ValidateModule(m)
				for _, err := range []error{byteErr, treeErr} {
					if tc.valid {
						if err != nil {
							t.Fatal(err)
						}
						continue
					}
					var ve *ValidationError
					if !errors.As(err, &ve) || ve.Code != ErrTypeMismatch || ve.Detail != "simd lane out of range" {
						t.Fatalf("shuffle lane must fail validation: %v", err)
					}
				}
			})
		}
	}
}
