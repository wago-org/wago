//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func expectGPFailure(t *testing.T, emit func()) {
	t.Helper()
	defer func() {
		failure := recover()
		if failure == nil || !strings.Contains(fmt.Sprint(failure), "immutable GP") {
			t.Fatalf("expected reservation failure, got %v", failure)
		}
	}()
	emit()
}
func preloadedTarget(t *testing.T, reg enc.Reg) *target {
	t.Helper()
	f := &target{}
	f.a.Constant64(reg, 0x123456789abcdef)
	if f.a.Err() != nil || f.Position() == 0 {
		t.Fatal("actual preload missing")
	}
	f.checkImmutable(reg)
	t.Cleanup(f.checkEndLifetimes)
	return f
}

func TestRegallocImmutableGPRejectsTypedWrites(t *testing.T) {
	for name, emit := range map[string]func(*target){
		"ordinary arithmetic":     func(f *target) { f.a.Add(enc.X9, enc.X5, enc.X6) },
		"word arithmetic":         func(f *target) { f.a.Addiw(enc.X9, enc.X9, 0) },
		"partial byte load":       func(f *target) { f.a.Lbu(enc.X9, enc.SP, 0) },
		"word load":               func(f *target) { f.a.Lwu(enc.X9, enc.SP, 0) },
		"full load":               func(f *target) { f.a.Ld(enc.X9, enc.SP, 0) },
		"same-register normalize": func(f *target) { f.NormalizeI32(9) },
		"same-register write":     func(f *target) { f.a.Addi(enc.X9, enc.X9, 0) },
		"direct call":             func(f *target) { f.a.Jal(enc.RA, 4) },
		"alternate link call":     func(f *target) { f.a.Jalr(enc.X5, enc.X6, 0) },
		"long call":               func(f *target) { f.a.JumpLong(true) },
		"write then restore":      func(f *target) { f.a.Constant64(enc.X9, 0); f.a.Constant64(enc.X9, 0x123456789abcdef) },
	} {
		t.Run(name, func(t *testing.T) {
			f := preloadedTarget(t, enc.X9)
			before := f.Position()
			expectGPFailure(t, func() { emit(f) })
			if name != "long call" && f.Position() != before {
				t.Fatal("overwriting instruction emitted before detection")
			}
		})
	}
	for _, reg := range []enc.Reg{enc.SP, enc.RA, enc.X31} {
		f := preloadedTarget(t, reg)
		expectGPFailure(t, func() {
			if reg == enc.X31 {
				f.a.JumpLong(false)
			} else {
				f.a.Addi(reg, reg, 0)
			}
		})
	}
}

func TestRegallocImmutableGPIndependentNestedTransfers(t *testing.T) {
	f := preloadedTarget(t, enc.X9)
	var outer, inner regalloccheck.State
	restoreOuter := f.a.ObserveRegalloc(outer.Apply)
	defer func() { f.a.ObserveRegalloc(restoreOuter) }()
	f.a.Sd(enc.X9, enc.SP, 16) // reservation reads are allowed
	restoreInner := f.a.ObserveRegalloc(inner.Apply)
	expectGPFailure(t, func() { f.a.Lw(enc.X9, enc.SP, 16) })
	f.a.ObserveRegalloc(restoreInner)
	expectGPFailure(t, func() { f.a.Addw(enc.X9, enc.X5, enc.X6) })
	// Transfer state can describe FP facts without reserving the same-numbered GP.
	fp := regalloccheck.Register(regalloccheck.FP, 9)
	v := outer.Seed(fp, 8)
	f.a.Add(enc.X5, enc.X6, enc.X7)
	outer.Expect("separate FP bank", fp, v)
}

func TestRegallocImmutableGPTerminalScopeAndObserverCleanup(t *testing.T) {
	f := &target{}
	observed := 0
	f.a.ObserveGPWrites(func(uint32) { observed++ })
	f.a.Constant64(enc.X9, 42)
	f.checkImmutable(enc.X9)
	before := observed
	f.a.Addi(enc.X5, enc.Zero, 0)
	if observed != before+1 {
		t.Fatal("outer observer lost")
	}
	func() {
		saved := f.checkTerminalGPWrites()
		defer f.checkRestoreGPWrites(saved)
		f.a.Addi(enc.X9, enc.Zero, 0)
	}()
	expectGPFailure(t, func() { f.a.Addi(enc.X9, enc.Zero, 0) })
	func() {
		defer func() {
			if recover() == nil {
				t.Error("terminal panic missing")
			}
		}()
		saved := f.checkTerminalGPWrites()
		defer f.checkRestoreGPWrites(saved)
		panic("terminal")
	}()
	expectGPFailure(t, func() { f.a.Addi(enc.X9, enc.Zero, 0) })
	f.a.Addi(enc.Zero, enc.Zero, 1) // hardwired x0 has no physical destination
	f.checkEndLifetimes()
	f.checkEndLifetimes()
	for _, mode := range []string{"success", "error", "panic", "rethrow"} {
		// Current experimental compiler propagates panics. Simulate both enclosing
		// recovery choices without adding a production debug/recovery policy.
		sentinel := errors.New("attempt failure")
		before = observed
		attempt := func() error {
			defer f.checkEndLifetimes()
			f.a.Constant64(enc.X9, 42)
			f.checkImmutable(enc.X9)
			if mode == "error" {
				return sentinel
			}
			if mode == "panic" || mode == "rethrow" {
				panic(sentinel)
			}
			return nil
		}
		func() {
			defer func() {
				if r := recover(); r != nil && r != sentinel {
					t.Error(r)
				}
			}()
			if mode == "rethrow" {
				func() {
					defer func() {
						if r := recover(); r != nil {
							panic(r)
						}
					}()
					_ = attempt()
				}()
			} else {
				_ = attempt()
			}
		}()
		if f.gpObserverActive || f.immutableGPMask != 0 || f.gpObserverPrevious != nil {
			t.Fatal("abandoned reservation on", mode)
		}
		f.a.Addi(enc.X9, enc.Zero, 0)
		if observed != before+2 {
			t.Fatal("outer observer not restored on", mode, observed, before)
		}
	}
}

func TestRegallocImmutableGPSharedAndFallbackAdmissionControls(t *testing.T) {
	// No production cache admission is implied by these explicit preloads.
	m, err := wasm.DecodeModule(binaryModule(pressureFixture()))
	if err != nil {
		t.Fatal(err)
	}
	fn := &m.Code[0]
	body := fn.BodyBytes
	ft, _ := m.LocalFuncType(0)
	summary := shared.AdmitScalarFunction(fn, ft)
	if !summary.Eligible {
		t.Fatal("shared control not admitted")
	}
	for _, bad := range []bool{false, true} {
		f := preloadedTarget(t, enc.X9)
		f.nlocals = 256
		f.regs = []uint8{5, 6, 7}
		if bad {
			f.regs = []uint8{9}
		}
		var worker shared.ScalarState
		defer worker.FinishWorker()
		compile := func() {
			if _, err := worker.CompileScalar(body, summary, make([]bool, 256), 1, f); err != nil {
				t.Fatal(err)
			}
		}
		if bad {
			expectGPFailure(t, compile)
		} else {
			compile()
		}
	}
	fixture := fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}
	fn = &wasm.Func{BodyBytes: fixture.body}
	ft = &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	for _, reg := range []enc.Reg{enc.X9, enc.X5} {
		f := integerEmitter{}
		f.a.Constant64(reg, 42)
		f.checkImmutable(reg)
		compile := func() {
			if _, err := walkInteger(fn, ft, &moduleState{}, &f); err != nil {
				t.Fatal(err)
			}
		}
		if reg == enc.X5 {
			expectGPFailure(t, compile)
		} else {
			compile()
		}
		f.checkEndLifetimes()
	}
}
