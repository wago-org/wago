//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"reflect"
	"testing"
)

func TestSmallInlineCallerSummaryParity(t *testing.T) {
	data := &inlineTargetData{slots: []uint32{1, 2}, targets: []inlineTarget{{globalIdx: 0}, {globalIdx: 1}}}
	targets := inlineTargetTable{data: data, classifier: wasm.NewModuleInstructionClassifier(nil, true)}
	cases := [][]byte{
		{0x0b}, {0x10, 0, 0x10, 1, 0x10, 0, 0x0b}, {0x10, 9, 0x0b},
		{0x10, 0, 0x12, 1, 0x0b}, {0x11, 0, 0, 0x0b}, {0x14, 0, 0x0b},
		{2, 0x40, 0x10, 1, 0x0b, 0x0b}, {0x10}, {0x10, 0, 0x0b, 1},
	}
	tape := newSmallInlineCallerHints(100000)
	if tape.smallBytes() >= 1024 || len(tape.ranges) != 0 {
		t.Fatal("small path eagerly allocated dense function ranges")
	}
	for i, body := range cases {
		if !tape.begin() {
			t.Fatal("unexpected disabled summary")
		}
		h := newFuncHints(0, 0)
		h.inlineCaller = tape
		elig := newGlobalEligibilityTracker(0)
		var globals shared.GlobalHintAccumulator
		globals.Reset(0)
		_, err := scanBodyBytesIntoMemory64WithModuleCalls(body, 0, 0, 99, h, &elig, false, nil, nil, nil, false, nil, nil, 0, &globals)
		if err != nil {
			if _, known := tape.plan(i, targets); known {
				t.Fatal("failed scan published summary")
			}
			continue
		}
		tape.finish(i)
		got, known := tape.plan(i, targets)
		want := buildInlineCallerPlan(&wasm.Func{BodyBytes: body}, targets)
		if i == 0 {
			if known {
				t.Fatal("no-call body retained")
			}
			continue
		}
		if !known || !reflect.DeepEqual(got, want) {
			t.Fatalf("case%d got=%+v known=%v want=%+v", i, got, known, want)
		}
	}
	if _, known := tape.plan(99999, targets); known {
		t.Fatal("unknown sparse entry published")
	}
}
func TestSmallInlineCallerSummaryBudget(t *testing.T) {
	tape := newSmallInlineCallerHints(1)
	tape.begin()
	for i := 0; i < maxSmallInlineCallerHintBytes; i++ {
		tape.record(uint32(i))
		if tape.disabled {
			break
		}
		if tape.smallBytes() > maxSmallInlineCallerHintBytes {
			t.Fatal("budget exceeded")
		}
	}
	if !tape.disabled || tape.calls != nil || tape.small != nil {
		t.Fatal("overflow did not release summary")
	}
	tape.finish(0)
	if tape.begin() {
		t.Fatal("disabled summary restarted")
	}
	if _, known := tape.plan(0, inlineTargetTable{}); known {
		t.Fatal("overflow bypassed fallback")
	}
	tape = newSmallInlineCallerHints(100000)
	for i := 0; i < 100000 && !tape.disabled; i++ {
		tape.begin()
		tape.blocked = true
		tape.finish(i)
		if !tape.disabled && tape.smallBytes() > maxSmallInlineCallerHintBytes {
			t.Fatal("range budget exceeded")
		}
	}
	if !tape.disabled {
		t.Fatal("range overflow not reached")
	}
}

func TestSmallInlineCallerSummaryLazyCreation(t *testing.T) {
	for _, body := range [][]byte{{1, 0x0b}, {0x10, 0, 0x0b}, {0x11, 0, 0, 0x0b}, {0x14, 0, 0x0b}} {
		h := newFuncHints(0, 0)
		h.smallInlineCallerPending = true
		elig := newGlobalEligibilityTracker(0)
		var globals shared.GlobalHintAccumulator
		globals.Reset(0)
		got, err := scanBodyBytesIntoMemory64WithModuleCalls(body, 0, 0, 99, h, &elig, false, nil, nil, nil, false, nil, nil, 0, &globals)
		if err != nil {
			t.Fatal(err)
		}
		if body[0] == 1 {
			if got.inlineCaller != nil {
				t.Fatal("call-free body allocated optional summary")
			}
			continue
		}
		if got.inlineCaller == nil || got.inlineCaller.small == nil {
			t.Fatal("first call did not create sparse summary")
		}
		if _, known := got.inlineCaller.plan(0, inlineTargetTable{}); known {
			t.Fatal("unpublished scan escaped")
		}
		got.inlineCaller.finish(0)
		if _, known := got.inlineCaller.plan(0, inlineTargetTable{}); !known {
			t.Fatal("successful caller missing")
		}
	}
}
