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
	features := CoreFeaturesV3 & SupportedFeatures()
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
			switch filepath.Base(rel) {
			case "gc_constexpr_element_roots.wasm", "gc_array_init_elem_generic.wasm":
				if !features.IsEnabled(CoreFeatureGC) {
					t.Skip("fixture requires native GC support")
				}
			}
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
			c, err := NewRuntimeConfig().WithCoreFeatures(features).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			refs, referenced := simpleElemFuncRefTargets(c)
			unique := 0
			pages := make(map[int]bool)
			for i, yes := range referenced {
				if yes {
					unique++
					addFuncRefDescriptorPages(pages, i)
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

func addFuncRefDescriptorPages(pages map[int]bool, index int) {
	start := (index + 1) * runtime.FuncRefDescBytes
	pages[start/4096] = true
	pages[(start+runtime.FuncRefDescBytes-1)/4096] = true
}

func TestFuncRefDescriptorPageSpan(t *testing.T) {
	for _, tc := range []struct {
		index int
		want  int
	}{
		{0, 1},
		{101, 2}, // Descriptor bytes 4080 through 4119 cross the 4096 boundary.
		{102, 1},
	} {
		pages := make(map[int]bool)
		addFuncRefDescriptorPages(pages, tc.index)
		if len(pages) != tc.want {
			t.Errorf("descriptor %d touches %d pages; want %d", tc.index, len(pages), tc.want)
		}
	}
}

func simpleElemFuncRefTargets(c *Compiled) (refs int, referenced []bool) {
	referenced = make([]bool, len(c.FuncTypeID))
	// Passive state slots for active/declarative segments have no payload,
	// so the two slices do not duplicate entries.
	for _, segments := range [][]ElemInit{c.Elems, c.passiveElems} {
		for _, segment := range segments {
			if normalizedElemRefType(segment.RefType) != ValFuncRef {
				continue
			}
			for _, value := range segment.Values {
				if value.Null || value.HasGlobal || value.Expr != nil || value.RepeatPrevious || int(value.FuncIndex) >= len(referenced) {
					continue
				}
				refs++
				referenced[value.FuncIndex] = true
			}
		}
	}
	return refs, referenced
}

func TestSparseFuncRefPassiveElementCoverage(t *testing.T) {
	features := CoreFeaturesV3 & SupportedFeatures()
	if !features.IsEnabled(CoreFeatureGC) {
		t.Skip("fixture requires native GC support")
	}
	data, err := os.ReadFile("testdata/gc_array_init_elem_generic.wasm")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(features).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.Elems) != 0 || len(c.passiveElems) == 0 {
		t.Fatal("fixture must contain passive elements without active elements")
	}
	refs, referenced := simpleElemFuncRefTargets(c)
	unique := 0
	pages := make(map[int]bool)
	for i, yes := range referenced {
		if yes {
			unique++
			addFuncRefDescriptorPages(pages, i)
		}
	}
	if refs != 1 || unique != 1 || !referenced[0] || len(pages) != 1 {
		t.Fatalf("element_refs=%d unique_targets=%d target0=%v pages=%d; want 1, 1, true, 1", refs, unique, referenced[0], len(pages))
	}
}

func TestSparseFuncRefRepeatedExpressionIsNotSimpleTarget(t *testing.T) {
	c := &Compiled{
		FuncTypeID: make([]uint64, 2),
		Elems: []ElemInit{{RefType: ValFuncRef, Values: []RefInit{
			{Expr: []byte{0xd2, 1, 0x0b}},
			{RepeatPrevious: true},
		}}},
	}
	refs, referenced := simpleElemFuncRefTargets(c)
	if refs != 0 || referenced[0] || referenced[1] {
		t.Fatalf("expression and repeat marker counted as simple targets: refs=%d targets=%v", refs, referenced)
	}
}
