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

func TestValidateSIMDImmediateStrictness(t *testing.T) {
	invalidShuffle := append([]byte{0xfd, 0x0d}, make([]byte, 15)...)
	invalidShuffle = append(invalidShuffle, 32, 0x0b)
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
			name:   "invalid i8x16.shuffle lane",
			body:   invalidShuffle,
			code:   ErrInvalidInstruction,
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
