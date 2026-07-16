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
	constraint    string
	onInstantiate func()
}

func (e tripleExt) Info() ExtensionInfo {
	var compat Compatibility
	switch {
	case e.constraint != "":
		compat.Engines = map[string]string{"wago": e.constraint}
	case e.minWago != "":
		compat.Engines = map[string]string{"wago": ">=" + e.minWago}
	}
	return ExtensionInfo{ID: "test.triple", Name: "Triple", Version: "1.0.0", Compat: compat, Stability: Stable}
}

func (e tripleExt) Register(reg *Registry) error {
	reg.Capability(CapMetricsWrite, CapabilityDocs("demo capability"))
	// Bare func literal (no explicit HostFunc conversion) — the portable form.
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

func TestRuntimeCompileHookTransformAndFailures(t *testing.T) {
	const module = "\x00asm\x01\x00\x00\x00"
	rt := NewRuntime()
	defer rt.Close()
	var beforeCalls, afterCalls int
	rt.hooks.BeforeCompile(func(ctx *CompileContext, source []byte) ([]byte, error) {
		beforeCalls++
		if ctx.Runtime != rt || ctx.Metadata == nil {
			t.Fatal("compile hook received incomplete context")
		}
		ctx.Metadata["transformed"] = true
		if len(source) == 0 {
			return []byte(module), nil
		}
		return nil, nil
	})
	rt.hooks.AfterCompile(func(ctx *CompileContext, mod *Module) error {
		afterCalls++
		if ctx.Metadata["transformed"] != true || mod == nil {
			t.Fatal("after hook lost compile context or module")
		}
		return nil
	})
	if _, err := rt.Compile(nil); err != nil {
		t.Fatalf("transformed Compile: %v", err)
	}
	if beforeCalls != 1 || afterCalls != 1 {
		t.Fatalf("hook calls before/after = %d/%d", beforeCalls, afterCalls)
	}
	rt.hooks.BeforeCompile(func(*CompileContext, []byte) ([]byte, error) { return nil, errors.New("before rejected") })
	if _, err := rt.Compile([]byte(module)); err == nil || afterCalls != 1 {
		t.Fatalf("before-hook failure = %v; after calls = %d", err, afterCalls)
	}

	failedAfter := NewRuntime()
	defer failedAfter.Close()
	failedAfter.hooks.AfterCompile(func(*CompileContext, *Module) error { return errors.New("after rejected") })
	if _, err := failedAfter.Compile([]byte(module)); err == nil {
		t.Fatal("after-hook failure accepted")
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

func TestImportFunctionDocumentation(t *testing.T) {
	reg := &Registry{}
	reg.ImportModule("env").Func("f", func(HostModule, []uint64, []uint64) {}).Docs("test import")
	if len(reg.imports) != 1 || reg.imports[0].docs != "test import" {
		t.Fatalf("import documentation = %#v", reg.imports)
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
		Func("f", HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })).
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

// TestRuntimeWagoConstraintRange checks that the "wago" engine constraint is
// evaluated as a full semver range at Use time.
func TestRuntimeWagoConstraintRange(t *testing.T) {
	ok := tripleExt{}
	ok.constraint = ">=0.1.0 <2.0.0"
	if err := NewRuntime().Use(ok); err != nil {
		t.Errorf("in-range constraint rejected: %v", err)
	}
	bad := tripleExt{}
	bad.constraint = ">=2.0.0"
	if err := NewRuntime().Use(bad); err == nil {
		t.Error("out-of-range constraint accepted")
	}
	malformed := tripleExt{}
	malformed.constraint = ">=1.2.3.4"
	if err := NewRuntime().Use(malformed); err == nil {
		t.Error("malformed constraint accepted")
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
		Func("now", HostFunc(func(_ HostModule, _, r []uint64) { r[0] = 0 })).
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
		WithImports(Imports{"wago_timer.now": HostFunc(func(_ HostModule, _, r []uint64) { r[0] = 99 })}))
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
		WithImports(Imports{"wago_timer.now": HostFunc(func(_ HostModule, _, r []uint64) { r[0] = 99 })}))
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

func TestInstantiationMarkersAndOwnedHostThunk(t *testing.T) {
	(&Compiled{}).instantiable()
	(&Snapshot{}).instantiable()
	owned := railshotHostIndirectOwnedSyncThunk(3, 1, 2)
	borrowed := railshotHostIndirectSyncThunk(3, 1, 2)
	if len(owned) == 0 || len(borrowed) == 0 || string(owned) == string(borrowed) {
		t.Fatal("owned host thunk was not emitted distinctly")
	}
}

func TestHostFuncRefAttachmentDeduplication(t *testing.T) {
	var attachments hostFuncRefAttachments
	if err := attachments.attach(nil, nil, FuncSig{}); err == nil {
		t.Fatal("nil host funcref owner accepted")
	}
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(func(HostModule, []uint64, []uint64) {}, FuncSig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := attachments.attach(owner, rt.refStore, FuncSig{}); err != nil {
		t.Fatal(err)
	}
	if err := attachments.attach(owner, rt.refStore, FuncSig{}); err != nil {
		t.Fatal(err)
	}
	if owner.importers != 1 {
		t.Fatalf("importers = %d, want deduplicated 1", owner.importers)
	}
	attachments.detachAll()
	if owner.importers != 0 {
		t.Fatalf("importers after detach = %d", owner.importers)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (&hostFuncRefAttachments{}).attach(owner, rt.refStore, FuncSig{}); err == nil {
		t.Fatal("closed host funcref owner attached")
	}
}
