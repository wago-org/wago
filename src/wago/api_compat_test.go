package wago

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicAPICompatibilityForms(t *testing.T) {
	c, err := Compile(signExtModule())
	if err != nil {
		t.Fatalf("Compile([]byte): %v", err)
	}
	in, err := Instantiate(c, Imports{})
	if err != nil {
		t.Fatalf("Instantiate(compiled, Imports): %v", err)
	}
	in.Close()
	in, err = Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate(compiled, nil): %v", err)
	}
	in.Close()

	cfg := NewRuntimeConfig().WithDeferBoundsChecks(true)
	if _, err := CompileWithConfig(cfg, signExtModule()); err != nil {
		t.Fatalf("CompileWithConfig: %v", err)
	}
}

func TestInvokeCacheKeepsAlternatingExports(t *testing.T) {
	c := MustCompile(alternatingExportsModule())
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	for i := int32(0); i < 8; i++ {
		f, err := in.Invoke("f", I32(i))
		if err != nil {
			t.Fatalf("Invoke f: %v", err)
		}
		if got := AsI32(f[0]); got != i+1 {
			t.Fatalf("f(%d) = %d, want %d", i, got, i+1)
		}
		g, err := in.Invoke("g", I32(i))
		if err != nil {
			t.Fatalf("Invoke g: %v", err)
		}
		if got := AsI32(g[0]); got != i+2 {
			t.Fatalf("g(%d) = %d, want %d", i, got, i+2)
		}
		if _, err := in.Invoke("__collect"); err != nil {
			t.Fatalf("Invoke __collect: %v", err)
		}
	}
}

func BenchmarkInvokeAlternatingExports(b *testing.B) {
	c := MustCompile(alternatingExportsModule())
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	x := I32(7)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("f", x); err != nil {
			b.Fatal(err)
		}
		if _, err := in.Invoke("g", x); err != nil {
			b.Fatal(err)
		}
		if _, err := in.Invoke("__collect"); err != nil {
			b.Fatal(err)
		}
	}
}

func alternatingExportsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f", 0, 0),
			wasmtest.ExportEntry("g", 0, 1),
			wasmtest.ExportEntry("__collect", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}), // local.get 0; i32.const 1; i32.add
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x02, 0x6a, 0x0b}), // local.get 0; i32.const 2; i32.add
			wasmtest.Code([]byte{0x0b}),
		)),
	)
}

func TestCompiledAPIHelpers(t *testing.T) {
	if got, err := funcrefExprPayload(wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}); err != nil || got != nullFuncRefIndex {
		t.Fatalf("null funcref payload = %d, %v", got, err)
	}
	if got, err := funcrefExprPayload(wasm.Expr{BodyBytes: []byte{0xd2, 0x02, 0x0b}}); err != nil || got != 2 {
		t.Fatalf("ref.func payload = %d, %v", got, err)
	}
	if _, err := funcrefExprPayload(wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}); err == nil {
		t.Fatal("non-funcref expression accepted")
	}
	if funcTypeUsesV128(nil) || funcTypeUsesV128(&wasm.CompType{Params: []wasm.ValType{wasm.I32}}) ||
		!funcTypeUsesV128(&wasm.CompType{Params: []wasm.ValType{wasm.V128}}) ||
		!funcTypeUsesV128(&wasm.CompType{Results: []wasm.ValType{wasm.V128}}) {
		t.Fatal("v128 function signature detection changed")
	}
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}
	if !sigMatches(ft, &InstanceExport{params: []ValType{ValI32}, results: []ValType{ValI64}}) ||
		sigMatches(ft, &InstanceExport{params: []ValType{ValI64}, results: []ValType{ValI64}}) ||
		sigMatches(ft, &InstanceExport{params: []ValType{ValI32}}) {
		t.Fatal("cross-instance signature matching changed")
	}
	for _, tc := range []struct {
		sig  FuncSig
		want bool
	}{
		{FuncSig{}, true},
		{FuncSig{Params: []ValType{ValI32}}, true},
		{FuncSig{Params: []ValType{ValI64}}, false},
		{FuncSig{Params: []ValType{ValI32, ValI32}}, false},
		{FuncSig{Results: []ValType{ValI32}}, false},
	} {
		if got := asyncReplayable(tc.sig); got != tc.want {
			t.Errorf("asyncReplayable(%+v) = %v, want %v", tc.sig, got, tc.want)
		}
	}
	if bodyBytesUseMemoryGrow([]byte{0x0b}) || !bodyBytesUseMemoryGrow([]byte{0x40, 0x00, 0x0b}) || !bodyBytesUseMemoryGrow([]byte{0xff}) {
		t.Fatal("memory.grow byte scanner changed")
	}
	if instrsUseMemoryGrow([]wasm.Instruction{{Kind: wasm.InstrI32Add}}) || !instrsUseMemoryGrow([]wasm.Instruction{{Kind: wasm.InstrMemoryGrow}}) {
		t.Fatal("programmatic memory.grow scanner changed")
	}
	if !moduleUsesMemoryGrow(&wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0x40, 0x00, 0x0b}}}}) ||
		moduleUsesMemoryGrow(&wasm.Module{Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Add}}}}}}) {
		t.Fatal("module memory.grow detection changed")
	}

	elem, data := 0, 0
	for _, in := range []wasm.Instruction{
		{Kind: wasm.InstrTableInit, Index: 2},
		{Kind: wasm.InstrElemDrop, Index: 1},
		{Kind: wasm.InstrMemoryInit, Index: 4},
		{Kind: wasm.InstrDataDrop, Index: 3},
	} {
		segmentStateCount(in.Kind, in.Index, &elem, &data)
	}
	if elem != 3 || data != 5 {
		t.Fatalf("segment state counts = %d, %d", elem, data)
	}
	elem, data = 0, 0
	instrsSegmentStateCounts([]wasm.Instruction{
		{Kind: wasm.InstrTableInit, Index: 2},
		{Kind: wasm.InstrDataDrop, Index: 4},
	}, &elem, &data)
	if elem != 3 || data != 5 {
		t.Fatalf("instruction segment state counts = %d, %d", elem, data)
	}
	if ok := bodyBytesSegmentStateCounts([]byte{0xfc, 0x0c, 0x02, 0x00, 0xfc, 0x09, 0x04, 0x0b}, &elem, &data); !ok || elem != 3 || data != 5 {
		t.Fatalf("byte segment state counts = %d, %d, %v", elem, data, ok)
	}
	if bodyBytesSegmentStateCounts([]byte{0xff}, &elem, &data) {
		t.Fatal("malformed segment bytecode accepted")
	}

	var nilCompiled *Compiled
	if _, ok := nilCompiled.TableImport(); ok || nilCompiled.TableImports() != nil || nilCompiled.FuncDebugName(3) != "func3" {
		t.Fatal("nil compiled helpers changed")
	}
	c := &Compiled{
		tableImport: "env.a",
		extraTables: []tableDef{{ImportKey: "env.b"}},
		Exports:     map[string]int{"z": 1, "a": 1},
		NumImports:  1,
		Names:       &wasm.NameSec{FunctionNames: wasm.NameMap{{Index: 0, Name: "host"}}},
	}
	if _, ok := c.TableImport(); ok {
		t.Fatal("legacy single-table helper accepted multiple imports")
	}
	if got := c.TableImports(); len(got) != 2 || got[0] != "env.a" || got[1] != "env.b" {
		t.Fatalf("TableImports = %v", got)
	}
	if name, ok := c.FuncName(0); !ok || name != "host" {
		t.Fatalf("FuncName = %q, %v", name, ok)
	}
	if _, ok := c.LocalFuncName(-1); ok {
		t.Fatal("negative local function index accepted")
	}
	if got := c.FuncDebugName(1); got != "a" {
		t.Fatalf("FuncDebugName export fallback = %q", got)
	}
	imports := Imports{"env.g": NewGlobalI32(3, false)}
	defer imports["env.g"].(*Global).Close()
	in := &Instance{imports: imports}
	if got := in.Imports(); got["env.g"] != imports["env.g"] {
		t.Fatalf("Imports = %v, want supplied map", got)
	}
}

func TestReturningHostImportUsesCompiledDispatch(t *testing.T) {
	funcImport := append(wasmtest.Name("env"), wasmtest.Name("answer")...)
	funcImport = append(funcImport, 0, 0) // function import, type 0
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile deferred host module: %v", err)
	}
	defer c.Close()
	imports := Imports{"env.answer": HostFunc(func(_ HostModule, _, results []uint64) { results[0] = I32(42) })}
	first, err := c.linkModule(imports, nil)
	if err != nil {
		t.Fatalf("link returning host module: %v", err)
	}
	second, err := c.linkModule(imports, nil)
	if err != nil {
		t.Fatalf("repeat link returning host module: %v", err)
	}
	if first != c || second != c || !c.dynamicImports || len(c.Code) == 0 {
		t.Fatalf("dynamic linked modules = %p, %p owner=%p dynamic=%v code=%d", first, second, c, c.dynamicImports, len(c.Code))
	}
	in, err := Instantiate(c, imports)
	if err != nil {
		t.Fatalf("Instantiate returning host module: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != I32(42) {
		t.Fatalf("run = %v, %v; want 42", got, err)
	}
}

func TestRuntimeConfigPortableFluentSurface(t *testing.T) {
	const emptyModule = "\x00asm\x01\x00\x00\x00"
	base := NewRuntimeConfig()
	cfg := base.WithCoreFeatures(CoreFeatureMutableGlobal).
		WithFeatures(CoreFeatureMutableGlobal, CoreFeatureSignExtensionOps).
		WithFeature(CoreFeatureSIMD, false).
		WithMemoryLimitPages(3).
		WithBoundsChecks(BoundsChecksExplicit).
		WithDeferBoundsChecks(false)
	if base == cfg || base.MemoryLimitPages() == 3 {
		t.Fatal("fluent methods mutated the base config")
	}
	if cfg.CoreFeatures() != CoreFeatureMutableGlobal|CoreFeatureSignExtensionOps || cfg.BoundsChecks() != BoundsChecksExplicit || cfg.DeferBoundsChecks() || cfg.MemoryLimitPages() != 3 {
		t.Fatalf("fluent config mismatch: %+v", cfg)
	}
	if !strings.Contains(cfg.String(), "maxMemoryPages: 3") {
		t.Fatalf("config String = %q", cfg.String())
	}
	if _, err := cfg.Compile([]byte(emptyModule)); err != nil {
		t.Fatalf("fluent Compile: %v", err)
	}
	if cfg.MustCompile([]byte(emptyModule)) == nil {
		t.Fatal("MustCompile returned nil")
	}
	for _, tc := range []struct {
		mode BoundsCheckMode
		want string
	}{{BoundsChecksExplicit, "explicit"}, {BoundsChecksSignalsBased, "signals-based"}, {BoundsCheckMode(99), "BoundsCheckMode(99)"}} {
		if got := tc.mode.String(); got != tc.want {
			t.Errorf("BoundsCheckMode(%d).String() = %q", tc.mode, got)
		}
	}
	if got := (&GuardPageUnavailableError{}).Error(); !strings.Contains(got, "signals-based") {
		t.Fatalf("GuardPageUnavailableError = %q", got)
	}
	if got := (&UnsupportedFeatureError{Requested: CoreFeatureTailCall, Supported: CoreFeaturesV2}).Error(); !strings.Contains(got, "tail-call") {
		t.Fatalf("UnsupportedFeatureError = %q", got)
	}
	err := NewRuntimeConfig().WithFeature(CoreFeatureTailCall, true).Validate()
	var unsupported *UnsupportedFeatureError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Validate unsupported = %v", err)
	}
	if !guardPageBuilt {
		err = NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased).Validate()
		if !IsGuardPageUnavailable(err) {
			t.Fatalf("Validate signals = %v", err)
		}
	}
}

func TestSIMDHostDetectionMatchesArchitectureBaseline(t *testing.T) {
	got := detectSIMDHostFeatures()
	if runtime.GOARCH == "arm64" && !got {
		t.Fatal("arm64 baseline Advanced SIMD was rejected")
	}
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" && got {
		t.Fatalf("unsupported architecture %s admitted SIMD", runtime.GOARCH)
	}
}

func TestRuntimeBuildCapabilitiesAndOptimizationKnobs(t *testing.T) {
	supported := SupportedFeatures()
	if supported&^coreFeaturesWago != 0 || (hostSupportsSIMD() && supported&CoreFeatureSIMD == 0) {
		t.Fatalf("supported features = %s", supported)
	}
	if GuardPageSupported() != guardPageBuilt {
		t.Fatal("guard-page build capability disagrees with build flag")
	}
	knobs := OptKnobs()
	if len(knobs) == 0 || knobs[0].Name == "" || knobs[0].Desc == "" {
		t.Fatalf("optimization knobs = %#v", knobs)
	}
	original := knobs[0].On
	if !SetOptKnob(knobs[0].Name, !original) {
		t.Fatalf("could not set known knob %q", knobs[0].Name)
	}
	if got := OptKnobs()[0].On; got != !original {
		t.Fatalf("knob %q = %v, want %v", knobs[0].Name, got, !original)
	}
	if !SetOptKnob(knobs[0].Name, original) || SetOptKnob("not-a-knob", true) {
		t.Fatal("optimization knob setter result changed")
	}
}

func TestGuardedMemoryFallbackExplainsBuildRequirement(t *testing.T) {
	if guardPageBuilt {
		t.Skip("guard-page build supplies real guarded memory")
	}
	if _, err := newGuardedJobMemory(1, 1); err == nil {
		t.Fatal("guarded memory unexpectedly available without wago_guardpage")
	}
}

func TestMemoryAccessorsPortable(t *testing.T) {
	// A valid module containing one one-page linear memory and no functions.
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0, 5, 3, 1, 0, 1}
	c, err := Compile(wasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	if !in.WriteUint8(0, 0x7f) || !in.WriteUint16Le(2, 0xabcd) || !in.WriteUint32Le(4, 0xdeadbeef) || !in.WriteUint64Le(8, 0x1122334455667788) || !in.WriteFloat32Le(16, 3.5) || !in.WriteFloat64Le(24, 2.5) {
		t.Fatal("typed write failed")
	}
	if v, ok := in.ReadUint8(0); !ok || v != 0x7f {
		t.Fatalf("ReadUint8 = %#x, %v", v, ok)
	}
	if v, ok := in.ReadUint16Le(2); !ok || v != 0xabcd {
		t.Fatalf("ReadUint16Le = %#x, %v", v, ok)
	}
	if v, ok := in.ReadUint32Le(4); !ok || v != 0xdeadbeef {
		t.Fatalf("ReadUint32Le = %#x, %v", v, ok)
	}
	if v, ok := in.ReadUint64Le(8); !ok || v != 0x1122334455667788 {
		t.Fatalf("ReadUint64Le = %#x, %v", v, ok)
	}
	if v, ok := in.ReadFloat32Le(16); !ok || v != 3.5 {
		t.Fatalf("ReadFloat32Le = %v, %v", v, ok)
	}
	if v, ok := in.ReadFloat64Le(24); !ok || v != 2.5 {
		t.Fatalf("ReadFloat64Le = %v, %v", v, ok)
	}
	if !in.Write(40, []byte{1, 2, 3}) {
		t.Fatal("Write failed")
	}
	if got, ok := in.Read(40, 3); !ok || string(got) != "\x01\x02\x03" {
		t.Fatalf("Read = %v, %v", got, ok)
	}

	const end = 65536
	if in.WriteUint8(end, 1) || in.WriteUint16Le(end-1, 1) || in.WriteUint32Le(end-3, 1) || in.WriteUint64Le(end-7, 1) || in.Write(65535, []byte{1, 2}) {
		t.Fatal("out-of-bounds write succeeded")
	}
	if _, ok := in.ReadUint8(end); ok {
		t.Fatal("out-of-bounds byte read succeeded")
	}
	if _, ok := in.ReadUint16Le(end - 1); ok {
		t.Fatal("out-of-bounds uint16 read succeeded")
	}
	if _, ok := in.ReadUint32Le(end - 3); ok {
		t.Fatal("out-of-bounds uint32 read succeeded")
	}
	if _, ok := in.ReadUint64Le(end - 7); ok {
		t.Fatal("out-of-bounds uint64 read succeeded")
	}
	if _, ok := in.Read(65535, 2); ok {
		t.Fatal("out-of-bounds slice read succeeded")
	}
}

func TestRuntimeReferenceAndErrorPortableSurface(t *testing.T) {
	if _, err := (*Runtime)(nil).NewExternRef("x"); err == nil {
		t.Fatal("nil runtime accepted an externref")
	}
	if _, err := (*Runtime)(nil).NewExternRefTable(0, 1); err == nil {
		t.Fatal("nil runtime accepted an externref table")
	}
	rt := NewRuntime()
	ref, err := rt.NewExternRef("value")
	if err != nil {
		t.Fatalf("NewExternRef: %v", err)
	}
	if got, ok := rt.ExternRefValue(ref); !ok || got != "value" {
		t.Fatalf("ExternRefValue = %#v, %v", got, ok)
	}
	if got, ok := rt.ExternRefValue(NullExternRef()); !ok || got != nil {
		t.Fatalf("null ExternRefValue = %#v, %v", got, ok)
	}
	if _, ok := (*Runtime)(nil).ExternRefValue(ref); ok {
		t.Fatal("nil runtime resolved an externref")
	}
	if _, err := (*Instance)(nil).NewExternRef("x"); err == nil {
		t.Fatal("nil instance accepted an externref")
	}
	private := &Instance{}
	privateRef, err := private.NewExternRef("private")
	if err != nil {
		t.Fatalf("private NewExternRef: %v", err)
	}
	if got, ok := private.ExternRefValue(privateRef); !ok || got != "private" || !private.validExternrefToken(privateRef.token) {
		t.Fatalf("private externref = %#v, %v", got, ok)
	}
	if _, ok := (&Instance{}).ExternRefValue(privateRef); ok {
		t.Fatal("another private instance resolved an externref")
	}
	private.closed = true
	if _, err := private.NewExternRef("after-close"); err == nil {
		t.Fatal("closed private instance accepted an externref")
	}
	if private.validExternrefToken(privateRef.token ^ 1) {
		t.Fatal("forged externref token was accepted")
	}
	table, err := rt.NewExternRefTable(2, 4)
	if err != nil {
		t.Fatalf("NewExternRefTable: %v", err)
	}
	if table.Size() != 2 || (*Table)(nil).Size() != 0 {
		t.Fatalf("table size = %d", table.Size())
	}
	if err := table.Close(); err != nil {
		t.Fatalf("table Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("runtime Close: %v", err)
	}
	if _, err := rt.NewExternRefTable(0, 1); err == nil {
		t.Fatal("closed runtime accepted an externref table")
	}

	globalRT := NewRuntime()
	globalRef, err := globalRT.NewExternRef("global")
	if err != nil {
		t.Fatal(err)
	}
	global, err := globalRT.NewExternRefGlobal(globalRef, true)
	if err != nil {
		t.Fatalf("NewExternRefGlobal: %v", err)
	}
	if got, err := global.GetValue(); err != nil || got != ValueExternRef(globalRef) {
		t.Fatalf("externref global value = %v, %v", got, err)
	}
	if err := global.SetValue(ValueExternRef(NullExternRef())); err != nil {
		t.Fatalf("SetValue(null): %v", err)
	}
	if got, err := global.GetValue(); err != nil || !got.ExternRef().IsNull() {
		t.Fatalf("externref global null value = %v, %v", got, err)
	}
	if err := global.SetValue(ValueI32(1)); err == nil {
		t.Fatal("externref global accepted wrong value type")
	}
	foreignRT := NewRuntime()
	foreignRef, err := foreignRT.NewExternRef("foreign")
	if err != nil {
		t.Fatal(err)
	}
	if err := global.SetValue(ValueExternRef(foreignRef)); err == nil {
		t.Fatal("externref global accepted foreign token")
	}
	if err := foreignRT.Close(); err != nil {
		t.Fatal(err)
	}
	if err := global.Close(); err != nil {
		t.Fatal(err)
	}
	if err := globalRT.Close(); err != nil {
		t.Fatal(err)
	}

	pluginErr := &PluginError{Plugin: "test", Phase: PluginPhaseRegister, Capability: PluginHostImports, Path: "x", Err: ErrMissingImport}
	if !errors.Is(pluginErr, ErrMissingImport) || pluginErr.Error() != "wago plugin test: register capability host.imports at x: wago: missing import" {
		t.Fatalf("PluginError = %q", pluginErr)
	}
	extErr := &ExtensionError{Extension: "test", Operation: "use", Err: ErrPermissionDenied}
	if !errors.Is(extErr, ErrPermissionDenied) || extErr.Error() != "wago extension test: use: wago: permission denied" {
		t.Fatalf("ExtensionError = %q", extErr)
	}
	for _, cap := range []PluginCapability{PluginHostImports, PluginHostEnvironment, PluginCompileHooks, PluginInstanceHooks, PluginInvokeHooks, PluginRuntimeHooks, PluginManagedInstances} {
		if !validPluginCapability(cap) {
			t.Errorf("validPluginCapability(%q) = false", cap)
		}
	}
	if validPluginCapability("unknown") {
		t.Fatal("unknown plugin capability accepted")
	}
	for _, tc := range []struct {
		kind ImportKind
		want string
	}{{ImportFunc, "func"}, {ImportGlobal, "global"}, {ImportMemory, "memory"}, {ImportTable, "table"}, {ImportKind(99), "func"}} {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("ImportKind(%d).String() = %q", tc.kind, got)
		}
	}
	if err := (&Module{}).Close(); err != nil {
		t.Fatalf("Module.Close: %v", err)
	}
	if _, ok := (*Runtime)(nil).Extension("anything"); ok {
		t.Fatal("nil runtime found an extension")
	}
	if _, ok := NewRuntime().Extension("anything"); ok {
		t.Fatal("empty runtime found an extension")
	}
	moduleRT := NewRuntime()
	mod, err := moduleRT.Compile([]byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0})
	if err != nil {
		t.Fatalf("Runtime.Compile: %v", err)
	}
	if meta := mod.Metadata(); len(meta.Functions) != 0 || len(meta.Globals) != 0 || len(meta.Tables) != 0 {
		t.Fatalf("empty Module.Metadata = %+v", meta)
	}
	if err := moduleRT.Close(); err != nil {
		t.Fatalf("module runtime Close: %v", err)
	}
}

func TestValuePortableSurface(t *testing.T) {
	cases := []struct {
		value Value
		want  string
	}{
		{ValueI32(-1), "i32(-1)"},
		{ValueI64(-2), "i64(-2)"},
		{ValueF32(1.5), "f32(1.5)"},
		{ValueF64(2.5), "f64(2.5)"},
		{ValueOf(ValV128, 0), "v128(…)"},
		{ValueFuncRef(NullFuncRef()), "funcref(null)"},
		{ValueExternRef(NullExternRef()), "externref(null)"},
		{ValueOf(ValFuncRef, 1), "funcref(opaque)"},
		{ValueOf(ValExternRef, 1), "externref(opaque)"},
		{ValueOf(ValType(99), 0), "unknown(…)"},
	}
	for _, tc := range cases {
		if got := tc.value.String(); got != tc.want {
			t.Errorf("Value(%v).String() = %q, want %q", tc.value.Type(), got, tc.want)
		}
	}
	if v := ValueI32(-3); v.Type() != ValI32 || v.I32() != -3 || v.Bits() != I32(-3) {
		t.Fatalf("i32 value = %+v", v)
	}
	if v := ValueI64(-4); v.Type() != ValI64 || v.I64() != -4 {
		t.Fatalf("i64 value = %+v", v)
	}
	if v := ValueF32(3.25); v.Type() != ValF32 || v.F32() != 3.25 {
		t.Fatalf("f32 value = %+v", v)
	}
	if v := ValueF64(4.5); v.Type() != ValF64 || v.F64() != 4.5 {
		t.Fatalf("f64 value = %+v", v)
	}
	if v := ValueOf(ValFuncRef, 7); v.FuncRef().token != 7 {
		t.Fatalf("funcref = %+v", v.FuncRef())
	}
	if v := ValueOf(ValExternRef, 8); v.ExternRef().token != 8 {
		t.Fatalf("externref = %+v", v.ExternRef())
	}
}

func TestHostGlobalConstructorsAndCompiledGlobalMetadata(t *testing.T) {
	vec := V128{1, 2, 3, 4, 5}
	for _, tc := range []struct {
		name string
		g    *Global
		bits uint64
	}{
		{"i32", NewGlobalI32(-3, true), I32(-3)},
		{"i64", NewGlobalI64(-4, true), I64(-4)},
		{"f32", NewGlobalF32(1.5, true), F32(1.5)},
		{"f64", NewGlobalF64(2.5, true), F64(2.5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.g.Close()
			if got := tc.g.Get(); got != tc.bits {
				t.Fatalf("Get = %#x, want %#x", got, tc.bits)
			}
		})
	}
	v := NewGlobalV128(vec, true)
	defer v.Close()
	if got := v.GetV128(); got != vec {
		t.Fatalf("GetV128 = %#v, want %#v", got, vec)
	}
	updated := V128{9, 8, 7}
	if err := v.SetV128(updated); err != nil || v.GetV128() != updated {
		t.Fatalf("SetV128 = %v, value %#v", err, v.GetV128())
	}
	immutable := NewGlobalI32(0, false)
	defer immutable.Close()
	if err := immutable.SetV128(vec); err == nil {
		t.Fatal("SetV128 accepted immutable non-v128 global")
	}
	scalar := NewGlobalI32(0, true)
	defer scalar.Close()
	if err := scalar.Set(0x11223344); err != nil || scalar.Get() != 0x11223344 {
		t.Fatalf("Set scalar = %v, value %#x", err, scalar.Get())
	}
	if got := scalar.GetV128(); got[0] != 0x44 || got[1] != 0x33 || got[2] != 0x22 || got[3] != 0x11 {
		t.Fatalf("scalar GetV128 = %x", got)
	}
	if err := immutable.Set(1); err == nil {
		t.Fatal("Set accepted immutable global")
	}
	if err := v.Set(1); err == nil {
		t.Fatal("Set accepted v128 global")
	}
	ref := &Global{Type: ValExternRef, Mutable: true}
	if ref.Get() != 0 || ref.Set(1) == nil {
		t.Fatal("reference scalar global access changed")
	}
	if (*Global)(nil).GetV128() != (V128{}) {
		t.Fatal("nil GetV128 was non-zero")
	}

	c := &Compiled{GlobalImports: []GlobalImportDef{{}, {}}, Globals: []GlobalDef{{}, {}, {}}}
	if c.ImportedGlobalCount() != 2 || c.LocalGlobalCount() != 1 || c.GlobalSlot(3) != 24 {
		t.Fatalf("global metadata = imports %d locals %d slot %d", c.ImportedGlobalCount(), c.LocalGlobalCount(), c.GlobalSlot(3))
	}
}

func TestInstanceGlobalAndCodeBaseAPIs(t *testing.T) {
	vec := V128{9, 8, 7, 6}
	cell := NewGlobalV128(V128{}, true)
	defer cell.Close()
	in := &Instance{
		base: 0xfeed,
		c: &Compiled{
			Entry:         []int{4, 12},
			Globals:       []GlobalDef{{Type: ValV128, Mutable: true}, {Type: ValI32}},
			GlobalExports: map[string]int{"vec": 0, "fixed": 1},
			Exports:       map[string]int{"fn": 0},
		},
		globalCells: []*Global{cell, NewGlobalI32(1, false)},
	}
	defer in.globalCells[1].Close()

	base, entries := in.CodeBase()
	if base != in.base || len(entries) != 2 || entries[0] != 4 {
		t.Fatalf("CodeBase = %#x, %v", base, entries)
	}
	entries[0] = 99
	if in.c.Entry[0] != 4 {
		t.Fatal("CodeBase exposed compiled entry storage")
	}
	if err := in.SetGlobalV128("vec", vec); err != nil {
		t.Fatalf("SetGlobalV128: %v", err)
	}
	if got, err := in.GlobalV128("vec"); err != nil || got != vec {
		t.Fatalf("GlobalV128 = %x, %v", got, err)
	}
	for _, name := range []string{"missing", "fn", "fixed"} {
		if err := in.SetGlobalV128(name, vec); err == nil {
			t.Errorf("SetGlobalV128(%q) unexpectedly succeeded", name)
		}
	}
	if _, err := in.GlobalV128("fixed"); err == nil {
		t.Fatal("GlobalV128 accepted non-v128 global")
	}
}

func TestRuntimeNewFuncRefGlobalPortableBoundaries(t *testing.T) {
	if _, err := (*Runtime)(nil).NewFuncRefGlobal(NullFuncRef(), true); err == nil {
		t.Fatal("nil Runtime accepted NewFuncRefGlobal")
	}
	rt := NewRuntime()
	g, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal(null): %v", err)
	}
	if got, err := g.GetValue(); err != nil || !got.FuncRef().IsNull() {
		t.Fatalf("null global = %v, %v", got, err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("Close global: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if _, err := rt.NewFuncRefGlobal(NullFuncRef(), true); err == nil {
		t.Fatal("closed Runtime accepted NewFuncRefGlobal")
	}
}
