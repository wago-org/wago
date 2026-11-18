//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wr "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// These fixtures intentionally disable inlining for positive call selection.
// Inlining precedence is checked separately, rather than counting its fallback
// as successful coverage of the experimental call emitter.
func streamCallGlobalFlags(t *testing.T) {
	t.Helper()
	a, b, c := streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled
	streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = true, true, false
	t.Cleanup(func() { streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = a, b, c })
}
func streamCallGlobalRun(t *testing.T, m *wasm.Module, want uint64, selected []bool, args ...uint64) {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, intr := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			streamingCallGlobalsEnabled = on
			opts := CompileOptions{Interruptible: intr, DeferCodeMapping: true}
			if diagnosticsEnabled {
				opts.Stats = &ModuleStats{}
			}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if got := runCompiledAmd64u(t, cm, args...); got != want {
				t.Fatalf("enabled=%v interruptible=%v got=%x want=%x", on, intr, got, want)
			}
			if on && diagnosticsEnabled && !regallocCheckEnabled {
				if selected[0] && opts.Stats.Funcs[0].Peephole["streaming-direct-call"] == 0 {
					t.Fatal("positive caller never emitted an experimental direct call")
				}
				for i, yes := range selected {
					got := opts.Stats.Funcs[i].Peephole["streaming-function"] != 0
					if got != yes {
						t.Fatalf("function %d selected=%v want=%v: %v", i, got, yes, opts.Stats.Funcs[i].Peephole)
					}
				}
			}
		}
	}
}
func TestStreamCallGlobalEightArgumentsAndSnapshots(t *testing.T) {
	streamCallGlobalFlags(t)
	ps := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	caller := []byte{0, 0x20, 0} // live old local 0 below all eight arguments
	for i := 7; i >= 0; i-- {
		caller = append(caller, 0x20, byte(i))
	}
	caller = append(caller, 0x10, 1, 0x41, 9, 0x21, 0, 0x6a, 0x20, 0, 0x6a, 0x0b)
	// Weight all arguments, including the eighth RDI argument. Reversing the
	// arguments exposes register permutation and late-argument clobber errors.
	callee := []byte{0, 0x20, 0}
	for i := 1; i < 8; i++ {
		callee = append(callee, 0x41, 3, 0x6c, 0x20, byte(i), 0x6a)
	}
	callee = append(callee, 0x0b)
	args := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	v := uint64(8)
	for i := 7; i >= 1; i-- {
		v = v*3 + uint64(i)
	}
	m := modFuncs(t, funcDef{ps, []wasm.ValType{wasm.I32}, caller}, funcDef{ps, []wasm.ValType{wasm.I32}, callee})
	streamCallGlobalRun(t, m, v+10, []bool{true, true}, args...)
}
func TestStreamCallGlobalRecursionAndMixedEngines(t *testing.T) {
	streamCallGlobalFlags(t)
	p := []wasm.ValType{wasm.I32}
	r := []wasm.ValType{wasm.I32}
	t.Run("recursive-sum", func(t *testing.T) {
		m := modFuncs(t, funcDef{p, r, []byte{0, 0x20, 0, 0x10, 1, 0x0b}}, funcDef{p, r, []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, 0, 0x05, 0x20, 0, 0x20, 0, 0x41, 1, 0x6b, 0x10, 1, 0x6a, 0x0b, 0x0b}})
		streamCallGlobalRun(t, m, 210, []bool{true, true}, 20)
	})
	// A dropped f32 constant forces legacy admission without changing the
	// integer ABI. Both call directions must share the real internal ABI.
	fp := []byte{0x43, 0, 0, 0, 0, 0x1a}
	for _, legacyCaller := range []bool{false, true} {
		caller := []byte{0}
		callee := []byte{0}
		if legacyCaller {
			caller = append(caller, fp...)
		} else {
			callee = append(callee, fp...)
		}
		caller = append(caller, 0x20, 0, 0x20, 0, 0x10, 1, 0x6a, 0x0b)
		callee = append(callee, 0x20, 0, 0x41, 3, 0x6c, 0x0b)
		m := modFuncs(t, funcDef{p, r, caller}, funcDef{p, r, callee})
		streamCallGlobalRun(t, m, 28, []bool{!legacyCaller, legacyCaller}, 7)
	}
}
func TestStreamCallGlobalInlinePrecedence(t *testing.T) {
	streamCallGlobalFlags(t)
	inlineEnabled = true
	m := inlineCallFreeModule(t)
	opts := CompileOptions{DeferCodeMapping: true}
	if diagnosticsEnabled {
		opts.Stats = &ModuleStats{}
	}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := runCompiledAmd64u(t, cm, 40); got != 42 {
		t.Fatal(got)
	}
	if diagnosticsEnabled && !regallocCheckEnabled {
		p := opts.Stats.Funcs[0].Peephole
		if p["streaming-function"] != 0 {
			t.Fatalf("selected inline caller was stolen by streaming admission: %v", p)
		}
		if p["all-calls-inlined"] == 0 {
			t.Fatalf("original inline priority was lost: %v", p)
		}
	}
}

func TestStreamCallGlobalCalleeTrapOrder(t *testing.T) {
	streamCallGlobalFlags(t)
	p := []wasm.ValType{wasm.I32, wasm.I32}
	r := []wasm.ValType{wasm.I32}
	m := modFuncs(t, funcDef{p, r, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x1a, 0x00, 0x0b}}, funcDef{p, r, []byte{0, 0x20, 0, 0x20, 1, 0x6d, 0x0b}})
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, intr := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			streamingCallGlobalsEnabled = on
			opts := CompileOptions{Interruptible: intr, DeferCodeMapping: true}
			if diagnosticsEnabled {
				opts.Stats = &ModuleStats{}
			}
			_, _, err := runMemAmd64WithOptions(t, m, opts, nil, 9, 0)
			var trap *wr.TrapError
			if !errors.As(err, &trap) || trap.Code != wr.TrapDivZero {
				t.Fatalf("on=%v intr=%v trap=%v", on, intr, err)
			}
			found := false
			for _, fr := range trap.Frames {
				if fr.FunctionIndex == 1 && fr.HasProgramCounter && fr.ProgramCounter == m.Code[1].LocalDeclBytes+4 {
					found = true
				}
			}
			if !found {
				t.Fatalf("callee division source missing: %+v", trap.Frames)
			}
			if on && diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["streaming-function"] == 0 {
				t.Fatal("trap caller used fallback")
			}
		}
	}
}

func TestStreamCallGlobalMutationAcrossCall(t *testing.T) {
	streamCallGlobalFlags(t)
	oldPins := pinGlobalK
	t.Cleanup(func() { pinGlobalK = oldPins })
	for _, wide := range []bool{false, true} {
		typ, add, constant := wasm.I32, byte(0x6a), byte(0x41)
		if wide {
			typ, add, constant = wasm.I64, 0x7c, 0x42
		}
		// Preserve old global on the operand stack; make a dirty caller write;
		// the callee must see it, replace it, and the caller must reload the new value.
		caller := []byte{0x23, 0, constant, 7, 0x24, 0, 0x20, 0, 0x10, 1, add, 0x23, 0, add, 0x0b}
		callee := []byte{0x23, 0, 0x20, 0, 0x24, 0, 0x0b}
		raw := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(typ, true, []byte{constant, 0, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(callee))))
		m, err := wasm.DecodeModule(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err = wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		for _, pins := range []int{0, 1} {
			pinGlobalK = pins
			for _, on := range []bool{false, true} {
				streamingCallGlobalsEnabled = on
				for _, intr := range []bool{false, true} {
					opts := CompileOptions{Interruptible: intr, DeferCodeMapping: true}
					if diagnosticsEnabled {
						opts.Stats = &ModuleStats{}
					}
					got, cell, err := runStreamGlobalFixture(t, m, opts, 5, 11)
					if err != nil || got != 23 || cell != 11 {
						t.Fatalf("wide=%v pins=%d on=%v intr=%v got=%d cell=%d err=%v", wide, pins, on, intr, got, cell, err)
					}
					if on && diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["streaming-direct-call"] == 0 {
						t.Fatal("joint global caller used fallback")
					}
				}
			}
		}
	}
}

func TestStreamCallGlobalBackedLoop(t *testing.T) {
	streamCallGlobalFlags(t)
	streamResidencyEnable(t)
	a, b, c := streamingResidencyEnabled, streamingWideHomesEnabled, streamingCopyProofEnabled
	streamingResidencyEnabled, streamingWideHomesEnabled, streamingCopyProofEnabled = true, true, true
	t.Cleanup(func() { streamingResidencyEnabled, streamingWideHomesEnabled, streamingCopyProofEnabled = a, b, c })
	// Force ordinary entry allocation rather than the tiny-function entry bank.
	body := []byte{1, 1, 0x7f}
	for i := 0; i < 100; i++ {
		body = append(body, 0x01)
	}
	body = append(body, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x10, 1, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body}, funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x43, 0, 0, 0, 0, 0x1a, 0x20, 0, 0x41, 3, 0x6c, 0x0b}})
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, intr := range []bool{false, true} {
		opts := CompileOptions{Interruptible: intr, DeferCodeMapping: true}
		if diagnosticsEnabled {
			opts.Stats = &ModuleStats{}
		}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if got := runCompiledAmd64u(t, cm, 10); got != 165 {
			t.Fatalf("intr=%v got=%d", intr, got)
		}
		if diagnosticsEnabled && !regallocCheckEnabled {
			p := opts.Stats.Funcs[0].Peephole
			if p["stream-residency-home"] == 0 || p["streaming-direct-call"] == 0 {
				t.Fatalf("backed call loop not activated: %v", p)
			}
		}
	}
}
