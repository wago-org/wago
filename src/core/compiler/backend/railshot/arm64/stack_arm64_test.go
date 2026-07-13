//go:build (linux || darwin) && arm64

package arm64

import "testing"

// Operand-stack arena sizing, ported from amd64/stack_test.go. The arena-capacity
// heuristics are shared verbatim with amd64 (identical constants and formulas), so
// these guard arm64's copy against drift; the pointer-stability invariant is
// covered by allocation_arm64_test.go.

func TestNewStackArenaDefaultCapacityArm64(t *testing.T) {
	s := newStack()
	if cap(s.arena) != defaultStackArenaCap {
		t.Fatalf("stack arena cap = %d, want %d", cap(s.arena), defaultStackArenaCap)
	}
}

func TestNewStackWithCapClampsArm64(t *testing.T) {
	for _, tc := range []struct {
		hint int
		want int
	}{
		{0, minStackArenaCap},
		{minStackArenaCap - 1, minStackArenaCap},
		{minStackArenaCap + 7, minStackArenaCap + 7},
		{defaultStackArenaCap + 1, defaultStackArenaCap},
	} {
		s := newStackWithCap(tc.hint)
		if cap(s.arena) != tc.want {
			t.Fatalf("newStackWithCap(%d) cap = %d, want %d", tc.hint, cap(s.arena), tc.want)
		}
		if s.head == nil || s.head.next != s.head || s.head.prev != s.head {
			t.Fatalf("newStackWithCap(%d) did not initialize sentinel links", tc.hint)
		}
	}
}

func TestStackArenaCapForBodyTinyFunctionArm64(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(0, 0))
	if cap(s.arena) != minStackArenaCap {
		t.Fatalf("tiny stack arena cap = %d, want %d", cap(s.arena), minStackArenaCap)
	}
}

func TestStackArenaCapForBodyMediumFunctionArm64(t *testing.T) {
	const bodyLen = 64
	const locals = 12
	want := bodyLen + locals/4 + 1
	s := newStackWithCap(stackArenaCapForBody(bodyLen, locals))
	if cap(s.arena) != want {
		t.Fatalf("medium stack arena cap = %d, want %d", cap(s.arena), want)
	}
}

func TestStackArenaCapForBodyLargeFunctionClampArm64(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(1024, 128))
	if cap(s.arena) != defaultStackArenaCap {
		t.Fatalf("large stack arena cap = %d, want clamp %d", cap(s.arena), defaultStackArenaCap)
	}
}

func TestStackArenaCapForHintsIgnoresLongImmediatesArm64(t *testing.T) {
	// A body with a few stack-producing opcodes and long immediates should reserve
	// from the opcode hint, not one arena elem per byte.
	const bodyLen = 64
	const nodes = 12
	want := nodes + nodes/2 + 1
	if got := stackArenaCapForHints(bodyLen, 0, nodes); got != want {
		t.Fatalf("stackArenaCapForHints(%d, 0, %d) = %d, want %d", bodyLen, nodes, got, want)
	}
}

func TestStackReserveForFuncReusesLargestSlabArm64(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	s.reserveForFunc(128)
	if got := cap(s.arena); got != 128 {
		t.Fatalf("reserve cap = %d, want 128", got)
	}
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
	s.reserveForFunc(64)
	if cap(s.arena) != 128 || s.head.next != s.head || s.head.prev != s.head {
		t.Fatalf("smaller reserve did not retain/reset slab: cap=%d", cap(s.arena))
	}
	if first.st.cval != 7 {
		t.Fatalf("prior node changed during reserve: %d", first.st.cval)
	}
}
