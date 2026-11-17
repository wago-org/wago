//go:build linux && amd64 && ((!tinygo && !wago_target_tinygo) || wago_tinygo_mapped_operands)

package amd64

import (
	coreplugins "github.com/wago-org/wago/src/core/plugins"
	"runtime"
	"testing"
)

func TestMappedOperandArenaLinksSurviveGCAndHeapOverflow(t *testing.T) {
	a := newOperandArena()
	if a == nil {
		t.Fatal("mapped arena unavailable")
	}
	defer a.Close()
	s := &stack{arena: a}
	s.initWithCap(minStackArenaCap)
	const count = 25000 // exceeds the fixed mapped arena; later chunks use heap
	for i := 0; i < count; i++ {
		s.pushIntegerConstant(mtI64, int64(i))
	}
	runtime.GC()
	for i := count - 1; i >= 0; i-- {
		e := s.back()
		if e == nil || e.st.cval != int64(i) {
			t.Fatalf("node %d lost across collection or arena overflow", i)
		}
		s.erase(e)
	}
	if s.back() != nil {
		t.Fatal("operand stack not empty")
	}
	s.reset()
	s.pushIntegerConstant(mtI32, 42)
	runtime.GC()
	if got := s.back().st.cval; got != 42 {
		t.Fatalf("reset value = %d", got)
	}
	runtime.KeepAlive(s)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if len(a.Mapping()) != 0 {
		t.Fatal("arena did not release mapping")
	}
}

func TestMappedOperandArenaColdRootsAcrossOverflow(t *testing.T) {
	a := newOperandArena()
	if a == nil {
		t.Fatal("mapped arena unavailable")
	}
	defer a.Close()
	s := &stack{arena: a}
	s.initWithCap(minStackArenaCap)
	s.pushIntegerConstant(mtI64, 123)
	root := s.back()
	func() {
		typ, err := coreplugins.PrepareCustomType(coreplugins.CustomTypeSpec{Name: "mapped-cold-root", Size: 16, Carrier: coreplugins.WasmV128})
		if err != nil {
			t.Fatal(err)
		}
		s.setElemCold(root, &typ, []Reg{R8, R9, R10})
	}()
	for i := 0; i < 25000; i++ {
		s.pushIntegerConstant(mtI64, int64(i))
	}
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	cold := s.elemCold(root)
	if cold == nil || cold.custom.Name() != "mapped-cold-root" || len(cold.vregs) != 3 || cold.vregs[2] != R10 {
		t.Fatal("managed cold root lost")
	}
	for i := 24999; i >= 0; i-- {
		e := s.back()
		if e.st.cval != int64(i) {
			t.Fatal("mapped/heap link lost", i)
		}
		s.erase(e)
	}
	if s.back() != root {
		t.Fatal("mapped root lost across heap links")
	}
	runtime.KeepAlive(s)
}

func TestMappedOperandArenaClosedOwnerFallsBack(t *testing.T) {
	a := newOperandArena()
	if a == nil {
		t.Fatal("mapped arena unavailable")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	chunk := operandArenaChunk(a, 8)
	chunk = append(chunk, elem{st: storage{cval: 42}})
	runtime.GC()
	if chunk[0].st.cval != 42 || len(a.Mapping()) != 0 {
		t.Fatal("closed mapping fallback")
	}
	if err := a.Close(); err != nil {
		t.Fatal("idempotent close", err)
	}
}

func TestMappedOperandArenaIndependentDeferredOwner(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			var owner *stack
			func() {
				defer func() {
					if v := recover(); v != nil && (mode != "panic" || v != "cleanup panic") {
						panic(v)
					}
				}()
				sc := newCompileScratch(minStackArenaCap)
				owner = sc.stack
				// Same independent defer as runWorker: capture the owner before lazy
				// allocation, and do not consult scratch after other teardown executes.
				defer closeOperandArenaOwner(sc.stack)
				defer func() {
					if mode == "panic" {
						panic("cleanup panic")
					}
					sc.finishControlWorker()
					sc.finishStackWorker()
					sc = nil
				}()
				sc.mappedOperands = true
				sc.ensureTargetStack()
				if len(owner.arena.Mapping()) == 0 {
					t.Fatal("lazy mapped arena unavailable")
				}
				if mode == "error" {
					return
				}
			}()
			if owner == nil || len(owner.arena.Mapping()) != 0 {
				t.Fatal("deferred mapping leak", mode)
			}
		})
	}
}
