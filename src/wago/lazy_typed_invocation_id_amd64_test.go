//go:build (linux || darwin) && amd64 && !tinygo

package wago

import (
	"context"
	"errors"
	"testing"
)

func TestLazyTypedInvocationIdentityAndInheritedContext(t *testing.T) {
	if !lazyTypedInvocationIDEnabled || !smallTypedEntryEnabled || !privateNumericLiveRouteEnabled || codeProfileEnabled {
		t.Skip("lazy private typed route disabled")
	}
	if activeHostInvocationBindings.Load() != 0 {
		t.Fatal("unexpected ambient host context")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	var stale instanceHostModule
	var ids []invocationID
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		ids = append(ids, in.currentInvocationID())
		if _, err := in.InvokeFromHost(context.Background(), stale, "run", 1, 0); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expired Caller gained authority: %v", err)
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	p := in.eng.PreparedScalarHost()
	if p == nil || !p.DetachedNumericContext() || !p.IntegerGuestContext() {
		t.Skip("private integer bridge unavailable")
	}
	state := in.ensurePluginState()
	stale = state.hostScope.beginReservedWithID(in, newInvocationID(), nil, nil)
	state.hostScope.end(stale.generation, stale.parentGeneration)
	invoke := func() {
		t.Helper()
		got, err := in.Invoke("run", 1, 0)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("invoke %v %v", got, err)
		}
		if state.invocationID != 0 || in.invocationState.Load() != 0 {
			t.Fatal("entry authority leaked")
		}
	}
	invoke() // populate the public cache through the cold fallback
	invoke()
	if len(ids) != 2 || ids[1] != 0 {
		t.Fatalf("warm plain typed invocation allocated identity: %v", ids)
	}
	outer := hostInvocationContext{id: newInvocationID(), parent: context.WithValue(context.Background(), struct{}{}, "outer")}
	ctrl := offHeapSlicePtr(in.ctrl)
	restore := bindHostInvocationContext(ctrl, outer)
	func() {
		defer restore()
		invoke()
		if ids[2] == 0 || ids[2] == outer.id {
			t.Fatal("inherited invocation did not allocate a fresh identity")
		}
		if value, ok := hostInvocationContexts.Load(ctrl); !ok || value.(hostInvocationContext) != outer {
			t.Fatal("outer context was not restored")
		}
	}()
	invoke()
	if ids[3] != 0 || activeHostInvocationBindings.Load() != 0 {
		t.Fatal("plain invocation retained inherited authority")
	}
}
