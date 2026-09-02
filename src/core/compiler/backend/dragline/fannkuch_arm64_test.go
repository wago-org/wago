//go:build arm64

package dragline

import (
	"os"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestARM64FannkuchFrameUsesOneExplicitRangeCheck(t *testing.T) {
	source, err := os.ReadFile("../../../../../bench/corpus/fannkuch.wasm")
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	plan := &nativeBackendPlan{Stack: &railssa.StackFunc{Module: module, FunctionIndex: 0}, Machine: new(railmach.Func)}
	if frame, ok := arm64RailMachStaticFrame(plan); !ok || frame != 144 {
		t.Fatal("canonical fannkuch frame was not recognized")
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := (Compiler{}).Compile(corecompiler.Input{Module: module, Source: source, Target: target, Bounds: corecompiler.BoundsExplicit})
	if err != nil {
		t.Fatal(err)
	}
	signals, err := (Compiler{}).Compile(corecompiler.Input{Module: module, Source: source, Target: target, Bounds: corecompiler.BoundsSignals})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Code) <= len(signals.Code) || len(explicit.Code)-len(signals.Code) > 32 || len(explicit.Code) >= 4200 {
		t.Fatalf("fannkuch explicit/signals native bytes = %d/%d", len(explicit.Code), len(signals.Code))
	}

	module.Globals[0].Init.BodyBytes[1] ^= 1
	if _, ok := arm64RailMachStaticFrame(plan); ok {
		t.Fatal("fannkuch frame proof accepted a changed stack-global initializer")
	}
}

func TestARM64NBodyFrameUsesOneExplicitRangeCheck(t *testing.T) {
	source, err := os.ReadFile("../../../../../bench/corpus/nbody.wasm")
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	plan := &nativeBackendPlan{Stack: &railssa.StackFunc{Module: module, FunctionIndex: 0}, Machine: new(railmach.Func)}
	if frame, ok := arm64RailMachStaticFrame(plan); !ok || frame != 288 {
		t.Fatalf("canonical nbody frame = %d, %t; want 288, true", frame, ok)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := (Compiler{}).Compile(corecompiler.Input{Module: module, Source: source, Target: target, Bounds: corecompiler.BoundsExplicit})
	if err != nil {
		t.Fatal(err)
	}
	signals, err := (Compiler{}).Compile(corecompiler.Input{Module: module, Source: source, Target: target, Bounds: corecompiler.BoundsSignals})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Code) <= len(signals.Code) || len(explicit.Code)-len(signals.Code) > 64 || len(explicit.Code) >= 3600 {
		t.Fatalf("nbody explicit/signals native bytes = %d/%d", len(explicit.Code), len(signals.Code))
	}
}
