//go:build unix

package wasm

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type streamChunkReader struct {
	b []byte
	n int
}

func (r *streamChunkReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := r.n
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.b) {
		n = len(r.b)
	}
	copy(p, r.b[:n])
	r.b = r.b[n:]
	return n, nil
}

func TestDecodeModuleForCompileStreamMatchesByteBacked(t *testing.T) {
	// type () -> (); function; memory; code(end); active data. The unknown
	// custom payload is intentionally large enough to prove it is only drained.
	b := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		0, 19, 0x08, 'p', 'r', 'o', 'd', 'u', 'c', 'e', 'r', 1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		5, 3, 1, 0, 1,
		10, 4, 1, 2, 0, 0x0b,
		11, 7, 1, 0, 0x41, 0, 0x0b, 1, 0xaa,
	}
	want, err := DecodeModuleForCompile(b)
	if err != nil {
		t.Fatalf("byte-backed decode: %v", err)
	}
	for n := 1; n <= len(b); n++ {
		got, err := DecodeModuleForCompileStream(&streamChunkReader{b: append([]byte(nil), b...), n: n})
		if err != nil {
			t.Fatalf("stream decode: %v", err)
		}
		if len(got.Module.Code) != len(want.Code) || len(got.Module.Data) != len(want.Data) || len(got.Module.Customs) != 0 {
			got.Close()
			t.Fatalf("stream product differs: code=%d data=%d customs=%d", len(got.Module.Code), len(got.Module.Data), len(got.Module.Customs))
		}
		if !bytes.Equal(got.Module.Code[0].BodyBytes, want.Code[0].BodyBytes) || !bytes.Equal(got.Module.Data[0].Init, want.Data[0].Init) {
			got.Close()
			t.Fatal("stream body/data differs")
		}
		got.Close()
	}
}

func TestDecodeModuleForCompileStreamRejectsNoProgress(t *testing.T) {
	_, err := DecodeModuleForCompileStream(stuckCompileStreamReader{})
	if err == nil || errors.As(err, new(*DecodeError)) {
		t.Fatalf("stream no-progress error = %v, want reader progress failure", err)
	}
}

func TestDecodeModuleForCompileStreamErrorPhaseDifferential(t *testing.T) {
	for kind := uint8(0); kind < 7; kind++ {
		for mutation := uint8(0); mutation < 6; mutation++ {
			data := mutateDifferentialModule(generatedDifferentialModule(kind, 3), mutation, 17)
			_, want := DecodeModuleForCompile(data)
			got, err := DecodeModuleForCompileStream(&streamChunkReader{b: append([]byte(nil), data...), n: 3})
			if got != nil {
				got.Close()
			}
			if (want == nil) != (err == nil) {
				t.Fatalf("kind=%d mutation=%d byte decode=%v stream decode=%v", kind, mutation, want, err)
			}
			if want != nil && errorPhase(want) != errorPhase(err) {
				t.Fatalf("kind=%d mutation=%d byte decode=%v (%s) stream decode=%v (%s)", kind, mutation, want, errorPhase(want), err, errorPhase(err))
			}
		}
	}
}

type stuckCompileStreamReader struct{}

func (stuckCompileStreamReader) Read([]byte) (int, error) { return 0, nil }
