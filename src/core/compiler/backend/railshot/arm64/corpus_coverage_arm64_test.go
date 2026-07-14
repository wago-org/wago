//go:build arm64

package arm64

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
)

// TestInstructionCorpusCompiles covers real opcode combinations through the
// ARM64 decode/validation/lowering pipeline. Keep this counterpart to the
// amd64 corpus test so supported targets receive equivalent regression breadth.
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
