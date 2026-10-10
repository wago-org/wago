//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type repeatedLoadKey struct {
	local  uint32
	opcode byte
	offset uint64
	memory uint32
}

// TestRepeatedLinearLoadUpperBound counts a strict straight-line, local-address
// source pattern. It is an upper bound: Railshot can fold/consume the first load
// and register pressure can make a retained value more expensive than a reload.
func TestRepeatedLinearLoadUpperBound(t *testing.T) {
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
			var loads, repeats, effectKills, controlKills int
			for _, fn := range m.Code {
				lastLocal, haveLocal := uint32(0), false
				var prior repeatedLoadKey
				havePrior := false
				r := wasm.NewReader(fn.BodyBytes)
				for r.HasNext() {
					op, err := r.Byte()
					if err != nil {
						t.Fatal(err)
					}
					if err := classifier.ClassifyInto(r, op, &imm); err != nil {
						t.Fatal(err)
					}
					if op == 0x20 {
						lastLocal, haveLocal = imm.Index, true
						continue
					}
					if op >= 0x28 && op <= 0x35 {
						loads++
						if haveLocal && (!imm.HasMemIndex || imm.MemIndex == 0) {
							key := repeatedLoadKey{lastLocal, op, imm.MemOffset, 0}
							if havePrior && key == prior {
								repeats++
							}
							prior, havePrior = key, true
						}
						haveLocal = false
						continue
					}
					haveLocal = false
					// Ambiguous memory effects and control transitions invalidate reuse.
					if (op >= 0x36 && op <= 0x40) || op == 0x10 || op == 0x11 || op == 0x12 || op == 0x13 || op == 0xfc || op == 0xfe || op == 0xfd {
						if havePrior {
							effectKills++
						}
						havePrior = false
					} else if op <= 0x0f || op == 0x21 || op == 0x22 {
						if havePrior {
							controlKills++
						}
						havePrior = false
					}
				}
			}
			t.Logf("scalar_loads=%d strict_local_address_repeats_upper_bound=%d effect_kills=%d control_or_local_write_kills=%d", loads, repeats, effectKills, controlKills)
		})
	}
}
