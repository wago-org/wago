//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/tests/support/regressiontest"
)

type pdfiumOracle struct {
	WasmSHA256  string `json:"wasm_sha256"`
	InputSHA256 string `json:"input_sha256"`
	Pages       []struct {
		Width        float64 `json:"width"`
		Height       float64 `json:"height"`
		Text         string  `json:"text"`
		BitmapSHA256 string  `json:"bitmap_sha256"`
	} `json:"pages"`
}

// TestPDFiumCorpus uses the unmodified application from issue #1013. Besides
// compiling its large functions, it parses a compressed two-page invoice,
// extracts every line and rasterizes both pages against an independent V8 oracle.
func TestPDFiumCorpus(t *testing.T) {
	if !wago.SupportedFeatures().IsEnabled(wago.CoreFeatureExceptionHandling) {
		t.Skip("PDFium fixture requires exception-handling support")
	}
	if regressiontest.RunIsolated(t, 60*time.Second) {
		return
	}
	root := "../../tests/corpus/regressions/pdfium"
	oracleData, err := os.ReadFile(filepath.Join(root, "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var oracle pdfiumOracle
	if err := json.Unmarshal(oracleData, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Pages) != 2 {
		t.Fatalf("oracle has %d pages, want 2", len(oracle.Pages))
	}
	read := func(name, digest string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != digest {
			t.Fatalf("%s SHA-256 = %s, want %s", name, got, digest)
		}
		return data
	}
	wasm := read("pdfium-8111.wasm", oracle.WasmSHA256)
	pdf := read("invoice.pdf", oracle.InputSHA256)
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			config := wago.NewRuntimeConfig().WithCompileWorkers(workers).WithBoundsChecks(wago.BoundsChecksExplicit)
			rt := wago.NewRuntime(wago.WithRuntimeConfig(config))
			defer rt.Close()
			module, err := rt.Compile(wasm)
			if err != nil {
				t.Fatal(err)
			}
			defer module.Compiled().Close()
			runPDFiumInvoice(t, rt, module, pdf, oracle)

		})
	}
}

func runPDFiumInvoice(t *testing.T, rt *wago.Runtime, module *wago.Module, pdf []byte, oracle pdfiumOracle) {
	t.Helper()
	imports := wago.NewImports()
	for _, spec := range module.Imports() {
		if spec.Kind != wago.ImportFunc || (spec.Module != "env" && spec.Module != "wasi_snapshot_preview1") {
			t.Fatalf("unexpected PDFium import %+v", spec)
		}
		namespace, name := spec.Module, spec.Name
		imports.HostFunc(namespace, name, func(caller wago.Caller, call wago.HostCall) {
			writeZero := func(ptr uint32) {
				memory := caller.Memory()
				if uint64(ptr)+4 > uint64(len(memory)) {
					panic("PDFium host output out of bounds")
				}
				binary.LittleEndian.PutUint32(memory[ptr:], 0)
			}
			switch {
			case namespace == "env" && name == "emscripten_notify_memory_growth" && call.I32(0) == 0:
			case namespace == "wasi_snapshot_preview1" && name == "fd_prestat_get" && call.I32(0) == 3:
				call.SetI32(0, 8) // EBADF: the in-memory workload has no preopened directories.
			case namespace == "wasi_snapshot_preview1" && name == "environ_sizes_get":
				writeZero(uint32(call.I32(0)))
				writeZero(uint32(call.I32(1)))
				call.SetI32(0, 0)
			case namespace == "wasi_snapshot_preview1" && name == "environ_get":
				writeZero(uint32(call.I32(0)))
				call.SetI32(0, 0)
			default:
				panic(fmt.Errorf("unexpected PDFium host call %s.%s", namespace, name))
			}
		}).Params(spec.Params...).Results(spec.Results...)
	}
	// The enclosing subprocess supplies the hard execution deadline.
	ctx := context.Background()
	in, err := rt.Instantiate(ctx, module, wago.WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	invoke := func(name string, args ...uint64) uint64 {
		t.Helper()
		results, err := in.InvokeContext(ctx, name, args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(results) == 0 {
			return 0
		}
		if len(results) != 1 {
			t.Fatalf("%s returned %v", name, results)
		}
		return results[0]
	}
	read := func(ptr, size uint64) []byte {
		t.Helper()
		if ptr > math.MaxUint32 || size > math.MaxUint32 {
			t.Fatal("PDFium output exceeds memory32")
		}
		data, ok := in.Read(uint32(ptr), uint32(size))
		if !ok {
			t.Fatalf("PDFium output [%d,%d) out of bounds", ptr, ptr+size)
		}
		return data
	}
	invoke("_initialize")
	invoke("FPDF_InitLibrary")
	ptr := invoke("malloc", uint64(len(pdf)))
	if ptr == 0 || !in.Write(uint32(ptr), pdf) {
		t.Fatal("allocate/write PDF")
	}
	doc := invoke("FPDF_LoadMemDocument", ptr, uint64(len(pdf)), 0)
	if doc == 0 {
		t.Fatalf("load PDF: error %d", invoke("FPDF_GetLastError"))
	}
	if pages := invoke("FPDF_GetPageCount", doc); pages != uint64(len(oracle.Pages)) {
		t.Fatalf("page count = %d, want %d", pages, len(oracle.Pages))
	}
	for index, want := range oracle.Pages {
		page := invoke("FPDF_LoadPage", doc, uint64(index))
		if page == 0 {
			t.Fatalf("load page %d", index)
		}
		if width, height := math.Float64frombits(invoke("FPDF_GetPageWidth", page)), math.Float64frombits(invoke("FPDF_GetPageHeight", page)); width != want.Width || height != want.Height {
			t.Fatalf("page %d size = %gx%g, want %gx%g", index, width, height, want.Width, want.Height)
		}
		textPage := invoke("FPDFText_LoadPage", page)
		if textPage == 0 {
			t.Fatal("load text page")
		}
		chars := invoke("FPDFText_CountChars", textPage)
		if chars != uint64(len(utf16.Encode([]rune(want.Text)))) {
			t.Fatalf("page %d character count = %d", index, chars)
		}
		buf := invoke("malloc", (chars+1)*2)
		if buf == 0 {
			t.Fatal("allocate text buffer")
		}
		units := invoke("FPDFText_GetText", textPage, 0, chars, buf)
		if units != chars+1 {
			t.Fatalf("text units = %d, want %d", units, chars+1)
		}
		raw := read(buf, units*2)
		words := make([]uint16, units)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(raw[i*2:])
		}
		if got := string(utf16.Decode(words[:len(words)-1])); got != want.Text || words[len(words)-1] != 0 {
			t.Fatalf("page %d text = %q, want %q", index, got, want.Text)
		}
		bitmap := invoke("FPDFBitmap_Create", 96, 128, 1)
		if bitmap == 0 {
			t.Fatal("allocate bitmap")
		}
		invoke("FPDFBitmap_FillRect", bitmap, 0, 0, 96, 128, math.MaxUint32)
		invoke("FPDF_RenderPageBitmap", bitmap, page, 0, 0, 96, 128, 0, 0)
		stride := invoke("FPDFBitmap_GetStride", bitmap)
		if stride != 96*4 {
			t.Fatalf("bitmap stride = %d", stride)
		}
		pixels := read(invoke("FPDFBitmap_GetBuffer", bitmap), stride*128)
		if got := fmt.Sprintf("%x", sha256.Sum256(pixels)); got != want.BitmapSHA256 {
			t.Fatalf("page %d bitmap SHA-256 = %s, want %s", index, got, want.BitmapSHA256)
		}
		invoke("FPDFBitmap_Destroy", bitmap)
		invoke("free", buf)
		invoke("FPDFText_ClosePage", textPage)
		invoke("FPDF_ClosePage", page)
	}
	invoke("FPDF_CloseDocument", doc)
	invoke("free", ptr)
	invoke("FPDF_DestroyLibrary")
}
