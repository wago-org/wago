//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestDirectIntegerAdapterProof(t *testing.T) {
	for params := 0; params <= 4; params++ {
		for results := 0; results <= 1; results++ {
			adapter := directIntegerAdapter(-256, params, results)
			code := append(make([]byte, 256), adapter...)
			if !NativeDirectIntegerEntry(code, 256, 0, params, results) {
				t.Fatalf("valid adapter %d/%d rejected", params, results)
			}
			if NativeDirectIntegerEntry(code, 256, 16, params, results) {
				t.Fatal("foreign frame accepted")
			}
			if NativeDirectIntegerEntry(code, 256, 0, (params+1)%5, results) {
				t.Fatal("argument shape change accepted")
			}
			code[256] ^= 4
			if NativeDirectIntegerEntry(code, 256, 0, params, results) {
				t.Fatal("modified adapter accepted")
			}
		}
	}
}
func TestBoundedDirectIntegerSourceProof(t *testing.T) {
	for _, tc := range []struct {
		body []byte
		want bool
	}{
		{[]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}, true},
		{[]byte{0x03, 0x40, 0x0b, 0x0b}, false},
		{[]byte{0x10, 0, 0x0b}, false},
		{[]byte{0x23, 0, 0x0b}, false},
		{[]byte{0x20, 0, 0x28, 2, 0, 0x0b}, false},
		{[]byte{0xd0, 0x70, 0x1a, 0x0b}, false},
	} {
		if got := boundedDirectInteger(&wasm.Func{BodyBytes: tc.body}, []byte{0x13, 0, 0, 0}, 32); got != tc.want {
			t.Errorf("body %x: got %v want %v", tc.body, got, tc.want)
		}
	}
}

func TestNativeTableHostControlDemand(t *testing.T) {
	if NativeTableHostControlRequired([]byte{0x13, 0, 0, 0}) {
		t.Fatal("pure body demands host control")
	}
	if !NativeTableHostControlRequired([]byte{0}) {
		t.Fatal("malformed span lost conservative control")
	}
	for _, invocation := range []bool{false, true} {
		image, err := nativeTableThunkImageAtInvocation(7, 1, 1, 0, 1, invocation)
		if err != nil {
			t.Fatal(err)
		}
		if !NativeTableHostControlRequired(image.code) {
			t.Fatal("actual table bridge lost host control")
		}
	}
}

func BenchmarkDirectIntegerAdapterEmission(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		code := directIntegerAdapter(-1024, 4, 1)
		if len(code) == 0 {
			b.Fatal("adapter")
		}
		b.ReportMetric(float64(len(code)), "code-bytes")
	}
}
func BenchmarkDirectIntegerAdapterRecognition(b *testing.B) {
	code := append(make([]byte, 1024), directIntegerAdapter(-1024, 4, 1)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !NativeDirectIntegerEntry(code, 1024, 0, 4, 1) {
			b.Fatal("proof")
		}
	}
}
