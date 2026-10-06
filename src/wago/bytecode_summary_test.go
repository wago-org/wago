package wago

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/summaryfixtures"
)

type bytecodeSummaryExpectation struct {
	name                                string
	features                            CoreFeatures
	elem, data                          int
	memGrow                             []bool
	tableGrow, refFunc, callRef, atomic bool
}

var bytecodeSummaryExpectations = []bytecodeSummaryExpectation{
	{name: "gc", features: CoreFeatureReferenceTypes | CoreFeatureGC},
	{name: "table_init", features: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes, elem: 1, refFunc: true},
	{name: "table_copy", features: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes, elem: 1, refFunc: true},
	{name: "elem_drop", features: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes, elem: 1, refFunc: true},
	{name: "scalar"},
	{name: "blocks", features: CoreFeatureMultiValue},
	{name: "branch_table"},
	{name: "calls"},
	{name: "memory32", memGrow: []bool{true}},
	{name: "mixed_memory", features: CoreFeatureMultiMemory | CoreFeatureMemory64, memGrow: []bool{true, true}},
	{name: "bulk_data", features: CoreFeatureBulkMemoryOperations, data: 2, memGrow: []bool{false}},
	{name: "bulk_table", features: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes, elem: 2, refFunc: true},
	{name: "references", features: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes, tableGrow: true, refFunc: true},
	{name: "simd", features: CoreFeatureSIMD, memGrow: []bool{false}},
	{name: "atomic", features: CoreFeatureThreads, memGrow: []bool{false}, atomic: true},
	{name: "saturation", features: CoreFeatureSignExtensionOps | CoreFeatureNonTrappingFloatToIntConversion},
	{name: "eh", features: CoreFeatureExceptionHandling},
	{name: "tail", features: CoreFeatureTailCall},
	{name: "call_ref", features: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences, refFunc: true, callRef: true},
}

func bytecodeSummaryRead(t testing.TB, name string) *wasm.Module {
	t.Helper()
	var data []byte
	for _, fixture := range summaryfixtures.Modules {
		if fixture.Name == name {
			var err error
			data, err = hex.DecodeString(fixture.Hex)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if data == nil {
		t.Fatalf("unknown fixture %q", name)
	}
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func bytecodeSummaryRequirementGate(m *wasm.Module, got moduleRequirements, want bytecodeSummaryExpectation) error {
	if got.features != want.features || got.elemStateCount != want.elem || got.dataStateCount != want.data || got.atomicWaitHelpers != want.atomic {
		return fmt.Errorf("features/counts/helpers=%#x/%d/%d/%t want %#x/%d/%d/%t", uint64(got.features), got.elemStateCount, got.dataStateCount, got.atomicWaitHelpers, uint64(want.features), want.elem, want.data, want.atomic)
	}
	f := got.moduleFacts
	if len(f.MemoryGrowUsed) != len(want.memGrow) {
		return fmt.Errorf("memory fact count = %d, want %d", len(f.MemoryGrowUsed), len(want.memGrow))
	}
	if f.UsesRefFunc != want.refFunc || f.UsesCallRef != want.callRef {
		return fmt.Errorf("ref/call=%t/%t want %t/%t", f.UsesRefFunc, f.UsesCallRef, want.refFunc, want.callRef)
	}
	for i, v := range f.MemoryGrowUsed {
		if v != want.memGrow[i] {
			return fmt.Errorf("memory %d grow=%t", i, v)
		}
	}
	for i, v := range f.TableGrowUsed {
		if v != want.tableGrow {
			return fmt.Errorf("table %d grow=%t", i, v)
		}
	}
	front, e := frontend.AnalyzeModuleFacts(m)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(f, front) {
		return fmt.Errorf("frontend facts differ: %+v vs %+v", f, front)
	}
	return nil
}
func TestBytecodeSummaryRequirements(t *testing.T) {
	// Reuse one analysis object after every distinct predecessor. A successful
	// new validation must reset all summary flags.
	var reused wasm.ValidatedModuleAnalysis
	for _, before := range bytecodeSummaryExpectations {
		for _, want := range bytecodeSummaryExpectations {
			t.Run(before.name+"/"+want.name, func(t *testing.T) {
				pred := bytecodeSummaryRead(t, before.name)
				f := wasm.ValidationFeatures{MultiMemory: true}
				if e := wasm.ValidateModuleWithAnalysis(pred, f, 1, wasm.ValidationLimits{}, &reused); e != nil {
					t.Fatal(e)
				}
				m := bytecodeSummaryRead(t, want.name)
				if e := wasm.ValidateModuleWithAnalysis(m, f, 1, wasm.ValidationLimits{}, &reused); e != nil {
					t.Fatal(e)
				}
				if !reused.ValidFor(m) {
					t.Fatal("missing validated analysis")
				}
				var fresh wasm.ValidatedModuleAnalysis
				if e := wasm.ValidateModuleWithAnalysis(m, f, 1, wasm.ValidationLimits{}, &fresh); e != nil {
					t.Fatal(e)
				}
				validated := analyzeModuleRequirementsWithValidation(m, &reused)
				if e := bytecodeSummaryRequirementGate(m, validated, want); e != nil {
					t.Fatalf("validated summary: %v", e)
				}
				if !reflect.DeepEqual(validated, analyzeModuleRequirementsWithValidation(m, &fresh)) {
					t.Fatal("fresh and reused validation summaries differ")
				}
				for i := range m.Code {
					if reused.Func(i) != fresh.Func(i) {
						t.Fatalf("fresh/reused function %d facts differ", i)
					}
				}
				got := analyzeModuleRequirements(m)
				if e := bytecodeSummaryRequirementGate(m, got, want); e != nil {
					t.Error(e)
				}
				if !reflect.DeepEqual(got, validated) {
					t.Fatalf("scan=%+v facts=%+v; validation=%+v facts=%+v", got, got.moduleFacts, validated, validated.moduleFacts)
				}
			})
		}
	}
}
func TestBytecodeSummaryDisabledObserverControl(t *testing.T) {
	for _, want := range bytecodeSummaryExpectations {
		if want.name != "memory32" {
			continue
		}
		m := bytecodeSummaryRead(t, want.name)
		got := analyzeModuleRequirements(m)
		if e := bytecodeSummaryRequirementGate(m, got, want); e != nil {
			t.Fatal(e)
		}
		// Passing nil disables the same growth observer at the scan call site.
		got.moduleFacts = frontend.NewModuleFacts(m.TableCount(), m.MemCount())
		elem, data := 0, 0
		got.features = requiredFeaturesAndSegmentCountsForBodyBytes(m.Code[0].BodyBytes, &elem, &data, nil, nil, m, nil, nil, nil, nil)
		if err := bytecodeSummaryRequirementGate(m, got, want); err == nil || err.Error() != "memory 0 grow=false" {
			t.Fatalf("disabled-observer control error = %v, want missing growth fact", err)
		}
	}
}
func BenchmarkBytecodeSummaryRequirements(b *testing.B) {
	for _, name := range []string{"scalar", "branch_table", "mixed_memory", "simd", "bulk_table"} {
		b.Run(name, func(b *testing.B) {
			m := bytecodeSummaryRead(b, name)
			body := m.Code[0].BodyBytes
			c := wasm.NewModuleInstructionClassifier(m, true)
			b.Run("body", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for i := 0; i < b.N; i++ {
					elem, data := 0, 0
					bytecodeSummaryFeatureSink = requiredFeaturesAndSegmentCountsForBodyBytes(body, &elem, &data, nil, nil, m, nil, nil, nil, &c)
				}
			})
			b.Run("module", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(body)))
				for i := 0; i < b.N; i++ {
					bytecodeSummaryRequirementSink = analyzeModuleRequirements(m)
				}
			})
		})
	}
}

var bytecodeSummaryFeatureSink CoreFeatures
var bytecodeSummaryRequirementSink moduleRequirements

func TestBytecodeSummaryPartialContracts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       []byte
		want       CoreFeatures
		frontError bool
	}{
		{"scalar_error", []byte{0x41, 0x80}, 0, true},
		{"general_error", []byte{0xff}, 0, false},
		{"prefix_before_error", []byte{0xfd, 0x0c, 0, 1}, CoreFeatureSIMD, false},
		{"known_feature_before_error", []byte{0xc0, 0xff}, CoreFeatureSignExtensionOps, false},
		{"observer_after_stop", []byte{0xff, 0xd2, 0, 0x40, 0}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{Memories: []wasm.MemType{{}}, Tables: []wasm.Table{{}}, Code: []wasm.Func{{BodyBytes: tc.body}}}
			elem, data := 0, 0
			facts := frontend.NewModuleFacts(1, 1)
			got := requiredFeaturesAndSegmentCountsForBodyBytes(tc.body, &elem, &data, facts, nil, m, nil, nil, nil, nil)
			if got != tc.want || elem != 0 || data != 0 || facts.UsesRefFunc || facts.MemoryGrowUsed[0] || facts.TableGrowUsed[0] {
				t.Fatalf("partial result: features=%x facts=%+v", got, facts)
			}
			f, e := frontend.AnalyzeModuleFacts(m)
			if tc.frontError {
				if e == nil || f != nil {
					t.Fatalf("want direct error: %+v %v", f, e)
				}
				return
			}
			if e != nil || !f.UsesRefFunc || !f.MemoryGrowUsed[0] || !f.TableGrowUsed[0] {
				t.Fatalf("want conservative fallback: %+v %v", f, e)
			}
		})
	}
}
func TestBytecodeSummaryBulkTablePolicy(t *testing.T) {
	m := bytecodeSummaryRead(t, "bulk_table")
	f := frontend.AllFeatures()
	f.ReferenceTypes = false
	// The feature summary must preserve the existing reference-types policy.
	facts, e := frontend.AnalyzeModuleFacts(m)
	if e != nil {
		t.Fatal(e)
	}
	e = frontend.RejectUnsupportedWithFeaturesFactsAndValidation(m, f, facts, nil)
	if e == nil || !strings.Contains(e.Error(), "reference-types disabled") {
		t.Fatalf("expected reference-types policy rejection: %v", e)
	}
	t.Log(e)
}

func TestBytecodeSummaryBodyAllocations(t *testing.T) {
	for _, name := range []string{"scalar", "branch_table", "mixed_memory", "simd", "bulk_table"} {
		m := bytecodeSummaryRead(t, name)
		body := m.Code[0].BodyBytes
		c := wasm.NewModuleInstructionClassifier(m, true)
		n := testing.AllocsPerRun(100, func() {
			elem, data := 0, 0
			bytecodeSummaryFeatureSink = requiredFeaturesAndSegmentCountsForBodyBytes(body, &elem, &data, nil, nil, m, nil, nil, nil, &c)
		})
		if n != 0 {
			t.Fatalf("%s: %v allocations", name, n)
		}
	}
}

func TestBytecodeSummaryValidationErrorReset(t *testing.T) {
	var analysis wasm.ValidatedModuleAnalysis
	for _, before := range []string{"gc", "mixed_memory", "simd", "atomic"} {
		for _, want := range bytecodeSummaryExpectations {
			m := bytecodeSummaryRead(t, before)
			f := wasm.ValidationFeatures{MultiMemory: true}
			if e := wasm.ValidateModuleWithAnalysis(m, f, 1, wasm.ValidationLimits{}, &analysis); e != nil {
				t.Fatal(e)
			}
			m.Code[0].BodyBytes = []byte{0xff}
			if e := wasm.ValidateModuleWithAnalysis(m, f, 1, wasm.ValidationLimits{}, &analysis); e == nil || analysis.ValidFor(m) {
				t.Fatal("failed validation retained valid analysis")
			}
			good := bytecodeSummaryRead(t, want.name)
			if e := wasm.ValidateModuleWithAnalysis(good, f, 1, wasm.ValidationLimits{}, &analysis); e != nil {
				t.Fatal(e)
			}
			if e := bytecodeSummaryRequirementGate(good, analyzeModuleRequirementsWithValidation(good, &analysis), want); e != nil {
				t.Fatalf("%s -> error -> %s: %v", before, want.name, e)
			}
		}
	}
}

func BenchmarkBytecodeSummaryCompile(b *testing.B) {
	for _, name := range []string{"scalar", "memory32", "bulk_table", "simd", "references", "mixed_memory"} {
		b.Run(name, func(b *testing.B) {
			var data []byte
			for _, fixture := range summaryfixtures.Modules {
				if fixture.Name == name {
					var err error
					data, err = hex.DecodeString(fixture.Hex)
					if err != nil {
						b.Fatal(err)
					}
					break
				}
			}
			if data == nil {
				b.Fatal("missing fixture")
			}
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				compiled, err := Compile(cfg, data)
				if err != nil {
					b.Fatal(err)
				}
				compiled.Close()
			}
		})
	}
}
