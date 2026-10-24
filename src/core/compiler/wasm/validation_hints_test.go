package wasm_test

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type validationHintRecorder struct {
	begins, ends, events int
	complete             bool
	opcodes              []byte
	values               []uint64
	details              []validationHintEvent
}

func (r *validationHintRecorder) BeginFunction(int) wasm.ValidationHintInstructionObserver {
	r.begins++
	return r
}
func (r *validationHintRecorder) EndFunction(int) { r.ends++ }
func (r *validationHintRecorder) Complete() bool  { return r.complete }

type validationHintEvent struct {
	op            byte
	kind          wasm.InstrKind
	index, index2 uint32
	bits          uint64
	meta          uint8
	start, end    int
}

func (r *validationHintRecorder) Observe(op byte, kind wasm.InstrKind, index, index2 uint32, bits uint64, meta uint8, start, end int) {
	r.events++
	r.opcodes = append(r.opcodes, op)
	r.values = append(r.values, bits)
	r.details = append(r.details, validationHintEvent{op, kind, index, index2, bits, meta, start, end})
}

func TestValidationHintObserverFallbackAndFailure(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, complete := range []bool{false, true} {
			m := validationAnalysisModule(t)
			r := &validationHintRecorder{complete: complete}
			var a wasm.ValidatedModuleAnalysis
			factories := 0
			factory := func(*wasm.Module) wasm.ValidationHintObserver { factories++; return r }
			if err := wasm.ValidateModuleWithHintObserver(m, wasm.ValidationFeatures{}, workers, wasm.ValidationLimits{}, &a, factory); err != nil {
				t.Fatal(err)
			}
			if !a.ValidFor(m) {
				t.Fatal("observer decline discarded valid analysis")
			}
			if workers == 2 {
				if factories != 0 || a.HintObserver() != nil {
					t.Fatal("parallel validation used serial observer")
				}
			} else {
				if r.begins != 2 || r.ends != 2 || r.events == 0 {
					t.Fatalf("incomplete callback sequence: %#v", r)
				}
				if (a.HintObserver() != nil) != complete {
					t.Fatal("observer completion was ignored")
				}
			}
		}
	}

	m := validationAnalysisModule(t)
	m.Code[0].BodyBytes = []byte{0x6a, 0x0b} // invalid operand stack
	r := &validationHintRecorder{complete: true}
	var a wasm.ValidatedModuleAnalysis
	factory := func(*wasm.Module) wasm.ValidationHintObserver { return r }
	if err := wasm.ValidateModuleWithHintObserver(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a, factory); err == nil {
		t.Fatal("observer bypassed validation failure")
	}
	if r.events != 0 || r.ends != 0 || !reflect.DeepEqual(a, wasm.ValidatedModuleAnalysis{}) {
		t.Fatal("failed instruction published hints or left analysis valid")
	}

	m = validationAnalysisModule(t)
	m.FuncTypes[0].Index = ^uint32(0) // declaration validation must precede the factory
	called := false
	factory = func(*wasm.Module) wasm.ValidationHintObserver { called = true; return r }
	if err := wasm.ValidateModuleWithHintObserver(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a, factory); err == nil || called {
		t.Fatal("factory ran before declaration validation")
	}
}

func TestValidationHintScalarDecodeMatchesOrdinaryValidation(t *testing.T) {
	cases := [][]byte{
		{0x20, 0, 0x21, 1, 0x20, 1, 0x22, 0, 0x1a, 0x0b},
		{0x41, 0x7f, 0x21, 0, 0x20, 0, 0x1a, 0x0b},
		{0x42, 0x7f, 0x1a, 0x43, 0, 0, 0xc0, 0x7f, 0x1a, 0x44, 0, 0, 0, 0, 0, 0, 0xf8, 0x7f, 0x1a, 0x0b},
		{0x20, 2, 0x0b},          // out of range
		{0x21, 0, 0x0b},          // underflow
		{0x42, 0, 0x22, 0, 0x0b}, // wrong local type
		{0x20, 0x80},             // truncated index
		{0x41, 0x80},             // truncated constant
		{0x41, 0xff, 0xff, 0xff, 0xff, 0x7f, 0x1a, 0x0b}, // invalid signed width
		{0x43, 0, 0},                         // truncated float
		{0x00, 0x21, 0, 0x20, 1, 0x1a, 0x0b}, // polymorphic stack
	}
	for i, body := range cases {
		m := validationAnalysisModule(t)
		m.Code[0].BodyBytes = body
		m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 2, Type: wasm.I32}}
		m.Code[1].BodyBytes = []byte{0x0b}
		var ordinary, observed wasm.ValidatedModuleAnalysis
		want := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &ordinary)
		r := &validationHintRecorder{complete: true}
		got := wasm.ValidateModuleWithHintObserver(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &observed, func(*wasm.Module) wasm.ValidationHintObserver { return r })
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d error differs: got %v want %v", i, got, want)
		}
		if want == nil {
			if ordinary.Flags() != observed.Flags() || ordinary.FuncCount() != observed.FuncCount() {
				t.Fatalf("case %d module facts differ", i)
			}
			for n := 0; n < ordinary.FuncCount(); n++ {
				if ordinary.Func(n) != observed.Func(n) {
					t.Fatalf("case %d function %d facts differ", i, n)
				}
			}
			for n, op := range r.opcodes {
				if (op == 0x41 || op == 0x42) && i >= 1 && i <= 2 && r.values[n] != ^uint64(0) {
					t.Fatal("signed constant was not sign extended")
				}
			}
		} else if observed.ValidFor(m) || observed.HintObserver() != nil {
			t.Fatal("invalid scalar body retained analysis")
		}
	}
}

func TestValidationHintDirectMemoryAndControlParity(t *testing.T) {
	cases := [][]byte{
		{0x41, 0, 0x28, 2, 0, 0x1a, 0x0b},
		{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x0b},
		{0x41, 0, 0x28, 0x42, 0, 0, 0x1a, 0x0b}, // explicit memory index
		{0x41, 0, 0x28, 3, 0, 0x1a, 0x0b},       // invalid alignment
		{0x41, 0, 0x28, 2, 0x80},                // truncated offset
		{0x41, 0, 0x28, 0x42, 0x80},             // truncated memory index
		{0x41, 0, 0x28, 0x42, 2, 0, 0x1a, 0x0b}, // unknown memory
		{0x42, 0, 0x28, 2, 0, 0x1a, 0x0b},       // wrong address type
		{0x41, 0, 0x42, 1, 0x36, 2, 0, 0x0b},    // wrong store type
		{0x00, 0x28, 2, 0, 0x1a, 0x0b},          // polymorphic stack
		{0x10, 1, 0x0b},
		{0x12, 1, 0x0b},
		{0x10, 2, 0x0b}, // unknown function
		{0x10, 0x80},    // truncated call index
		{0x02, 0x40, 0x0c, 0, 0x0b, 0x0b},
		{0x02, 0x40, 0x41, 0, 0x0d, 0, 0x0b, 0x0b},
		{0x0c, 2, 0x0b}, // unknown label
		{0x0d, 0x80},    // truncated branch index
		{0x41, 1, 0x04, 0x40, 0x05, 0x0b, 0x0b},
		{0x05, 0x0b}, // else outside if
		{0x0b, 0x0b}, // bytes after function end
	}
	for i, body := range cases {
		for _, multi := range []bool{false, true} {
			m := validationAnalysisModule(t)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			m.Code[0].BodyBytes, m.Code[1].BodyBytes = body, []byte{0x0b}
			features := wasm.ValidationFeatures{MultiMemory: multi}
			var ordinary, observed wasm.ValidatedModuleAnalysis
			want := wasm.ValidateModuleWithAnalysis(m, features, 1, wasm.ValidationLimits{}, &ordinary)
			r := &validationHintRecorder{complete: true}
			got := wasm.ValidateModuleWithHintObserver(m, features, 1, wasm.ValidationLimits{}, &observed, func(*wasm.Module) wasm.ValidationHintObserver { return r })
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("case %d multi=%v: got %v want %v", i, multi, got, want)
			}
			if want == nil {
				if ordinary.Flags() != observed.Flags() || ordinary.Func(0) != observed.Func(0) || ordinary.Func(1) != observed.Func(1) {
					t.Fatalf("case %d multi=%v: facts differ", i, multi)
				}
			} else if observed.ValidFor(m) || observed.HintObserver() != nil {
				t.Fatalf("case %d retained failed analysis", i)
			}
		}
	}
}

func TestValidationHintMixedMemoryWidthPayload(t *testing.T) {
	m := validationAnalysisModule(t)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1, Addr64: true}}}
	m.Code[0].BodyBytes = []byte{0x42, 0, 0x28, 0x42, 1, 0x80, 0x80, 0x80, 0x80, 0x10, 0x1a, 0x0b}
	m.Code[1].BodyBytes = []byte{0x0b}
	r := &validationHintRecorder{complete: true}
	var analysis wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithHintObserver(m, wasm.ValidationFeatures{MultiMemory: true}, 1, wasm.ValidationLimits{}, &analysis, func(*wasm.Module) wasm.ValidationHintObserver { return r }); err != nil {
		t.Fatal(err)
	}
	want := validationHintEvent{op: 0x28, kind: wasm.InstrI32Load, index: 1, bits: 1 << 32, meta: wasm.ValidationHintTouchesMemory | wasm.ValidationHintHasMemoryIndex, start: 2, end: 10}
	if len(r.details) < 2 || r.details[1] != want {
		t.Fatalf("memory event = %#v, want %#v", r.details, want)
	}
}
