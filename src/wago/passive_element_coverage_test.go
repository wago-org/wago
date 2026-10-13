//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// TestPassiveElementCoverage is the phase-0 admission control for #917.
// A candidate has only constant funcref/null entries and no GC array use.
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
			if rel == "src/wago/testdata/gc_array_init_elem_generic.wasm" && !SupportedFeatures().IsEnabled(CoreFeatureGC) {
				t.Skip("native GC is unsupported on this target; fixture not surveyed")
			}
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModuleWithFeatures(append([]byte(nil), data...), wasm.ValidationFeatures{GCConstExpr: true})
			if err != nil {
				t.Fatal(err)
			}
			gcSegments, err := gcArrayElementSegments(m)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 & SupportedFeatures()).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			slots := len(c.passiveElems)
			eligible, entries, expanded, gcRequired := countPassiveElementCandidates(c, gcSegments)
			t.Logf("functions=%d passive_state_slots=%d passive_entries=%d eligible_segments=%d eager_gc_array_segments=%d expanded_entries_bytes=%d descriptor_bytes=%d", len(c.FuncTypeID), slots, entries, eligible, gcRequired, expanded, slots*runtime.PassiveElemDescBytes)
		})
	}
}

func countPassiveElementCandidates(c *Compiled, gcSegments map[uint32]bool) (eligible, entries, expanded, gcRequired int) {
	for i, segment := range c.passiveElems {
		if len(segment.Values) == 0 {
			continue
		}
		entries += len(segment.Values)
		expanded += elemEntryBytes(segment.RefType) * len(segment.Values)
		if gcSegments[uint32(i)] || c.memoryDir != nil && c.memoryDir.gcArrayElement != nil && int(c.memoryDir.gcArrayElement.SegmentIndex) == i {
			gcRequired++
			continue
		}
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
		if ok {
			eligible++
		}
	}
	return eligible, entries, expanded, gcRequired
}

func gcArrayElementSegments(m *wasm.Module) (map[uint32]bool, error) {
	segments := make(map[uint32]bool)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var imm wasm.InstructionImmediate
	for _, fn := range m.Code {
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return nil, err
			}
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				return nil, err
			}
			if imm.Kind == wasm.InstrArrayNewElem || imm.Kind == wasm.InstrArrayInitElem {
				segments[imm.Index2] = true // Index is the array type; Index2 is the element segment.
			}
		}
	}
	return segments, nil
}

func TestPassiveElementGenericGCEligibility(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureGC) {
		t.Skip("native GC is unsupported on this target; fixture not surveyed")
	}
	data, err := os.ReadFile("testdata/gc_array_init_elem_generic.wasm")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModuleWithFeatures(append([]byte(nil), data...), wasm.ValidationFeatures{GCConstExpr: true})
	if err != nil {
		t.Fatal(err)
	}
	gcSegments, err := gcArrayElementSegments(m)
	if err != nil {
		t.Fatal(err)
	}
	if !gcSegments[1] || len(gcSegments) != 1 {
		t.Fatalf("GC element segments=%v; want only segment 1", gcSegments)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 & SupportedFeatures()).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCArrayProduct() != stagedGCArrayProductGeneric {
		t.Fatal("fixture must use generic GC array execution")
	}
	eligible, _, _, required := countPassiveElementCandidates(c, gcSegments)
	if eligible != 0 || required != 1 {
		t.Fatalf("eligible=%d GC-required=%d; want 0 and 1", eligible, required)
	}
	// Add a simple payload to the unused dummy segment. It remains eligible.
	c.passiveElems[0].Values = append([]RefInit(nil), c.passiveElems[1].Values...)
	eligible, _, _, required = countPassiveElementCandidates(c, gcSegments)
	if eligible != 1 || required != 1 {
		t.Fatalf("with unused segment: eligible=%d GC-required=%d; want 1 and 1", eligible, required)
	}
}

func TestGCArrayElementSegmentIndices(t *testing.T) {
	for _, subopcode := range []byte{0x0a, 0x13} {
		m := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0xfb, subopcode, 2, 1, 0x0b}}}}
		segments, err := gcArrayElementSegments(m)
		if err != nil || len(segments) != 1 || !segments[1] {
			t.Fatalf("subopcode %#x: segments=%v err=%v; want segment 1, not type 2", subopcode, segments, err)
		}
	}
}
