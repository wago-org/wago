package dragline

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompilerRecordsFunctionFailureReplay(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x1a, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var replay corecompiler.ReplayArtifact
	compiler := Compiler{Replay: func(got corecompiler.ReplayArtifact) error {
		replay = got
		return nil
	}}
	input := corecompiler.Input{
		Module: m, Source: source,
		Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target:  corecompiler.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
	}
	if _, err := compiler.Compile(input); err == nil {
		t.Fatal("unsupported function compiled")
	}
	if replay.Function != 0 || replay.Stage != "lower" || replay.Engine != corecompiler.EngineDragline {
		t.Fatalf("replay identity = function %d stage %q engine %s", replay.Function, replay.Stage, replay.Engine)
	}
	if _, err := corecompiler.MarshalReplay(replay); err != nil {
		t.Fatalf("recorded replay is invalid: %v", err)
	}
}

func TestCompilerPreservesFailureWhenReplaySinkFails(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x1a, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	sinkErr := errors.New("disk full")
	_, err = (Compiler{Replay: func(corecompiler.ReplayArtifact) error { return sinkErr }}).Compile(corecompiler.Input{
		Module: m, Source: source,
		Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target:  corecompiler.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
	})
	if !errors.Is(err, sinkErr) || !strings.Contains(err.Error(), "lower") {
		t.Fatalf("joined compiler/replay error = %v", err)
	}
}
