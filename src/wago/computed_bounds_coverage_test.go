//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type computedBoundsKey struct {
	local  uint32
	add    int32
	offset uint64
	width  byte
}

// TestComputedBoundsSourceUpperBound measures one exact, easy-to-prove source
// shape. This precedes backend pin/fold/certificate selection and is therefore
// only an upper bound for this shape, not a count of eliminated checks.
func TestComputedBoundsSourceUpperBound(t *testing.T) {
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
			var loads, forms, repeats int
			for _, fn := range m.Code {
				var stage uint8
				var local uint32
				var add int32
				var prior computedBoundsKey
				havePrior := false
				r := wasm.NewReader(fn.BodyBytes)
				for r.HasNext() {
					op, err := r.Byte()
					if err != nil {
						t.Fatal(err)
					}
					if op == 0x41 {
						v, err := r.I32()
						if err != nil {
							t.Fatal(err)
						}
						if stage == 1 {
							add, stage = v, 2
						} else {
							stage = 0
						}
						continue
					}
					if err := classifier.ClassifyInto(r, op, &imm); err != nil {
						t.Fatal(err)
					}
					if op == 0x20 {
						local, stage = imm.Index, 1
						continue
					}
					if op == 0x6a && stage == 2 {
						stage = 3
						continue
					}
					if op >= 0x28 && op <= 0x35 {
						loads++
						if stage == 3 && (!imm.HasMemIndex || imm.MemIndex == 0) {
							forms++
							key := computedBoundsKey{local, add, imm.MemOffset, op}
							if havePrior && key == prior {
								repeats++
							}
							prior, havePrior = key, true
						}
						stage = 0
						continue
					}
					stage = 0
					if (op >= 0x36 && op <= 0x40) || op == 0x10 || op == 0x11 || op == 0x12 || op == 0x13 || op == 0xfc || op == 0xfd || op == 0xfe || op <= 0x0f || op == 0x21 || op == 0x22 {
						havePrior = false
					}
				}
			}
			t.Logf("scalar_loads=%d local_plus_constant_load_forms=%d exact_repeats_upper_bound=%d", loads, forms, repeats)
		})
	}
}
