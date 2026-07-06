package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// tripleExt is a minimal extension that provides env.f(i32)->i32 = 3*x, declares
// a capability, and (optionally) records an instantiate hook.
type tripleExt struct {
	minWago       string
	onInstantiate func()
}

func (e tripleExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.triple", Name: "Triple", Version: "1.0.0", MinWago: e.minWago, Stability: Stable}
}

func (e tripleExt) Register(reg *Registry) error {
	reg.Capability(CapMetricsWrite, CapabilityDocs("demo capability"))
	// Bare func literal (no explicit SyncHostFunc conversion) — the portable form.
	reg.ImportModule("env").
		Func("f", func(_ HostModule, p, r []uint64) { r[0] = I32(AsI32(p[0]) * 3) }).
		Params(ValI32).Results(ValI32).Capability(CapMetricsWrite)
	if e.onInstantiate != nil {
		reg.Hooks().AfterInstantiate(func(_ *InstantiateContext, _ *Instance) error {
			e.onInstantiate()
			return nil
		})
	}
	return nil
}

// callsEnvF compiles a module: import env.f(i32)->i32, export g(x)=env.f(x).
func callsEnvF(t *testing.T, rt *Runtime) *Module {
	t.Helper()
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b} // local.get 0; call 0; end
	m, err := rt.Compile(returningImportModule(sig, body))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m
}

func TestRuntimeUseAndInvoke(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	c := callsEnvF(t, rt)
	in, err := rt.Instantiate(context.Background(), c)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(7))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 21 {
		t.Fatalf("g(7) = %d, want 21", AsI32(res[0]))
	}
}

func TestRuntimeInspection(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	if exts := rt.Extensions(); len(exts) != 1 || exts[0].ID != "test.triple" {
		t.Fatalf("Extensions() = %+v", exts)
	}
	caps := rt.Capabilities()
	if len(caps) != 1 || caps[0] != CapMetricsWrite {
		t.Fatalf("Capabilities() = %v", caps)
	}
}

func TestRuntimeAfterInstantiateHook(t *testing.T) {
	fired := 0
	rt := NewRuntime()
	if err := rt.Use(tripleExt{onInstantiate: func() { fired++ }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	c := callsEnvF(t, rt)
	in, err := rt.Instantiate(context.Background(), c)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	in.Close()
	if fired != 1 {
		t.Fatalf("AfterInstantiate fired %d times, want 1", fired)
	}
}

func TestRuntimeDuplicateExtension(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	err := rt.Use(tripleExt{})
	if !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("duplicate Use error = %v, want ErrExtensionConflict", err)
	}
}

// otherEnvExt claims module "env" under a different extension ID.
type otherEnvExt struct{}

func (otherEnvExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.other", Version: "1.0.0", Stability: Stable}
}
func (otherEnvExt) Register(reg *Registry) error {
	reg.ImportModule("env").
		Func("f", SyncHostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })).
		Params(ValI32).Results(ValI32)
	return nil
}

func TestRuntimeImportModuleCollision(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	err := rt.Use(otherEnvExt{})
	if !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("colliding module Use error = %v, want ErrExtensionConflict", err)
	}
	// The failed Use must not have registered the extension.
	if len(rt.Extensions()) != 1 {
		t.Fatalf("failed Use left %d extensions registered", len(rt.Extensions()))
	}
}

func TestRuntimeMinWagoTooNew(t *testing.T) {
	rt := NewRuntime()
	err := rt.Use(tripleExt{minWago: "999.0.0"})
	if err == nil {
		t.Fatal("expected version-incompatibility error")
	}
}

// timerLikeExt provides a reserved wago_timer module, to test user override
// protection.
type timerLikeExt struct{}

func (timerLikeExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "wago.timer", Version: "1.0.0", Stability: Stable}
}
func (timerLikeExt) Register(reg *Registry) error {
	reg.ImportModule("wago_timer").
		Func("now", SyncHostFunc(func(_ HostModule, _, r []uint64) { r[0] = 0 })).
		Results(ValI64)
	return nil
}

func TestReservedModuleUserOverrideRejected(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(timerLikeExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	// A module importing wago_timer.now, exported g()=now().
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I64})
	body := []byte{0x00, 0x10, 0x00, 0x0b} // call 0; end
	imp := append(append(wasmtest.Name("wago_timer"), wasmtest.Name("now")...), 0x00, 0x00)
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	c, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = rt.Instantiate(context.Background(), c,
		WithImports(Imports{"wago_timer.now": SyncHostFunc(func(_ HostModule, _, r []uint64) { r[0] = 99 })}))
	if err == nil {
		t.Fatal("expected reserved-module override to be rejected")
	}

	// With AllowTestOverrides the same override is permitted.
	rt2 := NewRuntime(WithImportOverridePolicy(AllowTestOverrides))
	if err := rt2.Use(timerLikeExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	c2, err := rt2.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt2.Instantiate(context.Background(), c2,
		WithImports(Imports{"wago_timer.now": SyncHostFunc(func(_ HostModule, _, r []uint64) { r[0] = 99 })}))
	if err != nil {
		t.Fatalf("instantiate with override: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI64(res[0]) != 99 {
		t.Fatalf("g() = %d, want 99 (overridden)", AsI64(res[0]))
	}
}
