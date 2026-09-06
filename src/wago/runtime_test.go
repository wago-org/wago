package wago

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
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

func TestRuntimeInstantiateRetainsOnlyEffectiveImports(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	rt.imports["env.f"] = HostFunc(func(_ HostModule, p, r []uint64) { r[0] = I32(AsI32(p[0]) * 3) })
	for i := 0; i < 256; i++ {
		rt.imports["unused."+strconv.Itoa(i)] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}

	mod := callsEnvF(t, rt)
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	imports := in.Imports()
	if len(imports) != 1 || imports["env.f"] == nil {
		t.Fatalf("Imports = %#v, want only env.f", imports)
	}
	imports["env.f"] = "caller mutation"
	if got := in.Imports()["env.f"]; got == "caller mutation" {
		t.Fatal("Imports exposed the instance's internal binding map")
	}

	result, err := in.Invoke("g", I32(7))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := AsI32(result[0]); got != 21 {
		t.Fatalf("g(7) = %d, want 21", got)
	}
}

func TestRuntimeInstantiateRetainsExplicitUnusedOverrides(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	rt.imports["env.f"] = HostFunc(func(_ HostModule, p, r []uint64) { r[0] = I32(AsI32(p[0]) * 3) })
	rt.imports["unused.runtime"] = HostFunc(func(HostModule, []uint64, []uint64) {})
	mod := callsEnvF(t, rt)

	marker := new(int)
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"env.f":           HostFunc(func(_ HostModule, p, r []uint64) { r[0] = I32(AsI32(p[0]) + 1) }),
		"unused.explicit": marker,
	}))
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	imports := in.Imports()
	if len(imports) != 2 || imports["unused.explicit"] != marker {
		t.Fatalf("Imports = %#v, want effective env.f and explicit unused override", imports)
	}
	if _, ok := imports["unused.runtime"]; ok {
		t.Fatal("Imports retained an unrelated runtime binding")
	}
	result, err := in.Invoke("g", I32(7))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := AsI32(result[0]); got != 8 {
		t.Fatalf("g(7) = %d, want override result 8", got)
	}
}

func TestResolveInstanceImportsDoesNotAllocateForUnrelatedNamespace(t *testing.T) {
	rt := NewRuntime()
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	for i := 0; i < 10_000; i++ {
		rt.imports["unused."+strconv.Itoa(i)] = fn
	}

	allocs := testing.AllocsPerRun(100, func() {
		imports, pluginGCImports, err := rt.resolveInstanceImports(nil, nil, nil, nil)
		if err != nil || imports != nil || pluginGCImports != nil {
			t.Fatalf("resolveInstanceImports = %#v, %#v, %v, want nil, nil, nil", imports, pluginGCImports, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("resolveInstanceImports allocations = %v, want 0", allocs)
	}
}

func TestResolveInstanceImportsOrdinaryImportDoesNotAllocateCollisionMap(t *testing.T) {
	rt := NewRuntime()
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	rt.imports["env.f"] = fn
	rt.importMeta["env.f"] = &registeredImport{module: "env", name: "f", fn: fn}
	specs := []ImportSpec{{Module: "env", Name: "f", Kind: ImportFunc}}
	allocs := testing.AllocsPerRun(100, func() {
		imports, pluginGCImports, err := rt.resolveInstanceImports(specs, nil, nil, nil)
		if err != nil || len(imports) != 1 || imports["env.f"] == nil || pluginGCImports != nil {
			t.Fatalf("resolveInstanceImports = %#v, %#v, %v", imports, pluginGCImports, err)
		}
	})
	if allocs > 3 {
		t.Fatalf("resolveInstanceImports allocations = %v, want at most 3 without a collision map", allocs)
	}
}

func TestResolveInstanceImportsDottedFieldsDoNotAllocateCollisionMap(t *testing.T) {
	rt := NewRuntime()
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	for _, name := range []string{"a", "b", "a.b", "c.d"} {
		key := "env." + name
		rt.imports[key] = fn
		rt.importMeta[key] = &registeredImport{module: "env", name: name, fn: fn}
	}
	allocations := func(specs []ImportSpec) float64 {
		return testing.AllocsPerRun(100, func() {
			imports, pluginGCImports, err := rt.resolveInstanceImports(specs, nil, nil, nil)
			if err != nil || len(imports) != 2 || pluginGCImports != nil {
				panic(fmt.Sprintf("resolveInstanceImports = %#v, %#v, %v", imports, pluginGCImports, err))
			}
		})
	}
	plain := allocations([]ImportSpec{{Module: "env", Name: "a", Kind: ImportFunc}, {Module: "env", Name: "b", Kind: ImportFunc}})
	dotted := allocations([]ImportSpec{{Module: "env", Name: "a.b", Kind: ImportFunc}, {Module: "env", Name: "c.d", Kind: ImportFunc}})
	if dotted > plain {
		t.Fatalf("dotted-field allocations = %.0f, plain fields = %.0f", dotted, plain)
	}
}

func TestResolveInstanceImportsMatchingExactIdentityDoesNotAllocateCollisionMap(t *testing.T) {
	rt := NewRuntime()
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	allocations := func(module string) float64 {
		specs := []ImportSpec{{Module: module, Name: "f", Kind: ImportFunc}}
		declared, err := indexDeclaredImportIdentities(specs)
		if err != nil {
			t.Fatal(err)
		}
		identity := importBindingKey{module: module, name: "f"}
		exact := map[string]exactImportOverride{module + ".f": {identity: identity, value: fn}}
		return testing.AllocsPerRun(100, func() {
			imports, pluginGCImports, err := rt.resolveInstanceImports(specs, declared, nil, exact)
			if err != nil || len(imports) != 1 || pluginGCImports != nil {
				panic(fmt.Sprintf("resolveInstanceImports = %#v, %#v, %v", imports, pluginGCImports, err))
			}
		})
	}
	plain := allocations("env")
	dotted := allocations("env.prod")
	if dotted > plain {
		t.Fatalf("matching dotted identity allocations = %.0f, plain identity = %.0f", dotted, plain)
	}
}

func TestRuntimeReservedUnusedOverrideRejected(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	rt.imports["wago_timer.now"] = HostFunc(func(HostModule, []uint64, []uint64) {})
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer mod.Close()

	if _, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"wago_timer.now": HostFunc(func(HostModule, []uint64, []uint64) {}),
	})); err == nil {
		t.Fatal("unused reserved-module override was accepted")
	}
}

func TestRuntimeCompileHookTransformAndFailures(t *testing.T) {
	const module = "\x00asm\x01\x00\x00\x00"
	rt := NewRuntime()
	defer rt.Close()
	var beforeCalls, afterCalls int
	rt.hooks.beforeCompile = append(rt.hooks.beforeCompile, func(ModuleSourceContext, []byte) ([]byte, error) {
		beforeCalls++
		return []byte(module), nil
	})
	rt.hooks.afterCompile = append(rt.hooks.afterCompile, func(event ModuleCompiledEvent) {
		afterCalls++
		if event.Module.compiled == nil {
			t.Fatal("compile observer lost immutable module view")
		}
	})
	if _, err := rt.Compile(nil); err != nil {
		t.Fatalf("transformed Compile: %v", err)
	}
	if beforeCalls != 1 || afterCalls != 1 {
		t.Fatalf("hook calls before/after = %d/%d", beforeCalls, afterCalls)
	}
	rt.hooks.beforeCompile = append(rt.hooks.beforeCompile, func(ModuleSourceContext, []byte) ([]byte, error) { return nil, errors.New("before rejected") })
	if _, err := rt.Compile([]byte(module)); err == nil || afterCalls != 1 {
		t.Fatalf("before-hook failure = %v; after calls = %d", err, afterCalls)
	}

	failedAfter := NewRuntime()
	defer failedAfter.Close()
	failedAfter.hooks.afterCompile = append(failedAfter.hooks.afterCompile, func(ModuleCompiledEvent) { panic(errors.New("observer panicked")) })
	if _, err := failedAfter.Compile([]byte(module)); err == nil {
		t.Fatal("observer panic was not contained")
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

func TestRuntimeDirectInstanceAggregateLimits(t *testing.T) {
	const pageBytes = uint64(65536)
	cfg := NewRuntimeConfig().WithInstanceLimits(2, pageBytes)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	first, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := rt.Instantiate(context.Background(), mod); err == nil || second != nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("second instance = %v, %v; want aggregate memory rejection", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if rt.instanceReservations != nil {
		t.Fatalf("released instance reservation remains indexed: %#v", rt.instanceReservations)
	}
	second, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate after release: %v", err)
	}
	defer second.Close()
	if rt.directInstanceCount != 1 || rt.directInstanceMemory != pageBytes {
		t.Fatalf("aggregate usage = %d instances, %d bytes", rt.directInstanceCount, rt.directInstanceMemory)
	}
}

func TestRuntimeNativeMemoryMappingLimitAndStats(t *testing.T) {
	cfg := NewRuntimeConfig().WithNativeMemoryMappingLimit(1)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	first, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	stats := rt.ResourceStats()
	if !stats.NativeMemoryMappingsTracked || stats.NativeMemoryMappings != 1 || stats.PeakNativeMemoryMappings != 1 || stats.MaxNativeMemoryMappings != 1 {
		t.Fatalf("runtime resource stats = %#v", stats)
	}
	second, err := rt.Instantiate(context.Background(), mod)
	if second != nil || !errors.Is(err, ErrResourceLimit) || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("second instance = %v, %v; want resource and permission limit", second, err)
	}
	var limitErr *ResourceLimitError
	if !errors.As(err, &limitErr) || limitErr.Scope != "runtime" || limitErr.Used != 1 || limitErr.Requested != 1 || limitErr.Limit != 1 {
		t.Fatalf("runtime limit error = %#v / %v", limitErr, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 0 || stats.PeakNativeMemoryMappings != 1 {
		t.Fatalf("released runtime resource stats = %#v", stats)
	}
	second, err = rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate after release: %v", err)
	}
	defer second.Close()
}

func TestRuntimeModuleEnforcesMemoryCountLimit(t *testing.T) {
	compiled := &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{{}, {}}}}
	defer compiled.Close()

	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithMaxMemoriesPerModule(1)))
	defer rt.Close()
	_, err := rt.Module(compiled)
	if !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Module error = %v, want ErrResourceLimit", err)
	}
	var limitErr *ResourceLimitError
	if !errors.As(err, &limitErr) || limitErr.Requested != 2 || limitErr.Limit != 1 {
		t.Fatalf("Module error = %#v, want requested 2 and limit 1", err)
	}
}

func TestRuntimeNativeMemoryMappingLimitIncludesManagedInstances(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithNativeMemoryMappingLimit(1)))
	defer rt.Close()
	mod := &Module{c: &Compiled{}}
	reservation, err := rt.reserveRuntimeInstance(mod, InstantiateManaged)
	if err != nil {
		t.Fatalf("first managed reservation: %v", err)
	}
	defer reservation.release()
	if reservation.direct {
		t.Fatal("managed reservation was classified as direct")
	}
	if second, err := rt.reserveRuntimeInstance(mod, InstantiateManaged); second != nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("second managed reservation = %v, %v; want resource limit", second, err)
	}
	if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 1 || stats.DirectInstances != 0 {
		t.Fatalf("managed resource stats = %#v", stats)
	}

	imported := &Module{c: &Compiled{
		memoryImport: "env.memory",
		memoryDir:    &compiledMemoryDirectory{defs: []memoryDef{{ImportKey: "env.memory"}}},
	}}
	if importedReservation, err := rt.reserveRuntimeInstance(imported, InstantiateManaged); err != nil || importedReservation != nil {
		t.Fatalf("imported memory reservation = %v, %v; want no charge", importedReservation, err)
	}
}

func TestRuntimeOwnedNativeMemoryMappingCount(t *testing.T) {
	local := memoryDef{}
	imported := memoryDef{ImportKey: "env.memory"}
	for _, tc := range []struct {
		name string
		c    *Compiled
		want uint32
	}{
		{name: "memoryless control", c: &Compiled{}, want: 1},
		{name: "one local memory", c: &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{local}}}, want: 1},
		{name: "one imported memory", c: &Compiled{memoryImport: "env.memory", memoryDir: &compiledMemoryDirectory{defs: []memoryDef{imported}}}, want: 0},
		{name: "import and two locals", c: &Compiled{memoryImport: "env.memory", memoryDir: &compiledMemoryDirectory{defs: []memoryDef{imported, local, local}}}, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeOwnedNativeMemoryMappings(&Module{c: tc.c}); got != tc.want {
				t.Fatalf("mapping count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRuntimeFailedRetainedInstanceKeepsAggregateReservation(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInstanceLimits(1, 0)))
	defer rt.Close()
	shared, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(watToWasmCA(t, `(module
		(import "owner" "shared" (table $imported 1 1 funcref))
		(table $local 1 1 funcref)
		(func $f)
		(elem (table $imported) (i32.const 0) func $f)
		(elem (table $local) (i32.const 1) func $f))`))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	instantiate := func() (*Instance, error) {
		return rt.Instantiate(context.Background(), mod, WithImports(Imports{"owner.shared": shared}))
	}
	if in, err := instantiate(); err == nil || in != nil || !strings.Contains(err.Error(), "table 1") {
		t.Fatalf("failed retained instance = %v, %v; want local-table bounds error", in, err)
	}
	if rt.directInstanceCount != 1 || len(rt.instanceReservations) != 1 {
		t.Fatalf("retained aggregate reservation = %d instances, %d records; want 1, 1", rt.directInstanceCount, len(rt.instanceReservations))
	}
	if in, err := instantiate(); err == nil || in != nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("second instance = %v, %v; want aggregate limit rejection", in, err)
	}
	if err := shared.Close(); err != nil {
		t.Fatal(err)
	}
	if rt.directInstanceCount != 0 || rt.instanceReservations != nil {
		t.Fatalf("released aggregate reservation = %d instances, %#v records; want 0, nil", rt.directInstanceCount, rt.instanceReservations)
	}
}

func TestRuntimeCountOnlyInstanceLimitSkipsMemoryAccounting(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInstanceLimits(1, 0)))
	defer rt.Close()
	// A 2^48-page memory64 maximum is exactly 2^64 bytes and therefore cannot
	// be represented by the optional aggregate byte counter. Count-only limits
	// must not inspect or reject that otherwise valid declaration.
	mod := &Module{c: &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{{Addr64: true, HasMax: true, Max: 1 << 48}}}}}
	reservation, err := rt.reserveRuntimeInstance(mod, InstantiateDirect)
	if err != nil {
		t.Fatalf("count-only reservation: %v", err)
	}
	defer reservation.release()
	if reservation.memory != 0 {
		t.Fatalf("count-only reservation charged %d memory bytes", reservation.memory)
	}
	if second, err := rt.reserveRuntimeInstance(mod, InstantiateDirect); err == nil || second != nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("second count-only reservation = %v, %v; want instance limit", second, err)
	}
}

func TestRuntimeMemoryLimitChargesFullNoMaximumMemory32(t *testing.T) {
	const (
		pageBytes = uint64(65536)
		maxPages  = uint64(65536)
	)
	mod := &Module{c: &Compiled{HasMemory: true}}
	if got, err := managedMemoryReservation(mod); err != nil || got != maxPages*pageBytes {
		t.Fatalf("no-max memory32 reservation = %d, %v; want %d", got, err, maxPages*pageBytes)
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInstanceLimits(0, (maxPages-1)*pageBytes)))
	defer rt.Close()
	if reservation, err := rt.reserveRuntimeInstance(mod, InstantiateDirect); reservation != nil || err == nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("undersized no-max memory32 reservation = %v, %v; want permission denial", reservation, err)
	}
}

func TestRuntimeMemoryLimitChargesFullSecondaryNoMaximumMemory64(t *testing.T) {
	const (
		pageBytes = uint64(65536)
		maxPages  = uint64(65536)
	)
	mod := &Module{c: &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{
		{HasMax: true, Max: 1},
		{Addr64: true},
	}}}}
	want := (maxPages + 1) * pageBytes
	if got, err := managedMemoryReservation(mod); err != nil || got != want {
		t.Fatalf("secondary no-max memory64 reservation = %d, %v; want %d", got, err, want)
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInstanceLimits(0, want-pageBytes)))
	defer rt.Close()
	if reservation, err := rt.reserveRuntimeInstance(mod, InstantiateDirect); reservation != nil || err == nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("undersized secondary no-max memory64 reservation = %v, %v; want permission denial", reservation, err)
	}
}

func TestRuntimeMemoryLimitChargesFullImportedNoMaximumMemory64(t *testing.T) {
	const (
		pageBytes = uint64(65536)
		maxPages  = uint64(65536)
	)
	mod := &Module{c: &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{
		{ImportKey: "env.memory", Addr64: true},
	}}}}
	if got, err := managedMemoryReservation(mod); err != nil || got != maxPages*pageBytes {
		t.Fatalf("imported no-max memory64 reservation = %d, %v; want %d", got, err, maxPages*pageBytes)
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInstanceLimits(0, (maxPages-1)*pageBytes)))
	defer rt.Close()
	if reservation, err := rt.reserveRuntimeInstance(mod, InstantiateDirect); reservation != nil || err == nil || !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("undersized imported no-max memory64 reservation = %v, %v; want permission denial", reservation, err)
	}
}

func TestRuntimeMemoryLimitClassifiesAccountingOverflow(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInstanceLimits(0, 1)))
	defer rt.Close()
	mod := &Module{c: &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{{Addr64: true, HasMax: true, Max: 1 << 48}}}}}
	reservation, err := rt.reserveRuntimeInstance(mod, InstantiateDirect)
	if reservation != nil || err == nil || !errors.Is(err, ErrPermissionDenied) || !strings.Contains(err.Error(), "overflows bytes") {
		t.Fatalf("overflow reservation = %v, %v; want detailed ErrPermissionDenied", reservation, err)
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
	owned := railshotHostIndirectOwnedSyncThunk(3, 1, 2)
	borrowed := railshotHostIndirectSyncThunk(3, 1, 2)
	if len(owned) == 0 || len(borrowed) == 0 || string(owned) == string(borrowed) {
		t.Fatal("owned host thunk was not emitted distinctly")
	}
}

func TestHostFuncRefAttachmentDeduplication(t *testing.T) {
	var attachments hostFuncRefAttachments
	if err := attachments.attach(nil, nil, FuncSig{}, nil, 0, nil, 0); err == nil {
		t.Fatal("nil host funcref owner accepted")
	}
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(func(HostModule, []uint64, []uint64) {}, FuncSig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := attachments.attach(owner, rt.refStore, FuncSig{}, nil, 0, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := attachments.attach(owner, rt.refStore, FuncSig{}, nil, 0, nil, 0); err != nil {
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
	if err := (&hostFuncRefAttachments{}).attach(owner, rt.refStore, FuncSig{}, nil, 0, nil, 0); err == nil {
		t.Fatal("closed host funcref owner attached")
	}
}

func TestRuntimeImportOptionsOwnOneResolvedMap(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod := callsEnvF(t, rt)
	first := Imports{"unused.first": 1, "env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = I32(1) })}
	last := Imports{"unused.last": 2, "env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = I32(9) })}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(first), WithImports(last))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	first["unused.first"] = 99
	delete(last, "env.f")
	last["unused.last"] = 99
	got, err := in.Invoke("g", I32(7))
	if err != nil || AsI32(got[0]) != 9 {
		t.Fatalf("last override did not remain owned: %v, %v", got, err)
	}
	imports := in.Imports()
	if imports["unused.first"] != 1 || imports["unused.last"] != 2 {
		t.Fatalf("caller maps changed resolved imports: %v", imports)
	}
	if len(first) != 2 || len(last) != 1 {
		t.Fatal("resolution mutated caller maps")
	}
}
