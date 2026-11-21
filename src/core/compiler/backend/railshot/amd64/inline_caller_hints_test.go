//go:build amd64

package amd64

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInlineCallerHintPlanParity(t *testing.T) {
	const n = inlineLinearSeenTargets + 4
	data := &inlineTargetData{slots: make([]uint32, n), targets: make([]inlineTarget, n)}
	for i := range n {
		data.slots[i] = uint32(i + 1)
		data.targets[i].globalIdx = i
	}
	data.slots[2] = 0
	data.targets[1].params = inlineRecursiveFlag
	for _, wide := range []bool{false, true} {
		m := &wasm.Module{Memories: []wasm.MemType{{}, {Limits: wasm.Limits{Addr64: wide}}}}
		targets := inlineTargetTable{data: data, classifier: wasm.NewModuleInstructionClassifier(m, true)}
		check := func(body []byte, mustScan ...bool) {
			t.Helper()
			tape := newInlineCallerHints(1, smallInlineCallerHintBytes)
			tape.begin()
			h := newFuncHints(4, 0)
			h.inlineCaller = tape
			elig := newGlobalEligibilityTracker(0)
			var globals shared.GlobalHintAccumulator
			globals.Reset(0)
			_, err := scanBodyBytesIntoMemory64WithModuleCalls(body, 4, 0, 99, h, &elig, false, m, nil, nil, false, nil, nil, 0, &globals)
			if err != nil {
				if len(mustScan) != 0 && mustScan[0] {
					t.Fatalf("fixture scan failed: %v, body=%x", err, body)
				}
				if _, known := tape.plan(0, targets); known {
					t.Fatal("failed scan published a caller plan")
				}
				return
			}
			tape.finish(0)
			got, known := tape.plan(0, targets)
			want := buildInlineCallerPlan(&wasm.Func{BodyBytes: body}, targets)
			if !known && len(want.callees) == 0 && !want.allCallsInline {
				return // success with no plan can remain sparse
			}
			if !known || !reflect.DeepEqual(got, want) {
				t.Fatalf("wide=%v body=%x: got=%+v known=%v want=%+v", wide, body, got, known, want)
			}
		}
		check([]byte{0x10, 0, 0x10, 3, 0x0b}, true)
		check([]byte{0x10, 0, 0x12, 3, 0x0b}, true)
		check([]byte{0x0b}, true)
		atoms := [][]byte{
			{0x10, 0}, {0x10, 1}, {0x10, 2}, {0x10, 3}, {0x10, 99},
			{0x11, 0, 0}, {0x12, 0}, {0x13, 0, 0}, {0x14, 0}, {0x15, 0},
			{0x41, 0x10}, {0x42, 0x11}, {0x43, 0x10, 0x11, 0x12, 0x13}, {0x44, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0, 0},
			{0x28, 0x40, 1, 0x10}, {0x36, 0x40, 1, 0x11},
			{0x02, 0x40, 0x10, 0, 0x0b}, {0x03, 0x40, 0x10, 3, 0x0b},
			{0x04, 0x40, 0x10, 0, 0x05, 0x10, 3, 0x0b},
			{0x0e, 2, 0x10, 0x11, 0x12}, {0xfc, 0x0a, 0, 0}, {0x1a}, {0x20, 0}, {0x21, 0}, {0x6a},
		}
		rng := rand.New(rand.NewSource(98))
		for sample := 0; sample < 512; sample++ {
			var body []byte
			for i := 0; i < 32; i++ {
				body = append(body, atoms[rng.Intn(len(atoms))]...)
			}
			check(append(body, 0x0b), true)
		}
		var body []byte
		for i := range n {
			body = append(body, 0x10, byte(i))
		}
		check(append(body, 0x10, 0, 0x0b))
		for op := 0; op < 256; op++ {
			for _, payload := range [][]byte{nil, {0}, {0x40}, {0x80}, {0xff, 0xff, 0xff, 0xff, 0x0f}} {
				body := append([]byte{0x10, 0, byte(op)}, payload...)
				body = append(body, 0x10, 3, 0x0b)
				check(body)
				for cut := 1; cut < len(body); cut++ {
					check(body[:cut])
				}
			}
		}
	}
}

func TestInlineCallerHintFallbackAndBudget(t *testing.T) {
	var absent *inlineCallerHints
	if absent.begin() {
		t.Fatal("nil tape started")
	}
	if _, known := absent.plan(0, inlineTargetTable{}); known {
		t.Fatal("nil tape known")
	}
	const budget = inlineCallerHintHeaderBytes + 8*inlineCallerWordBytes + 4*inlineCallerRangeBytes
	tape := newInlineCallerHints(2, budget)
	if _, known := tape.plan(0, inlineTargetTable{}); known {
		t.Fatal("unknown AST range known")
	}
	tape.begin()
	tape.record(0)
	tape.finish(0)
	if _, known := tape.plan(0, inlineTargetTable{}); !known {
		t.Fatal("complete range unknown")
	}
	tape.begin()
	tape.blocked = true
	for range 8 {
		tape.record(1)
	}
	tape.finish(1)
	if !tape.disabled || tape.calls != nil || tape.ranges != nil {
		t.Fatal("budget did not discard cache")
	}
	if _, known := tape.plan(0, inlineTargetTable{}); known {
		t.Fatal("overflow retained earlier range")
	}
	if newInlineCallerHints(1, inlineCallerHintHeaderBytes-1) != nil {
		t.Fatal("undersized budget retained header")
	}
}

func TestSparseInlineCallerRangesPreserveOrderAndBlockers(t *testing.T) {
	data := &inlineTargetData{
		slots:   []uint32{1, 2},
		targets: []inlineTarget{{globalIdx: 3}, {globalIdx: 4}},
	}
	targets := inlineTargetTable{first: 3, data: data, classifier: wasm.NewModuleInstructionClassifier(nil, true)}
	tape := newInlineCallerHints(8, smallInlineCallerHintBytes)
	for function, body := range [][]byte{
		{0x10, 3, 0x10, 3, 0x0b},          // repeated local target
		{0x0b},                            // call-free hole
		{0x10, 1, 0x0b},                   // imported-only (blocker)
		{0x10, 4, 0x10, 1, 0x10, 3, 0x0b}, // mixed, first-seen order
		{0x12, 3, 0x0b},                   // tail-only (blocker)
		{0x10, 3, 0x0b},
	} {
		tape.begin()
		h := newFuncHints(0, 0)
		h.inlineCaller = tape
		elig := newGlobalEligibilityTracker(0)
		var globals shared.GlobalHintAccumulator
		if _, err := scanBodyBytesIntoMemory64WithModuleCalls(body, 0, 0, 99, h, &elig, false, nil, nil, nil, false, nil, nil, 3, &globals); err != nil {
			t.Fatal(err)
		}
		tape.finish(function)
		got, known := tape.plan(function, targets)
		want := buildInlineCallerPlan(&wasm.Func{BodyBytes: body}, targets)
		if known != (function != 1) || !reflect.DeepEqual(got, want) {
			t.Fatalf("function %d: cached=%+v known=%v body=%+v", function, got, known, want)
		}
	}
	if len(tape.calls) != 5 || len(tape.ranges) != 5 || tape.storageBytes() > smallInlineCallerHintBytes {
		t.Fatalf("expected local calls and sparse range records only: %+v", tape)
	}
}

func TestInlineCallerHintModuleIntegration(t *testing.T) {
	savedInline := inlineEnabled
	inlineEnabled = true
	t.Cleanup(func() { inlineEnabled = savedInline })
	body := append([]byte{0, 0x10, 1}, bytes.Repeat([]byte{0x01}, 64<<10)...)
	body = append(body, 0x0b)
	m := modFuncs(t, funcDef{body: body}, funcDef{body: []byte{0, 0x01, 0x0b}}, funcDef{body: []byte{0, 0x0b}})
	// AST-only entries remain unknown even when the module has a bytecode cache.
	m.Code[2].BodyBytes = nil
	policy := currentCodegenPolicy()
	hints, sidecar, _, err := computeModuleHintsWithPolicy(m, 0, 0, nil, false, policy, false)
	if err != nil {
		t.Fatal(err)
	}
	if sidecar.inlineCaller == nil {
		t.Fatal("large serial module did not collect calls")
	}
	targets := buildInlineTargets(m, hints, policy)
	if targets.target(1) == nil {
		t.Fatal("fixture has no admitted target")
	}
	for i := range m.Code {
		view := sidecar.viewAt(hints[i], i)
		got, known := view.inlineCaller.plan(view.inlineCallerIndex, targets)
		if i != 0 {
			if known {
				t.Fatalf("function %d: call-free or AST body published cached plan", i)
			}
			continue
		}
		want := buildInlineCallerPlan(&m.Code[i], targets)
		if !known || !reflect.DeepEqual(got, want) {
			t.Fatalf("function %d: cached=%+v known=%v body=%+v", i, got, known, want)
		}
	}
	if sidecar.view(hints[0]).inlineCaller != nil {
		t.Fatal("unindexed view exposed indexed cache")
	}
	_, gotBytes := funcHintStorageBytes(hints, sidecar)
	without := sidecar
	without.inlineCaller = nil
	_, baseBytes := funcHintStorageBytes(hints, without)
	tape := sidecar.inlineCaller
	wantBytes := uint64(cap(tape.calls))*4 + uint64(cap(tape.ranges))*8 + uint64(unsafe.Sizeof(*tape))
	if gotBytes-baseBytes != wantBytes || wantBytes > maxInlineCallerHintBytes {
		t.Fatalf("cache accounting=%d want=%d", gotBytes-baseBytes, wantBytes)
	}
	// Smaller modules with a meaningful rescan use a small, sparse budget.
	m.Code[0].BodyBytes = append([]byte{0x10, 1}, bytes.Repeat([]byte{0x01}, minInlineCallerHintBody)...)
	m.Code[0].BodyBytes = append(m.Code[0].BodyBytes, 0x0b)
	_, small, _, err := computeModuleHintsWithPolicy(m, 0, 0, nil, false, policy, false)
	if err != nil {
		t.Fatal(err)
	}
	if tape := small.inlineCaller; tape == nil || len(tape.ranges) != 1 || cap(tape.calls) != 8 ||
		tape.storageBytes() > smallInlineCallerHintBytes {
		t.Fatalf("small module tape=%+v", tape)
	}
	got, known := small.inlineCaller.plan(0, targets)
	want := buildInlineCallerPlan(&m.Code[0], targets)
	if !known || !reflect.DeepEqual(got, want) {
		t.Fatalf("small caller: cached=%+v known=%v body=%+v", got, known, want)
	}

	// A tiny body still takes the original allocation-free tape path.
	m.Code[0].BodyBytes = []byte{0x10, 1, 0x0b}
	_, tiny, _, err := computeModuleHintsWithPolicy(m, 0, 0, nil, false, policy, false)
	if err != nil {
		t.Fatal(err)
	}
	if tiny.inlineCaller != nil {
		t.Fatal("tiny module retained a cache")
	}
}

func TestInlineCallTapeCanBeSuppressedForGCRootPlans(t *testing.T) {
	saved := inlineEnabled
	inlineEnabled = true
	t.Cleanup(func() { inlineEnabled = saved })
	body := append([]byte{0, 0x10, 1}, bytes.Repeat([]byte{1}, 64<<10)...)
	body = append(body, 0x0b)
	m := modFuncs(t, funcDef{body: body}, funcDef{body: []byte{0, 0x0b}})
	policy := currentCodegenPolicy()
	for _, eligible := range []bool{true, false} {
		_, sidecar, _, err := computeModuleHintsWithWorkersResidencyPolicy(m, 0, 0, 1, nil, false, policy, false, eligible)
		if err != nil {
			t.Fatal(err)
		}
		if got := sidecar.inlineCaller != nil; got != eligible {
			t.Fatalf("inline eligibility=%t: call tape=%t", eligible, got)
		}
	}
}
