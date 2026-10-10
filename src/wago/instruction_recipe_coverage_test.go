//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestInstructionRecipeFamilyCoverage is the first selection gate for #923.
// Frequencies are static Wasm sites, not dynamic execution or backend costs.
func TestInstructionRecipeFamilyCoverage(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"corpus/workloads/semantic/yyjson/yyjson.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{})
			if err != nil {
				t.Fatal(err)
			}
			classifier := wasm.NewModuleInstructionClassifier(m, true)
			var imm wasm.InstructionImmediate
			var i32DivRem, i64DivRem, fpMinMax, scalarMem int
			for _, fn := range m.Code {
				r := wasm.NewReader(fn.BodyBytes)
				for r.HasNext() {
					op, err := r.Byte()
					if err != nil {
						t.Fatal(err)
					}
					if err := classifier.ClassifyInto(r, op, &imm); err != nil {
						t.Fatal(err)
					}
					if op >= 0x6d && op <= 0x70 {
						i32DivRem++
					}
					if op >= 0x7f && op <= 0x82 {
						i64DivRem++
					}
					if op == 0x96 || op == 0x97 || op == 0xa4 || op == 0xa5 {
						fpMinMax++
					}
					if op >= 0x28 && op <= 0x3e {
						scalarMem++
					}
				}
			}
			t.Logf("functions=%d i32_divrem=%d i64_divrem=%d fp_minmax=%d scalar_memory=%d", len(m.Code), i32DivRem, i64DivRem, fpMinMax, scalarMem)
		})
	}
}
