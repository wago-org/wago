//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// TestPassiveElementCoverage is the phase-0 admission control for #917.
// A segment is counted only if all entries are constant funcref/null tags.
func TestPassiveElementCoverage(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"src/wago/testdata/gc_array_init_elem_generic.wasm",
		"examples/22-language-guests/assemblyscript/answer.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			slots, eligible, entries, expanded, gcRequired := len(c.passiveElems), 0, 0, 0, 0
			for i, segment := range c.passiveElems {
				if len(segment.Values) == 0 {
					continue
				}
				entries += len(segment.Values)
				expanded += elemEntryBytes(segment.RefType) * len(segment.Values)
				if normalizedElemRefType(segment.RefType) != ValFuncRef {
					continue
				}
				ok := true
				for _, value := range segment.Values {
					if value.Expr != nil || value.HasGlobal || value.RepeatPrevious {
						ok = false
						break
					}
				}
				if c.memoryDir != nil && c.memoryDir.gcArrayElement != nil && int(c.memoryDir.gcArrayElement.SegmentIndex) == i {
					gcRequired++
					ok = false
				}
				if ok {
					eligible++
				}
			}
			t.Logf("functions=%d passive_state_slots=%d passive_entries=%d eligible_segments=%d eager_gc_array_segments=%d expanded_entries_bytes=%d descriptor_bytes=%d", len(c.FuncTypeID), slots, entries, eligible, gcRequired, expanded, slots*runtime.PassiveElemDescBytes)
		})
	}
}
