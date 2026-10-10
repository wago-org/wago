//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func bodyRefFuncTargets(m *wasm.Module) (sites, unique int, err error) {
	seen := make([]bool, m.FuncCount())
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var immediate wasm.InstructionImmediate
	for _, fn := range m.Code {
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, readErr := r.Byte()
			if readErr != nil {
				return 0, 0, readErr
			}
			if op == 0xd2 {
				index, readErr := r.U32()
				if readErr != nil {
					return 0, 0, readErr
				}
				sites++
				if int(index) < len(seen) && !seen[index] {
					seen[index] = true
					unique++
				}
				continue
			}
			if readErr := classifier.ClassifyInto(r, op, &immediate); readErr != nil {
				return 0, 0, readErr
			}
		}
	}
	return sites, unique, nil
}

// TestSparseFuncRefCoverage is the phase-0 resource control for #916. It is
// deliberately a report, not a production policy: dynamic ref.func targets and
// late host requests need separate proof before sparse writes are safe.
func TestSparseFuncRefCoverage(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"src/wago/testdata/gc_constexpr_element_roots.wasm",
		"src/wago/testdata/gc_array_init_elem_generic.wasm",
		"examples/22-language-guests/tinygo/answer.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			module, err := wasm.DecodeModuleWithFeatures(append([]byte(nil), data...), wasm.ValidationFeatures{GCConstExpr: true})
			if err != nil {
				t.Fatal(err)
			}
			bodySites, bodyUnique, err := bodyRefFuncTargets(module)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			refs, referenced := 0, make([]bool, len(c.FuncTypeID))
			for _, segment := range c.Elems {
				if normalizedElemRefType(segment.RefType) != ValFuncRef {
					continue
				}
				for _, value := range segment.Values {
					if value.Null || value.HasGlobal || value.Expr != nil || int(value.FuncIndex) >= len(referenced) {
						continue
					}
					refs++
					referenced[value.FuncIndex] = true
				}
			}
			unique := 0
			pages := make(map[int]bool)
			for i, yes := range referenced {
				if yes {
					unique++
					pages[((i+1)*runtime.FuncRefDescBytes)/4096] = true
				}
			}
			bytes := 0
			if c.needsFuncRefDescs() {
				bytes = (len(c.FuncTypeID) + 1) * runtime.FuncRefDescBytes
			}
			t.Logf("functions=%d descriptor_needed=%v compile_flag=%v funcref_table=%v element_refs=%d unique_element_targets=%d body_ref_func_sites=%d unique_body_targets=%d dense_descriptor_bytes=%d element_touched_pages=%d total_descriptor_pages=%d exports=%d", len(c.FuncTypeID), c.needsFuncRefDescs(), c.NeedsFuncRefDescs, c.hasFuncrefTable(), refs, unique, bodySites, bodyUnique, bytes, len(pages), (bytes+4095)/4096, len(c.Exports))
		})
	}
}
