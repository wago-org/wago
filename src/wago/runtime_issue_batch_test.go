package wago

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func TestRuntimeCompileRejectsClosedRuntimeBeforeHooks(t *testing.T) {
	rt := NewRuntime()
	var beforeCalls int
	rt.hooks.BeforeCompile(func(*CompileContext, []byte) ([]byte, error) {
		beforeCalls++
		return nil, nil
	})
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Compile(wasmtest.Module()); err == nil || !strings.Contains(err.Error(), "closed runtime") {
		t.Fatalf("Compile after Close = %v, want closed-runtime error", err)
	}
	if beforeCalls != 0 {
		t.Fatalf("BeforeCompile ran %d times after close", beforeCalls)
	}
}

func TestRuntimeCompileAdmittedBeforeCloseMayComplete(t *testing.T) {
	rt := NewRuntime()
	entered := make(chan struct{})
	release := make(chan struct{})
	rt.hooks.BeforeCompile(func(*CompileContext, []byte) ([]byte, error) {
		close(entered)
		<-release
		return nil, nil
	})
	compileDone := make(chan error, 1)
	go func() {
		_, err := rt.Compile(wasmtest.Module())
		compileDone <- err
	}()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-compileDone; err != nil {
		t.Fatalf("compile admitted before Close = %v", err)
	}
}

func TestRuntimeCloseIsolatesCallbackPanicsAndClosesStore(t *testing.T) {
	stopPanic := errors.New("stop panic")
	internalPanic := errors.New("internal panic")
	hookPanic := errors.New("hook panic")
	var events []string
	rt := NewRuntime()
	rt.pluginStops = []registeredPluginStop{
		{name: "continues", stop: func(context.Context) error { events = append(events, "stop:continues"); return nil }},
		{name: "panics", stop: func(context.Context) error { events = append(events, "stop:panics"); panic(stopPanic) }},
	}
	rt.hooks.internalClose = []func() error{
		func() error { events = append(events, "internal:continues"); return nil },
		func() error { events = append(events, "internal:panics"); panic(internalPanic) },
	}
	rt.hooks.onRuntimeClose = []func(*RuntimeContext){
		func(*RuntimeContext) { events = append(events, "hook:continues") },
		func(*RuntimeContext) { events = append(events, "hook:panics"); panic(hookPanic) },
	}

	err := rt.Close()
	for _, want := range []error{stopPanic, internalPanic, hookPanic} {
		if !errors.Is(err, want) {
			t.Fatalf("Close error = %v, want wrapped %v", err, want)
		}
	}
	wantEvents := []string{
		"stop:panics", "stop:continues",
		"internal:panics", "internal:continues",
		"hook:panics", "hook:continues",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("close events = %v, want %v", events, wantEvents)
	}
	rt.refStore.mu.Lock()
	storeClosed := rt.refStore.runtimeClosed
	rt.refStore.mu.Unlock()
	if !storeClosed {
		t.Fatal("reference store remained open after callback panics")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("idempotent Close reran callbacks: %v", events)
	}
}

func TestRuntimeRejectsForeignModuleAndAllowsExplicitRebind(t *testing.T) {
	rtA := NewRuntime()
	modA, err := rtA.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	compiled := modA.Compiled()
	defer compiled.Close()
	if err := rtA.Close(); err != nil {
		t.Fatal(err)
	}

	rtB := NewRuntime()
	defer rtB.Close()
	var beforeInstantiate int
	rtB.hooks.BeforeInstantiate(func(*InstantiateContext) error {
		beforeInstantiate++
		return nil
	})
	if _, err := rtB.Instantiate(context.Background(), modA); !errors.Is(err, ErrForeignModule) {
		t.Fatalf("foreign Instantiate error = %v, want ErrForeignModule", err)
	}
	if beforeInstantiate != 0 {
		t.Fatalf("foreign module reached %d instantiate hooks", beforeInstantiate)
	}

	modB, err := rtB.Module(compiled)
	if err != nil {
		t.Fatalf("explicit rebind: %v", err)
	}
	instance, err := rtB.Instantiate(context.Background(), modB)
	if err != nil {
		t.Fatalf("Instantiate rebound module: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if beforeInstantiate != 1 {
		t.Fatalf("rebound module reached %d instantiate hooks, want 1", beforeInstantiate)
	}
}

type mutableInfoExtension struct {
	info ExtensionInfo
}

func (e *mutableInfoExtension) Info() ExtensionInfo    { return e.info }
func (*mutableInfoExtension) Register(*Registry) error { return nil }

func TestInspectionSnapshotsDeepCopyMutableContainers(t *testing.T) {
	rt := NewRuntime()
	ext := &mutableInfoExtension{info: ExtensionInfo{
		ID: "test.mutable-info", Authors: []string{"author"}, Tags: []string{"tag"},
		Requires: []string{"required"}, Before: []string{"before"}, After: []string{"after"},
		RequiresCapabilities: []PluginCapability{PluginHostImports},
		Compat:               Compatibility{Engines: map[string]string{"wago": "*"}, Platforms: []string{"linux/amd64"}},
	}}
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	baseline, err := json.Marshal(rt.Extensions())
	if err != nil {
		t.Fatal(err)
	}

	ext.info.Authors[0] = "changed"
	ext.info.Tags[0] = "changed"
	ext.info.Requires[0] = "changed"
	ext.info.Before[0] = "changed"
	ext.info.After[0] = "changed"
	ext.info.RequiresCapabilities[0] = PluginCoreRuntime
	ext.info.Compat.Platforms[0] = "changed"
	ext.info.Compat.Engines["wago"] = "changed"
	returned := rt.Extensions()
	returned[0].Authors[0] = "caller"
	returned[0].Tags[0] = "caller"
	returned[0].Requires[0] = "caller"
	returned[0].Before[0] = "caller"
	returned[0].After[0] = "caller"
	returned[0].RequiresCapabilities[0] = PluginCoreRuntime
	returned[0].Compat.Platforms[0] = "caller"
	returned[0].Compat.Engines["wago"] = "caller"
	after, err := json.Marshal(rt.Extensions())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(baseline) {
		t.Fatalf("extension snapshot mutated:\n got %s\nwant %s", after, baseline)
	}

	mod := callsEnvF(t, rt)
	importBaseline, err := json.Marshal(mod.Imports())
	if err != nil {
		t.Fatal(err)
	}
	imports := mod.Imports()
	imports[0].Params[0] = ValV128
	imports[0].Results[0] = ValV128
	imports[0].ParamTypes[0] = ValueTypeDescriptor{Kind: ValueTypeV128}
	imports[0].ResultTypes[0] = ValueTypeDescriptor{Kind: ValueTypeV128}
	importAfter, err := json.Marshal(mod.Imports())
	if err != nil {
		t.Fatal(err)
	}
	if string(importAfter) != string(importBaseline) {
		t.Fatalf("import snapshot mutated:\n got %s\nwant %s", importAfter, importBaseline)
	}
}

func TestInspectionCloneCoverageTracksMutableFields(t *testing.T) {
	assertMutableFields(t, reflect.TypeOf(ImportSpec{}), map[string]bool{
		"Params": true, "Results": true, "ParamTypes": true, "ResultTypes": true,
	})
	assertMutableFields(t, reflect.TypeOf(ExtensionInfo{}), map[string]bool{
		"Authors": true, "Tags": true, "Requires": true, "Before": true,
		"After": true, "RequiresCapabilities": true,
	})
	assertMutableFields(t, reflect.TypeOf(Compatibility{}), map[string]bool{
		"Engines": true, "Platforms": true,
	})

	empty := cloneExtensionInfo(ExtensionInfo{Authors: []string{}, Compat: Compatibility{Engines: map[string]string{}, Platforms: []string{}}})
	if empty.Authors == nil || empty.Compat.Engines == nil || empty.Compat.Platforms == nil {
		t.Fatal("clone collapsed non-nil empty containers")
	}
}

func assertMutableFields(t *testing.T, typ reflect.Type, want map[string]bool) {
	t.Helper()
	got := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Pointer {
			got[field.Name] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutable fields in %s = %v, update clone coverage for %v", typ.Name(), got, want)
	}
}
