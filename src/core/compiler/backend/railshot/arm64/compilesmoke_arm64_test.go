//go:build arm64

package arm64

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func mod1(t *testing.T, params, results []wasm.ValType, funcBody []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(funcBody))), funcBody...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestCompileSmoke exercises the ported arm64 backend's full compile path (hint
// scan → prologue → condense engine → register allocator → control flow →
// epilogue → module layout) under qemu, asserting it produces non-empty AArch64
// code without erroring or panicking. It does not execute the code (that needs
// the real enterNative arm64 trampoline); it verifies the code *generator* runs.
func TestCompileSmoke(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	cases := []struct {
		name string
		in   []wasm.ValType
		out  []wasm.ValType
		body []byte
	}{
		{"add", i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}},
		{"mul", i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}},
		{"const_add", i32, i32, []byte{0x00, 0x41, 0xe8, 0x07, 0x20, 0x00, 0x6a, 0x0b}},
		{"lt_s", i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x48, 0x0b}},
		{"eqz", i32, i32, []byte{0x00, 0x20, 0x00, 0x45, 0x0b}},
		{
			// if (x!=0) 10 else 20
			"if_else", i32, i32,
			[]byte{
				0x01, 0x01, 0x7f, 0x20, 0x00, 0x45,
				0x04, 0x7f, 0x41, 0x14, 0x05, 0x41, 0x0a, 0x0b, 0x0b,
			},
		},
		{
			// iterative fib(n)
			"fib", i32, i32,
			[]byte{
				0x01, 0x03, 0x7f,
				0x41, 0x00, 0x21, 0x01, 0x41, 0x01, 0x21, 0x02, 0x41, 0x00, 0x21, 0x03,
				0x02, 0x40, 0x03, 0x40,
				0x20, 0x03, 0x20, 0x00, 0x4e, 0x0d, 0x01,
				0x20, 0x01, 0x20, 0x02, 0x6a, 0x20, 0x02, 0x21, 0x01, 0x21, 0x02,
				0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03,
				0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x01, 0x0b,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.in, tc.out, tc.body)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("CompileModule: %v", err)
			}
			if len(cm.Code) == 0 {
				t.Fatal("empty code")
			}
			if len(cm.Code)%4 != 0 {
				t.Errorf("code length %d not a multiple of 4 (A64 words)", len(cm.Code))
			}
		})
	}
}

func TestInstructionCorpusCompiles(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{
		"isa_i32.wasm", "isa_i64.wasm", "isa_f32.wasm", "isa_f64.wasm", "isa_cvt.wasm", "isa_ctl.wasm", "isa_call.wasm", "isa_mem.wasm", "isa_bulk_mem.wasm", "isa_var.wasm",
		"isa_simd_i8x16.wasm", "isa_simd_i16x8.wasm", "isa_simd_i32x4.wasm", "isa_simd_i64x2.wasm", "isa_simd_f32x4.wasm", "isa_simd_f64x2.wasm", "isa_simd_reduce.wasm", "isa_simd_v128.wasm",
		"arith.wasm", "branches.wasm", "dispatch.wasm", "fib_iter.wasm", "fib_rec.wasm", "float.wasm", "globals.wasm", "linked_list.wasm", "many_funcs.wasm", "memory.wasm", "memory_tree.wasm", "sieve.wasm", "matmul.wasm", "quicksort.wasm", "fannkuch.wasm", "nbody.wasm", "sha256.wasm", "raytrace.wasm", "spectralnorm.wasm", "wasm3.wasm",
		"base64x.wasm", "bignum.wasm", "blake3sum.wasm", "json-as.wasm", "utf-as.wasm", "blake-as.wasm", "blake-as-simd.wasm", "json-as-simd.wasm", "utf-as-simd.wasm", "jsonproc.wasm", "lua.wasm", "markdown.wasm", "sqlite3.wasm", "regexmatch.wasm", "crc32.wasm", "crcsum.wasm", "script.wasm", "esbuild.wasm", "ruby.wasm",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpus, name))
			if err != nil {
				t.Fatal(err)
			}
			mod, err := frontend.DecodeValidate(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompileModule(mod); err != nil {
				t.Fatal(err)
			}
		})
	}
}
