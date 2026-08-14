package shared

import "testing"

func TestPropagateFuncEffects(t *testing.T) {
	// 0 -> 1 -> 2 -> 1 is a recursive SCC; 3 is independent.
	direct := []FuncEffects{0, 0, EffectGrowsMemory, EffectWritesGlobals}
	starts := []uint32{0, 1, 2, 3, 3}
	calls := []uint32{1, 2, 1}
	got := PropagateFuncEffects(direct, starts, calls)
	want := []FuncEffects{EffectGrowsMemory, EffectGrowsMemory, EffectGrowsMemory, EffectWritesGlobals}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("effects[%d] = %02b, want %02b", i, got[i], want[i])
		}
	}
}

func TestPropagateFuncEffectsMalformedGraphIsConservative(t *testing.T) {
	if got := PropagateFuncEffects([]FuncEffects{0}, []uint32{0, 1}, []uint32{4}); got[0] != AllFuncEffects {
		t.Fatalf("out-of-range callee effects = %02b, want all", got[0])
	}
	if got := PropagateFuncEffects([]FuncEffects{0}, []uint32{1, 0}, nil); got[0] != AllFuncEffects {
		t.Fatalf("malformed range effects = %02b, want all", got[0])
	}
}

func TestFuncEffectCollectorBoundedFallback(t *testing.T) {
	large := NewFuncEffectCollector(3, 0, 0, 2, 4)
	large.Begin(0)
	large.Call(0, 1)
	if got := large.Finish()[0]; got != AllFuncEffects {
		t.Fatalf("large graph caller effects = %02b, want all", got)
	}

	overflow := NewFuncEffectCollector(2, 0, 2, 2, 2)
	overflow.Begin(0)
	for range 3 {
		overflow.Call(0, 1)
	}
	if got := overflow.Finish()[0]; got != AllFuncEffects {
		t.Fatalf("edge-cap caller effects = %02b, want all", got)
	}
}
