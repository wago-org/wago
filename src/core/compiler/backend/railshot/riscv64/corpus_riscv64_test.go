//go:build linux && riscv64

package riscv64

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
)

func corpusDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "bench", "corpus")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("bench/corpus not found")
		}
		dir = parent
	}
}

func TestScalarCorpusCompiles(t *testing.T) {
	corpus := corpusDir(t)
	for _, name := range []string{
		"isa_i32.wasm", "isa_i64.wasm", "isa_f32.wasm", "isa_f64.wasm", "isa_cvt.wasm", "isa_ctl.wasm", "isa_call.wasm", "isa_mem.wasm", "isa_bulk_mem.wasm", "isa_var.wasm",
		"arith.wasm", "branches.wasm", "dispatch.wasm", "fib_iter.wasm", "fib_rec.wasm", "float.wasm", "globals.wasm", "linked_list.wasm", "many_funcs.wasm", "memory.wasm", "memory_tree.wasm", "sieve.wasm", "matmul.wasm", "quicksort.wasm", "fannkuch.wasm", "nbody.wasm", "sha256.wasm", "raytrace.wasm", "spectralnorm.wasm", "wasm3.wasm",
		"base64x.wasm", "bignum.wasm", "blake3sum.wasm", "json-as.wasm", "utf-as.wasm", "blake-as.wasm", "jsonproc.wasm", "lua.wasm", "markdown.wasm", "sqlite3.wasm", "regexmatch.wasm", "crc32.wasm", "crcsum.wasm", "script.wasm", "esbuild.wasm", "ruby.wasm",
	} {
		t.Run(strings.TrimSuffix(name, ".wasm"), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpus, name))
			if err != nil {
				t.Fatal(err)
			}
			mod, err := frontend.DecodeValidate(data)
			if err != nil {
				t.Fatal(err)
			}
			cm, err := CompileModule(mod)
			if err != nil {
				t.Fatal(err)
			}
			if len(cm.Code) == 0 && len(mod.Code) != 0 {
				t.Fatal("empty code")
			}
		})
	}
}

func TestSIMDCorpusCompilesWithoutRVV(t *testing.T) {
	path := filepath.Join(corpusDir(t), "isa_simd_v128.wasm")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := frontend.DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModule(mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Code) == 0 {
		t.Fatal("empty SIMD code")
	}
}
