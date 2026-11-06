//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package amd64

import (
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
