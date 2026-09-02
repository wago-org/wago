//go:build (linux || darwin) && arm64

package arm64

import (
	"slices"
	"testing"
)

// Operand-stack arena sizing, ported from amd64/stack_test.go. The arena-capacity
// heuristics are shared verbatim with amd64 (identical constants and formulas), so
// these guard arm64's copy against drift; the pointer-stability invariant is
// covered by allocation_arm64_test.go.

func TestNewStackArenaDefaultCapacityArm64(t *testing.T) {
	s := newStack()
	if cap(s.chunks[0]) != defaultStackArenaCap {
		t.Fatalf("stack arena cap = %d, want %d", cap(s.chunks[0]), defaultStackArenaCap)
	}
}

func TestNewStackWithCapSizesFirstChunkArm64(t *testing.T) {
	for _, tc := range []struct {
		hint int
		want int
	}{
		{0, minStackArenaCap},
		{minStackArenaCap - 1, minStackArenaCap},
		{minStackArenaCap + 7, minStackArenaCap + 7},
		{defaultStackArenaCap + 1, defaultStackArenaCap + 1},
	} {
		s := newStackWithCap(tc.hint)
		if cap(s.chunks[0]) != tc.want {
			t.Fatalf("newStackWithCap(%d) cap = %d, want %d", tc.hint, cap(s.chunks[0]), tc.want)
		}
		if s.head == nil || s.head.next != s.head || s.head.prev != s.head {
			t.Fatalf("newStackWithCap(%d) did not initialize sentinel links", tc.hint)
		}
	}
}

func TestHintedStackGrowthMatchesLegacyRetentionArm64(t *testing.T) {
	const (
		firstCap = 386
		nodes    = 31_000
	)
	hinted, legacy := newStackWithCap(firstCap), newStack()
	for i := 1; i < nodes; i++ { // each stack already contains its sentinel
		hinted.alloc()
		legacy.alloc()
	}
	if len(hinted.chunks) < 2 || cap(hinted.chunks[1]) != 768-firstCap {
		t.Fatalf("hinted fallback chunks = %v, want second cap %d", stackChunkCapsArm64(hinted), 768-firstCap)
	}
	if got, want := retainedStackArenaCapacityArm64(hinted), retainedStackArenaCapacityArm64(legacy); got != want {
		t.Fatalf("hinted retained capacity = %d, want legacy %d; hinted=%v legacy=%v", got, want, stackChunkCapsArm64(hinted), stackChunkCapsArm64(legacy))
	}
}

func TestSubDefaultHintPreservesGeometricGrowthArm64(t *testing.T) {
	const nodes = 2_000
	s := newStackWithCap(101)
	for i := 1; i < nodes; i++ {
		s.alloc()
	}
	want := []int{101, 202, 404, 808, 1616}
	if got := stackChunkCapsArm64(s); !slices.Equal(got, want) {
		t.Fatalf("sub-default growth = %v, want %v", got, want)
	}
}

func retainedStackArenaCapacityArm64(s *stack) int {
	total := 0
	for i := range s.chunks {
		total += cap(s.chunks[i])
	}
	return total
}

func stackChunkCapsArm64(s *stack) []int {
	caps := make([]int, len(s.chunks))
	for i := range s.chunks {
		caps[i] = cap(s.chunks[i])
	}
	return caps
}

func TestStackArenaCapForBodyTinyFunctionArm64(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(0, 0))
	if cap(s.chunks[0]) != minStackArenaCap {
		t.Fatalf("tiny stack arena cap = %d, want %d", cap(s.chunks[0]), minStackArenaCap)
	}
}

func TestStackArenaCapForBodyMediumFunctionArm64(t *testing.T) {
	const bodyLen = 64
	const locals = 12
	want := bodyLen + locals/4 + 1
	s := newStackWithCap(stackArenaCapForBody(bodyLen, locals))
	if cap(s.chunks[0]) != want {
		t.Fatalf("medium stack arena cap = %d, want %d", cap(s.chunks[0]), want)
	}
}

func TestStackArenaCapForBodyLargeFunctionArm64(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(1024, 128))
	want := stackArenaCapForBody(1024, 128)
	if cap(s.chunks[0]) != want {
		t.Fatalf("large stack arena cap = %d, want %d", cap(s.chunks[0]), want)
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
