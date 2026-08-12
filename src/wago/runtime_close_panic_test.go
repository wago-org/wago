package wago

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeCloseSanitizesPanicsAndReleasesManagedResources(t *testing.T) {
	secret := strings.Repeat("secret-token-", 1024)
	stopPanic := errors.New(secret + "stop")
	internalPanic := errors.New(secret + "internal")
	hookPanic := errors.New(secret + "hook")
	var events []string
	rt := NewRuntime()
	manager := newPendingInstanceManager("test.manager", CapabilityBudget{})
	manager.activate(rt)
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(funcrefImportedRefFuncModule())
	if err != nil {
		t.Fatal(err)
	}
	managed, err := manager.Instantiate(context.Background(), mod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := managed.Instance().Call(context.Background(), "get")
	if err != nil || len(result) != 1 || result[0].FuncRef().IsNull() {
		t.Fatalf("get retained token = %v, %v", result, err)
	}
	rt.refStore.mu.Lock()
	retainedTokens := len(rt.refStore.byToken)
	rt.refStore.mu.Unlock()
	if retainedTokens == 0 {
		t.Fatal("test setup did not retain a funcref token")
	}
	rt.pluginStops = []registeredPluginStop{
		{name: "continues", stop: func(context.Context) error { events = append(events, "stop:continues"); return nil }},
		{name: "panics", stop: func(context.Context) error { events = append(events, "stop:panics"); panic(stopPanic) }},
	}
	rt.hooks.internalClose = []func() error{
		manager.close,
		func() error { events = append(events, "internal:continues"); return nil },
		func() error { events = append(events, "internal:panics"); panic(internalPanic) },
	}
	rt.hooks.onRuntimeClose = []func(*RuntimeContext){
		func(*RuntimeContext) { events = append(events, "hook:continues") },
		func(*RuntimeContext) { events = append(events, "hook:panics"); panic(hookPanic) },
	}

	err = rt.Close()
	if !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("Close error = %v, want ErrCallbackPanic", err)
	}
	for _, panicErr := range []error{stopPanic, internalPanic, hookPanic} {
		if errors.Is(err, panicErr) || strings.Contains(err.Error(), panicErr.Error()) {
			t.Fatalf("Close error retained secret panic value: %v", err)
		}
	}
	if len(err.Error()) > 512 {
		t.Fatalf("Close panic report is unbounded: %d bytes", len(err.Error()))
	}
	wantEvents := []string{
		"stop:panics", "stop:continues",
		"internal:panics", "internal:continues",
		"hook:panics", "hook:continues",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("close events = %v, want %v", events, wantEvents)
	}
	if managed.Instance() != nil {
		t.Fatal("managed instance remained open after callback panics")
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("host token owner remained retained after Close: %v", err)
	}
	rt.refStore.mu.Lock()
	storeClosed := rt.refStore.runtimeClosed
	liveInstances := rt.refStore.liveInstances
	instances := len(rt.refStore.instances)
	funcrefTokens := len(rt.refStore.byToken)
	gcTokens := len(rt.refStore.gcByToken)
	rt.refStore.mu.Unlock()
	if !storeClosed || liveInstances != 0 || instances != 0 || funcrefTokens != 0 || gcTokens != 0 {
		t.Fatalf("reference resources after Close: closed=%v live=%d instances=%d funcrefs=%d gc=%d",
			storeClosed, liveInstances, instances, funcrefTokens, gcTokens)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("idempotent Close reran callbacks: %v", events)
	}
}
