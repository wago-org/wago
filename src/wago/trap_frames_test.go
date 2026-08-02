//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestTrapCarriesWasmSourceFrame(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boom", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
	)
	instance, err := Instantiate(MustCompile(module), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	_, err = instance.Invoke("boom")
	var trap *TrapError
	if !errors.As(err, &trap) {
		t.Fatalf("Invoke error = %v, want TrapError", err)
	}
	if len(trap.Frames) != 1 {
		t.Fatalf("trap frames = %#v, want one frame", trap.Frames)
	}
	frame := trap.Frames[0]
	if frame.FunctionIndex != 0 || frame.FunctionName != "boom" || frame.ProgramCounter != 1 || !frame.HasProgramCounter {
		t.Fatalf("trap frame = %#v, want boom func[0] at wasm pc 1", frame)
	}
	if !strings.Contains(err.Error(), "at boom (func[0], wasm pc 0x1)") {
		t.Fatalf("trap error omitted formatted frame: %v", err)
	}
}

func TestTrapFrameTracksInlinedCallee(t *testing.T) {
	previous := false
	for _, knob := range OptKnobs() {
		if knob.Name == "inline" {
			previous = knob.On
		}
	}
	SetOptKnob("inline", true)
	t.Cleanup(func() { SetOptKnob("inline", previous) })

	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call_boom", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
		)),
	)
	instance, err := Instantiate(MustCompile(module), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	_, err = instance.Invoke("call_boom")
	var trap *TrapError
	if !errors.As(err, &trap) || len(trap.Frames) != 1 {
		t.Fatalf("Invoke error = %v, trap %#v", err, trap)
	}
	if frame := trap.Frames[0]; frame.FunctionIndex != 0 || frame.ProgramCounter != 1 || !frame.HasProgramCounter {
		t.Fatalf("inlined trap frame = %#v, want callee func[0] at wasm pc 1", frame)
	}
}

func TestTrapFrameOmitsAmbiguousProgramCounter(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("choose_boom", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x04, 0x40, // if (empty block type)
			0x00,       // unreachable
			0x05,       // else
			0x00,       // unreachable
			0x0b, 0x0b, // end if; end function
		}))),
	)
	instance, err := Instantiate(MustCompile(module), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	_, err = instance.Invoke("choose_boom", I32(1))
	var trap *TrapError
	if !errors.As(err, &trap) || len(trap.Frames) != 1 {
		t.Fatalf("Invoke error = %v, trap %#v", err, trap)
	}
	frame := trap.Frames[0]
	if frame.FunctionIndex != 0 || frame.FunctionName != "choose_boom" || frame.HasProgramCounter {
		t.Fatalf("shared-site trap frame = %#v, want function-only frame", frame)
	}
	if strings.Contains(frame.String(), "wasm pc") {
		t.Fatalf("function-only frame guessed a PC: %s", frame.String())
	}
}
