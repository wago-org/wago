//go:build linux && (amd64 || arm64)

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// signExtModule exports f(i32)->i32 = i32.extend8_s(local0).
func signExtModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xc0, 0x0b}))),
	)
}

// simdModule exports f() and uses v128.const/drop, enough to exercise 0xfd
// feature gating without requiring the public API to marshal a v128 result.
func simdModule() []byte {
	body := []byte{0x00, 0xfd, 0x0c}
	body = append(body, make([]byte, 16)...)
	body = append(body, 0x1a, 0x0b) // drop; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func simdI8x16AddModule(a, b V128) []byte {
	body := []byte{0xfd, 0x0c}
	body = append(body, a[:]...)
	body = append(body, 0xfd, 0x0c)
	body = append(body, b[:]...)
	body = append(body, 0xfd, 0x6e, 0x0b) // i8x16.add; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func slotsToV128(slots []uint64) V128 {
	var v V128
	if len(slots) >= 2 {
		binary.LittleEndian.PutUint64(v[0:8], slots[0])
		binary.LittleEndian.PutUint64(v[8:16], slots[1])
	}
	return v
}

func TestSIMDI8x16AddExec(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	a := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	b := V128{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	want := V128{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	c, err := Compile(nil, simdI8x16AddModule(a, b))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("f")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := slotsToV128(got); got != want {
		t.Fatalf("i8x16.add = % x, want % x", got, want)
	}
}

func v128BlockResultImmediateModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x7b, // block (result v128)
			0x00, // unreachable
			0x0b, // end block
			0x1a, // drop v128 result
			0x0b, // end function
		}))),
	)
}

func v128TypedSelectImmediateModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x00,       // unreachable (leaves the stack polymorphic)
			0x41, 0x01, // i32.const 1
			0x1c, 0x01, 0x7b, // select (result v128)
			0x1a, // drop v128 result
			0x0b, // end function
		}))),
	)
}

func v128ParamModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func v128ResultModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))), // unreachable; end
	)
}

func v128LocalModule() []byte {
	body := []byte{0x01, 0x01, wasm.MustEncodeValType(wasm.V128), 0x0b} // one v128 local; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func v128GlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "src", wasm.V128, false))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, []byte{0x23, 0x00, 0x0b}))), // global.get 0; end
	)
}

func v128FuncImportModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
	)
}

func TestConfigDefaultAcceptsSupportedFeatures(t *testing.T) {
	if _, err := Compile(nil, signExtModule()); err != nil {
		t.Fatalf("default config should accept sign-extension: %v", err)
	}
	if hostSupportsSIMD() {
		if _, err := Compile(nil, simdModule()); err != nil {
			t.Fatalf("default config should accept supported SIMD: %v", err)
		}
	}
	if _, err := Compile(nil, signExtModule()); err != nil {
		t.Fatalf("nil config should use defaults: %v", err)
	}
}

func TestConfigFeatureGatingRejects(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(platformCoreFeatures() &^ CoreFeatureSignExtensionOps)
	_, err := Compile(cfg, signExtModule())
	if err == nil || !strings.Contains(err.Error(), "sign-extension") {
		t.Fatalf("disabling sign-extension should reject the module, got %v", err)
	}

	cfg = NewRuntimeConfig().WithCoreFeatures(platformCoreFeatures() &^ CoreFeatureSIMD)
	_, err = Compile(cfg, simdModule())
	if err == nil || !strings.Contains(err.Error(), "simd disabled") {
		t.Fatalf("disabling SIMD should reject the module, got %v", err)
	}
}

func TestConfigValidationRejectsUnsupported(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatures(uint64(1)<<63), true)
	if _, err := Compile(cfg, signExtModule()); err == nil {
		t.Fatal("enabling an unknown feature bit should error")
	}
}

func TestEffectiveCompileBoundsModeZeroMemoryARM64Fallback(t *testing.T) {
	zeroLocal := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 0}}}}
	want := BoundsChecksSignalsBased
	if runtime.GOARCH == "arm64" {
		want = BoundsChecksExplicit
	}
	if got := effectiveCompileBoundsMode(BoundsChecksSignalsBased, zeroLocal); got != want {
		t.Fatalf("zero-minimum local memory mode = %v, want %v", got, want)
	}
	zeroImport := &wasm.Module{Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternMem, Mem: wasm.MemType{Limits: wasm.Limits{Min: 0}}}}}}
	if got := effectiveCompileBoundsMode(BoundsChecksSignalsBased, zeroImport); got != want {
		t.Fatalf("zero-minimum imported memory mode = %v, want %v", got, want)
	}
	for name, module := range map[string]*wasm.Module{
		"no memory":       {},
		"one-page memory": {Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}},
	} {
		if got := effectiveCompileBoundsMode(BoundsChecksSignalsBased, module); got != BoundsChecksSignalsBased {
			t.Errorf("%s mode = %v, want signals-based", name, got)
		}
	}
	if got := effectiveCompileBoundsMode(BoundsChecksExplicit, zeroLocal); got != BoundsChecksExplicit {
		t.Fatalf("explicit request changed to %v", got)
	}
}

func TestConfigSignalsBasedRequiresBuildTag(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	_, err := Compile(cfg, signExtModule())
	if guardPageBuilt {
		if err != nil {
			t.Fatalf("signals-based should compile under the build tag: %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "wago_guardpage") {
		t.Fatalf("signals-based without the tag should error, got %v", err)
	}
}

func TestConfigBoundsEnv(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "signals")
	cfg := NewRuntimeConfig()
	if cfg.BoundsChecks() != BoundsChecksSignalsBased {
		t.Fatalf("WAGO_BOUNDS=signals should select signals-based checks, got %v", cfg.BoundsChecks())
	}
}

func TestConfigImmutable(t *testing.T) {
	base := NewRuntimeConfig()
	baseMode := base.BoundsChecks() // default depends on the build tag; capture it
	derived := base.WithBoundsChecks(BoundsChecksSignalsBased).WithMemoryLimitPages(10)
	if base.BoundsChecks() != baseMode {
		t.Fatal("WithBoundsChecks mutated the base config")
	}
	if derived.BoundsChecks() != BoundsChecksSignalsBased {
		t.Fatal("derived config did not take the new bounds mode")
	}
}

func TestCoreFeaturesV2ReleaseScope(t *testing.T) {
	want := CoreFeaturesV1 |
		CoreFeatureBulkMemoryOperations |
		CoreFeatureMultiValue |
		CoreFeatureNonTrappingFloatToIntConversion |
		CoreFeatureReferenceTypes |
		CoreFeatureSignExtensionOps |
		CoreFeatureSIMD
	if CoreFeaturesV2 != want {
		t.Fatalf("CoreFeaturesV2 = %s, want WebAssembly 2.0 scope %s", CoreFeaturesV2, want)
	}
}

func TestCoreFeaturesV3ReleaseScopeAndAdmission(t *testing.T) {
	wasm3Only := CoreFeatureTailCall |
		CoreFeatureExtendedConstExpressions |
		CoreFeatureTypedFunctionReferences |
		CoreFeatureGC |
		CoreFeatureExceptionHandling |
		CoreFeatureMultiMemory |
		CoreFeatureMemory64 |
		CoreFeatureTable64
	if want := CoreFeaturesV2 | wasm3Only; CoreFeaturesV3 != want {
		t.Fatalf("CoreFeaturesV3 = %s, want mandatory WebAssembly 3.0 scope %s", CoreFeaturesV3, want)
	}
	if !CoreFeaturesV3.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("CoreFeaturesV3 must include the existing SIMD admission bit that also gates relaxed SIMD")
	}
	completeCore3Backend := runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
	for _, tc := range []struct {
		bit       CoreFeatures
		name      string
		supported bool
	}{
		{CoreFeatureTailCall, "tail-call", completeCore3Backend},
		{CoreFeatureExtendedConstExpressions, "extended-const-expressions", true},
		{CoreFeatureTypedFunctionReferences, "typed-function-references", completeCore3Backend},
		{CoreFeatureGC, "gc", completeCore3Backend},
		{CoreFeatureExceptionHandling, "exception-handling", completeCore3Backend},
		{CoreFeatureMultiMemory, "multi-memory", completeCore3Backend},
		{CoreFeatureMemory64, "memory64", completeCore3Backend},
		{CoreFeatureTable64, "table64", completeCore3Backend},
	} {
		if got := SupportedFeatures().IsEnabled(tc.bit); got != tc.supported {
			t.Errorf("SupportedFeatures admission for %s = %v, want %v", tc.name, got, tc.supported)
		}
		if got := tc.bit.String(); got != tc.name {
			t.Errorf("%#x String() = %q, want %q", uint64(tc.bit), got, tc.name)
		}
	}

	err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Validate()
	if completeCore3Backend {
		if err != nil {
			t.Fatalf("CoreFeaturesV3 Validate = %v, want complete admission", err)
		}
	} else {
		var unsupported *UnsupportedFeatureError
		if !errors.As(err, &unsupported) {
			t.Fatalf("CoreFeaturesV3 Validate = %v, want platform UnsupportedFeatureError", err)
		}
		if unsupported.Requested != CoreFeaturesV3&^SupportedFeatures() {
			t.Fatalf("unsupported Core 3 features = %s, want %s", unsupported.Requested, CoreFeaturesV3&^SupportedFeatures())
		}
	}
}

func TestCoreFeaturesBitset(t *testing.T) {
	if !CoreFeaturesV2.IsEnabled(CoreFeatureSignExtensionOps) {
		t.Fatal("V2 should include sign-extension")
	}
	on := CoreFeaturesV1.SetEnabled(CoreFeatureSIMD, true)
	if !on.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SetEnabled(true) failed")
	}
	if CoreFeaturesV1.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SetEnabled must not mutate the receiver")
	}
	if off := on.SetEnabled(CoreFeatureSIMD, false); off.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SetEnabled(false) failed")
	}
}

func TestConfigTypedErrors(t *testing.T) {
	// Unsupported feature -> *UnsupportedFeatureError naming it.
	unknown := CoreFeatures(uint64(1) << 63)
	_, err := NewRuntimeConfig().WithFeature(unknown, true).Compile(signExtModule())
	var ufe *UnsupportedFeatureError
	if !errors.As(err, &ufe) {
		t.Fatalf("want *UnsupportedFeatureError, got %T: %v", err, err)
	}
	if ufe.Requested != unknown {
		t.Fatalf("error should preserve unknown feature bit, got %#x", uint64(ufe.Requested))
	}
	// Signals-based without the build tag -> GuardPageUnavailableError (default build).
	if !guardPageBuilt {
		err = NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased).Validate()
		if !IsGuardPageUnavailable(err) {
			t.Fatalf("want GuardPageUnavailableError, got %v", err)
		}
	}
}

func TestConfigValidateAndIntrospection(t *testing.T) {
	if err := NewRuntimeConfig().Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	if err := NewRuntimeConfig().WithFunctionWorkers(-1).Validate(); err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("negative function workers should fail validation, got %v", err)
	}
	workers := NewRuntimeConfig().WithFunctionWorkers(4)
	if workers.FunctionWorkers() != 4 || NewRuntimeConfig().FunctionWorkers() != 1 {
		t.Fatal("WithFunctionWorkers must be immutable and observable; default must remain serial")
	}
	if got := NewRuntimeConfig().WithCompileWorkers(3); got.CompileWorkers() != 3 || got.FunctionWorkers() != 3 {
		t.Fatal("deprecated compile-worker aliases must preserve the function-worker policy")
	}
	wantFeatures := coreFeaturesWago
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		wantFeatures &^= CoreFeatureTailCall |
			CoreFeatureTypedFunctionReferences |
			CoreFeatureGC |
			CoreFeatureExceptionHandling |
			CoreFeatureMultiMemory |
			CoreFeatureMemory64 |
			CoreFeatureTable64
	}
	if !hostSupportsSIMD() {
		wantFeatures &^= CoreFeatureSIMD
	}
	if SupportedFeatures() != wantFeatures {
		t.Fatal("SupportedFeatures mismatch")
	}
	if GuardPageSupported() != guardPageBuilt {
		t.Fatal("GuardPageSupported should mirror the build tag")
	}
	// String is non-empty / informative. The default bounds mode depends on the
	// build tag (explicit normally, signals-based under wago_guardpage).
	if s := NewRuntimeConfig().String(); (!strings.Contains(s, "explicit") && !strings.Contains(s, "signals-based")) || !strings.Contains(s, "functionWorkers: 1") {
		t.Fatalf("config String missing bounds mode or serial default policy: %q", s)
	}
}

func TestFunctionWorkersImportedCodeAndSerialization(t *testing.T) {
	producerCode := MustCompile(benchAddOneModule())
	defer producerCode.Close()
	producer, err := Instantiate(producerCode, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer producer.Close()
	f, err := producer.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export producer function: %v", err)
	}

	mod := benchImportedModule(64, 16)
	imports := Imports{"env.f": f}
	compile := func(workers int) *Compiled {
		t.Helper()
		c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(workers).Compile(mod)
		if err != nil {
			t.Fatalf("workers=%d compile: %v", workers, err)
		}
		if !c.dynamicImports || len(c.Code) == 0 {
			t.Fatalf("workers=%d dynamic=%v code=%d", workers, c.dynamicImports, len(c.Code))
		}
		if err := c.validateImportBindings(imports, nil); err != nil {
			_ = c.Close()
			t.Fatalf("workers=%d bindings: %v", workers, err)
		}
		return c
	}
	serial := compile(1)
	defer serial.Close()
	parallel := compile(8)
	defer parallel.Close()
	if !bytes.Equal(parallel.Code, serial.Code) || !bytes.Equal(intSliceBytes(parallel.Entry), intSliceBytes(serial.Entry)) || !bytes.Equal(intSliceBytes(parallel.InternalEntry), intSliceBytes(serial.InternalEntry)) {
		t.Fatal("parallel codegen differs from serial output")
	}
	serialBlob, err := serial.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parallelBlob, err := parallel.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parallelBlob, serialBlob) {
		t.Fatal("function-worker policy changed serialized output")
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(parallelBlob); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !loaded.dynamicImports {
		t.Fatal("serialized imported module lost dynamic dispatch metadata")
	}
}

func intSliceBytes(v []int) []byte {
	out := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[8*i:], uint64(x))
	}
	return out
}

func TestFunctionWorkersForModulePolicy(t *testing.T) {
	module := func(funcs, bodyBytes int) *wasm.Module {
		m := &wasm.Module{Code: make([]wasm.Func, funcs)}
		remaining := bodyBytes
		for i := range m.Code {
			n := 0
			if left := funcs - i; left > 0 {
				n = remaining / left
			}
			m.Code[i].BodyBytes = make([]byte, n)
			remaining -= n
		}
		return m
	}
	capWant := func(w, funcs int) int {
		if w > runtime.GOMAXPROCS(0) {
			w = runtime.GOMAXPROCS(0)
		}
		if w > funcs {
			w = funcs
		}
		if w <= 1 {
			return 1
		}
		return w
	}
	if got := functionWorkersForModule(module(2, 9), 0); got != 1 {
		t.Fatalf("tiny auto workers = %d, want serial", got)
	}
	if got, want := functionWorkersForModule(module(301, 2053), 0), capWant(4, 301); got != want {
		t.Fatalf("many-functions auto workers = %d, want %d", got, want)
	}
	if got, want := functionWorkersForModule(module(658, 234408), 0), capWant(4, 658); got != want {
		t.Fatalf("lua-tier auto workers = %d, want %d", got, want)
	}
	if got, want := functionWorkersForModule(module(2831, 798392), 0), capWant(4, 2831); got != want {
		t.Fatalf("sqlite-tier auto workers = %d, want %d", got, want)
	}
	if got, want := functionWorkersForModule(module(10, 10), 3), capWant(3, 10); got != want {
		t.Fatalf("forced maximum workers = %d, want %d", got, want)
	}
}

func TestConfigRejectsSIMDWhenHostUnsupported(t *testing.T) {
	old := simdHostFeaturesSupported
	simdHostFeaturesSupported = func() bool { return false }
	defer func() { simdHostFeaturesSupported = old }()
	if _, err := Compile(nil, signExtModule()); err != nil {
		t.Fatalf("non-SIMD module should still compile when host SIMD is unavailable: %v", err)
	}
	_, err := Compile(nil, simdModule())
	if err == nil || !strings.Contains(err.Error(), "simd disabled") {
		t.Fatalf("SIMD module should be rejected when host SIMD is unavailable, got %v", err)
	}
	if SupportedFeatures().IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SupportedFeatures should clear SIMD when host SIMD is unavailable")
	}
}

func TestConfigRejectsV128TypesWhenHostUnsupported(t *testing.T) {
	old := simdHostFeaturesSupported
	simdHostFeaturesSupported = func() bool { return false }
	defer func() { simdHostFeaturesSupported = old }()

	cases := []struct {
		name string
		mod  []byte
	}{
		{"param", v128ParamModule()},
		{"result", v128ResultModule()},
		{"local", v128LocalModule()},
		{"global", v128GlobalModule()},
		{"func import", v128FuncImportModule()},
		{"block result immediate", v128BlockResultImmediateModule()},
		{"typed select immediate", v128TypedSelectImmediateModule()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(nil, tc.mod)
			if err == nil || !strings.Contains(err.Error(), "v128") {
				t.Fatalf("v128 module should be rejected when host SIMD is unavailable, got %v", err)
			}
		})
	}
}

func TestConfigCompileMethod(t *testing.T) {
	if _, err := NewRuntimeConfig().Compile(signExtModule()); err != nil {
		t.Fatalf("fluent Compile: %v", err)
	}
}

func TestConfigWithFeatures(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeatures(CoreFeatureMutableGlobal, CoreFeatureSignExtensionOps)
	if !cfg.CoreFeatures().IsEnabled(CoreFeatureMutableGlobal) ||
		!cfg.CoreFeatures().IsEnabled(CoreFeatureSignExtensionOps) {
		t.Fatalf("WithFeatures did not set the union: %v", cfg.CoreFeatures())
	}
	if cfg.CoreFeatures().IsEnabled(CoreFeatureBulkMemoryOperations) {
		t.Fatal("WithFeatures should replace, not add to, the set")
	}
}
