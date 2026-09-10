package wago

import (
	"context"
	"math/rand"
	"sync"
	"testing"
)

func TestHostInvocationContextCrossInstanceChain(t *testing.T) {
	t.Run("legacy", func(t *testing.T) { testHostInvocationContextCrossInstanceChain(t, false) })
	t.Run("concrete", func(t *testing.T) { testHostInvocationContextCrossInstanceChain(t, true) })
}

func TestHostLoopActivationContextNesting(t *testing.T) {
	// Model distinct parked control frames without entering native code. Real
	// re-entry, including A -> B -> A, is covered by the chain test above.
	outerFrame, innerFrame := make([]byte, 8), make([]byte, 8)
	root := &Instance{ctrl: outerFrame}
	outer := hostInvocationContext{id: 71, reservation: &pluginOperationReservation{}, parent: context.Background()}
	innerParent, cancel := context.WithCancel(context.Background())
	defer cancel()
	inner := hostInvocationContext{id: 72, reservation: &pluginOperationReservation{}, parent: innerParent}
	restoreOuter := bindHostInvocationContext(offHeapSlicePtr(outerFrame), outer)
	defer restoreOuter()
	restoreInner := bindHostInvocationContext(offHeapSlicePtr(innerFrame), inner)
	defer restoreInner()
	a := hostLoopActivation{root: root, ctrl: offHeapSlicePtr(outerFrame)}
	if got := a.context(root); got != outer {
		t.Fatalf("outer context = %+v, want %+v", got, outer)
	}
	root.ctrl = innerFrame
	b := hostLoopActivation{root: root, ctrl: offHeapSlicePtr(innerFrame)}
	if got := b.context(root); got != inner {
		t.Fatalf("inner context = %+v, want %+v", got, inner)
	}
	if got := a.context(root); got != inner || a.invocation != outer {
		t.Fatalf("changed frame must use lookup without replacing outer snapshot: got %+v, cache %+v", got, a.invocation)
	}
	root.ctrl = outerFrame
	if got := a.context(root); got != outer || b.invocation != inner {
		t.Fatal("nested return changed an activation's identity, reservation or parent")
	}
	// A later public call can reuse the frame, but never the old Go-loop cache.
	next := hostInvocationContext{id: 73, parent: innerParent}
	restoreNext := bindHostInvocationContext(offHeapSlicePtr(outerFrame), next)
	defer restoreNext()
	c := hostLoopActivation{root: root, ctrl: offHeapSlicePtr(outerFrame)}
	if got := c.context(root); got != next || a.invocation != outer {
		t.Fatal("later invocation reused the previous loop's snapshot")
	}
}

func TestHostLoopActivationDoesNotCacheCalleeAsRoot(t *testing.T) {
	root := &Instance{ctrl: make([]byte, 8)}
	callee := &Instance{ctrl: make([]byte, 8)}
	child := hostInvocationContext{id: 81}
	restoreChild := bindHostInvocationContext(offHeapSlicePtr(callee.ctrl), child)
	defer restoreChild()
	a := hostLoopActivation{root: root, ctrl: offHeapSlicePtr(root.ctrl)}
	if got := a.context(callee); got != child || !a.invocation.empty() {
		t.Fatal("callee fallback became cached root authority")
	}
	parent := hostInvocationContext{id: 82, reservation: &pluginOperationReservation{}}
	restoreParent := bindHostInvocationContext(offHeapSlicePtr(root.ctrl), parent)
	defer restoreParent()
	if got := a.context(callee); got != parent || a.invocation != parent {
		t.Fatal("runtime root identity did not replace uncached fallback")
	}
}

func testHostInvocationContextCrossInstanceChain(t *testing.T, concrete bool) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	var a, b *Instance
	var rootID invocationID
	var outer instanceHostModule
	calls := 0
	host := func(owner **Instance, next **Instance) HostFunc {
		return func(mod HostModule, p, r []uint64) {
			h, _ := resolveHostCaller(mod)
			calls++
			if rootID == 0 {
				rootID = h.invocationID
				outer = h
			}
			if h.in != *owner || h.invocationID != rootID || !isNativeActive(*owner, rootID) {
				t.Error("callback changed callee or invocation ownership")
			}
			if h.scope != &(*owner).pluginState.Load().hostScope {
				t.Error("dispatcher cached another instance's callback scope")
			}
			if p[0] == 0 {
				if outer.valid() {
					t.Error("outer A capability active during inner A callback")
				}
				r[0] = 10
				return
			}
			got, err := (*next).InvokeFromHost(context.Background(), mod, "g", p[0]-1)
			if err != nil {
				panic(err)
			}
			if !h.valid() {
				t.Error("outer generation not restored")
			}
			r[0] = got[0] + 1
		}
	}
	var err error
	a, err = Instantiate(c, Imports{"env.f": callerTestCallback(concrete, host(&a, &b))})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err = Instantiate(c, Imports{"env.f": callerTestCallback(concrete, host(&b, &a))})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, err := a.Invoke("g", I32(2))
	if err != nil || len(got) != 1 || got[0] != 12 || calls != 3 {
		t.Fatalf("chain = %v, %v, calls %d", got, err, calls)
	}
	if outer.valid() || isNativeActive(a, rootID) || isNativeActive(b, rootID) {
		t.Fatal("chain retained callback authority")
	}
}

func TestInstanceActivationsMatchCountedIdentity(t *testing.T) {
	var instances [2]Instance
	var counts [2][9]int
	rng := rand.New(rand.NewSource(42))
	for step := 0; step < 10000; step++ {
		i, id := rng.Intn(2), rng.Intn(9)
		if rng.Intn(2) == 0 {
			markNativeActiveID(&instances[i], invocationID(id))
			counts[i][id]++
		} else {
			unmarkNativeActiveID(&instances[i], invocationID(id))
			if counts[i][id] != 0 {
				counts[i][id]--
			}
		}
		for j := range instances {
			for k, count := range counts[j] {
				if got := isNativeActive(&instances[j], invocationID(k)); got != (k != 0 && count != 0) {
					t.Fatalf("step %d instance %d identity %d: active=%v count=%d", step, j, k, got, count)
				}
			}
		}
	}
	for i := range instances {
		for id, count := range counts[i] {
			for n := 0; n < count; n++ {
				unmarkNativeActiveID(&instances[i], invocationID(id))
			}
		}
		a := &instances[i].pluginState.Load().activations
		if a.count != 0 || a.other != nil {
			t.Fatal("activation state retained after unwind")
		}
	}
}

func TestInstanceActivationsConcurrentIndependent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var in Instance
			for n := 0; n < 1000; n++ {
				id := newInvocationID()
				markNativeActiveID(&in, id)
				markNativeActiveID(&in, id)
				unmarkNativeActiveID(&in, id)
				if !isNativeActive(&in, id) {
					t.Error("nested activation lost")
				}
				unmarkNativeActiveID(&in, id)
				if isNativeActive(&in, id) {
					t.Error("stale activation accepted")
				}
			}
		}()
	}
	wg.Wait()
}

func TestInstanceReservationScope(t *testing.T) {
	var a, b Instance
	a.ensurePluginState().invocationID = 7
	b.ensurePluginState().invocationID = 7
	r := &pluginOperationReservation{}
	if a.swapInvocationReservation(r) != nil || currentInvocationReservation(&b) != nil {
		t.Fatal("reservation crossed instance")
	}
	a.pluginState.Load().invocationID = 8
	if currentInvocationReservation(&a) != nil {
		t.Fatal("reservation crossed identity")
	}
	a.pluginState.Load().invocationID = 7
	if a.swapInvocationReservation(nil) != r || a.pluginState.Load().activations.reservations != nil || a.pluginState.Load().activations.reservation != nil {
		t.Fatal("reservation not released")
	}
}

func TestInstanceReservationNestedIdentities(t *testing.T) {
	var in Instance
	state := in.ensurePluginState()
	a, b := &pluginOperationReservation{}, &pluginOperationReservation{}
	state.invocationID = 1
	in.swapInvocationReservation(a)
	state.invocationID = 2
	in.swapInvocationReservation(b)
	state.invocationID = 1
	if in.swapInvocationReservation(nil) != a {
		t.Fatal("outer reservation lost")
	}
	state.invocationID = 2
	if currentInvocationReservation(&in) != b || in.swapInvocationReservation(a) != b || in.swapInvocationReservation(nil) != a {
		t.Fatal("nested reservation lost")
	}
	if state.activations.reservation != nil || state.activations.reservations != nil {
		t.Fatal("reservation retained after unwind")
	}
}

func BenchmarkHostInvocationReservation(b *testing.B) {
	var in Instance
	in.ensurePluginState().invocationID = 1
	reservation := &pluginOperationReservation{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		previous := in.swapInvocationReservation(reservation)
		if currentInvocationReservation(&in) != reservation {
			b.Fatal("reservation lost")
		}
		in.swapInvocationReservation(previous)
	}
}
