//go:build unix

package wago

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

type chunkReader struct {
	b []byte
	n int
}

type intermittentEmptyReader struct {
	r     io.Reader
	empty bool
}

func (r *intermittentEmptyReader) Read(p []byte) (int, error) {
	if r.empty {
		r.empty = false
		return 0, nil
	}
	r.empty = true
	return r.r.Read(p)
}

func readerFuncImportEntry(module, name string, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x00) // ExternFunc
	return append(out, wasmtest.ULEB(typeIdx)...)
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := r.n
	if n > len(r.b) {
		n = len(r.b)
	}
	copy(p, r.b[:n])
	r.b = r.b[n:]
	return n, nil
}

func TestCompileReaderMatchesByteCompile(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	want, err := Compile(mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for chunk := 1; chunk <= len(mod); chunk++ {
		got, err := CompileReader(&chunkReader{b: append([]byte(nil), mod...), n: chunk})
		if err != nil {
			t.Fatalf("CompileReader chunk %d: %v", chunk, err)
		}
		if !bytes.Equal(got.Code, want.Code) || len(got.Entry) != len(want.Entry) {
			t.Fatalf("streamed product chunk %d differs: got code=%x entries=%v, want code=%x entries=%v", chunk, got.Code, got.Entry, want.Code, want.Entry)
		}
	}
}

func TestCompileReaderAtMatchesReaderAndHonorsLimit(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	got, err := CompileReaderAt(bytes.NewReader(mod), int64(len(mod)))
	if err != nil {
		t.Fatalf("CompileReaderAt: %v", err)
	}
	defer got.Close()
	want, err := CompileReader(bytes.NewReader(mod))
	if err != nil {
		t.Fatalf("CompileReader: %v", err)
	}
	defer want.Close()
	if !bytes.Equal(got.Code, want.Code) || !reflect.DeepEqual(got.Entry, want.Entry) {
		t.Fatalf("reader-at product differs: code %x/%x entries %v/%v", got.Code, want.Code, got.Entry, want.Entry)
	}
	_, err = NewRuntimeConfig().WithCompileInputLimit(int64(len(mod)-1)).CompileReaderAt(bytes.NewReader(mod), int64(len(mod)))
	var limitErr *ResourceLimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != "compile input" {
		t.Fatalf("CompileReaderAt limit error = %v", err)
	}
}

func TestCompileReaderToleratesIntermittentEmptyReads(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	if _, err := CompileReader(&intermittentEmptyReader{r: bytes.NewReader(mod), empty: true}); err != nil {
		t.Fatalf("CompileReader: %v", err)
	}
}

func TestCompileReaderInputLimit(t *testing.T) {
	_, err := NewRuntimeConfig().WithCompileInputLimit(7).CompileReader(bytes.NewReader([]byte("12345678")))
	var limitErr *ResourceLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("CompileReader error = %v, want ResourceLimitError", err)
	}
	if limitErr.Resource != "compile input" || limitErr.Limit != 7 || limitErr.Used != 8 {
		t.Fatalf("limit error = %+v", limitErr)
	}
}

func TestCompileCopiesActiveData(t *testing.T) {
	data := []byte{0xaa, 0xbb}
	mod := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x41, 0x00, 0x0b, byte(len(data))}, data...))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for i := range mod {
		mod[i] = 0
	}
	if got, want := c.Data[0].Bytes, data; !bytes.Equal(got, want) {
		t.Fatalf("active data aliases source: got %x, want %x", got, want)
	}
}

func TestCompileLimitsRejectEachBoundedProductPart(t *testing.T) {
	funcMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	data := []byte{0xaa, 0xbb}
	dataMod := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x41, 0x00, 0x0b, byte(len(data))}, data...))),
	)
	base := NewRuntimeConfig().CompileLimits()
	for _, tc := range []struct {
		name      string
		mod       []byte
		configure func(*CompileLimits)
		resource  string
	}{
		{"input", funcMod, func(l *CompileLimits) { l.InputBytes = 1 }, "compile input"},
		{"body", funcMod, func(l *CompileLimits) { l.BodyBytes = 0 }, "function body"},
		{"code", funcMod, func(l *CompileLimits) { l.NativeCodeBytes = 0 }, "native code"},
		{"data", dataMod, func(l *CompileLimits) { l.RetainedDataBytes = 1 }, "retained data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := base
			tc.configure(&limits)
			_, err := Compile(NewRuntimeConfig().WithCompileLimits(limits), tc.mod)
			var limitErr *ResourceLimitError
			if !errors.As(err, &limitErr) {
				t.Fatalf("Compile error = %v, want ResourceLimitError", err)
			}
			if limitErr.Resource != tc.resource {
				t.Fatalf("ResourceLimitError = %+v, want resource %q", limitErr, tc.resource)
			}
		})
	}
}

func FuzzCompileReaderChunking(f *testing.F) {
	valid := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	// A malformed structured name section is deliberately included: reader
	// chunking must preserve the strict custom-section behavior of Compile.
	badName := wasmtest.Module(wasmtest.Section(0, []byte{0x04, 'n', 'a', 'm', 'e', 0x01, 0x02, 0x00, 0xff}))
	f.Add(valid, uint8(1))
	f.Add(valid, uint8(7))
	f.Add(badName, uint8(2))
	f.Fuzz(func(t *testing.T, wasmBytes []byte, chunk uint8) {
		if len(wasmBytes) > 1<<20 {
			t.Skip()
		}
		n := int(chunk%31) + 1
		_, directErr := Compile(wasmBytes)
		_, readerErr := CompileReader(&chunkReader{b: append([]byte(nil), wasmBytes...), n: n})
		if (directErr == nil) != (readerErr == nil) {
			t.Fatalf("chunk %d: Compile=%v CompileReader=%v", n, directErr, readerErr)
		}
	})
}

func BenchmarkCompileReaderDrainsLargeCustomSection(b *testing.B) {
	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i)
	}
	mod := wasmtest.Module(wasmtest.Custom("producer", payload))
	b.SetBytes(int64(len(mod)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := CompileReader(bytes.NewReader(mod))
		if err != nil {
			b.Fatal(err)
		}
		if err := c.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCompileReaderFunctionImportUsesCompactLinkArtifact(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Custom("producer", make([]byte, 1<<20)),
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(readerFuncImportEntry("env", "f", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0xaa})),
	)
	c, err := CompileReader(bytes.NewReader(mod))
	if err != nil {
		t.Fatalf("CompileReader: %v", err)
	}
	if c.wasmBytes != nil {
		t.Fatal("CompileReader retained raw wasm for function import")
	}
	if c.hostLink == nil || c.hostLink.module == nil {
		t.Fatal("CompileReader did not retain link artifact")
	}
	store := c.hostLink.bodyStore
	if store == nil || !store.mapped {
		t.Fatalf("link artifact body replay store = %#v, want Unix file mapping", store)
	}
	artifact := c.hostLink.module
	if len(artifact.Customs) != 0 || artifact.NameSec != nil {
		t.Fatalf("link artifact retained custom data: customs=%d name=%#v", len(artifact.Customs), artifact.NameSec)
	}
	if len(artifact.Data) != 1 || artifact.Data[0].Init != nil {
		t.Fatalf("link artifact retained data payload: %#v", artifact.Data)
	}
	if len(artifact.Code) != 1 || len(artifact.Code[0].BodyBytes) == 0 {
		t.Fatalf("link artifact lost local body: %#v", artifact.Code)
	}
	footprint := c.Footprint()
	if footprint.LinkReplayBytes != len(artifact.Code[0].BodyBytes) || !footprint.LinkReplayMapped {
		t.Fatalf("footprint replay = %+v, want %d mapped bytes", footprint, len(artifact.Code[0].BodyBytes))
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Compiled.Close: %v", err)
	}
	if store.data != nil {
		t.Fatal("Compiled.Close retained link body mapping")
	}
}

func TestCompactNativeCodeDropsDisproportionateBacking(t *testing.T) {
	code := make([]byte, 8, 1024)
	got := compactNativeCode(code)
	if len(got) != len(code) || cap(got) != len(code) {
		t.Fatalf("compactNativeCode len/cap = %d/%d, want %d/%d", len(got), cap(got), len(code), len(code))
	}
}

func TestSealCodeDropsHeapCodeAndMaterializesForCodec(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()
	codeLen := len(c.Code)
	if err := c.SealCode(); err != nil {
		t.Fatalf("SealCode: %v", err)
	}
	if c.Code != nil {
		t.Fatal("SealCode retained mutable heap Code")
	}
	if f := c.Footprint(); f.NativeCodeBytes != codeLen || f.ExecutableImageBytes != codeLen {
		t.Fatalf("sealed footprint = %+v, want %d-byte image", f, codeLen)
	}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate sealed code: %v", err)
	}
	if _, err := in.Invoke("f"); err != nil {
		in.Close()
		t.Fatalf("Invoke sealed code: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("Instance.Close: %v", err)
	}
	if _, err := c.MarshalBinary(); err != nil {
		t.Fatalf("MarshalBinary sealed code: %v", err)
	}
	if len(c.Code) != codeLen {
		t.Fatalf("MarshalBinary materialized %d code bytes, want %d", len(c.Code), codeLen)
	}
}

func TestCompileWithSealedCodeUsesExecutableImage(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithSealedCode(true), mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()
	if c.Code != nil {
		t.Fatal("WithSealedCode retained mutable heap Code")
	}
	if f := c.Footprint(); f.NativeCodeBytes == 0 || f.ExecutableImageBytes != f.NativeCodeBytes {
		t.Fatalf("sealed compile footprint = %+v", f)
	}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate sealed code: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("f"); err != nil {
		t.Fatalf("Invoke sealed code: %v", err)
	}
}
