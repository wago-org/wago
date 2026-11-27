// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBuildProfileLibrary(t *testing.T) {
	if !buildfeatures.Custom {
		t.Skip("requires a feature-trimmed build")
	}
	if got := NewRuntimeConfig().CoreFeatures() &^ SupportedFeatures(); got != 0 {
		t.Fatalf("default configuration enables unsupported features: %s", got)
	}
	for _, feature := range FeatureInfos() {
		if feature.Feature&CoreFeatures(buildfeatures.Enabled) != 0 {
			continue
		}
		cfg := NewRuntimeConfig().WithFeature(feature.Feature, true)
		var unsupported *UnsupportedFeatureError
		if feature.Available || !errors.As(cfg.Validate(), &unsupported) || !unsupported.Requested.IsEnabled(feature.Feature) {
			t.Fatalf("feature %s admitted by a build without it", feature.Name)
		}
	}
	c, err := Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	values, err := in.Invoke("f", I32(41))
	if err != nil || len(values) != 1 || AsI32(values[0]) != 42 {
		t.Fatalf("Invoke = %v, %v", values, err)
	}
}

func TestBuildProfileBoundaryRoots(t *testing.T) {
	var state gcPublicState
	var wantArguments uintptr
	if wasmGCBuilt || ownedExceptionReferencesRuntime() {
		wantArguments = unsafe.Sizeof(gcArgumentRoots{})
	}
	if size := unsafe.Sizeof(state.gcArgumentRootState); size != wantArguments || (state.argumentRoots() != nil) != (wantArguments != 0) {
		t.Fatalf("argument root state has size %d and available storage %t, want size %d", size, state.argumentRoots() != nil, wantArguments)
	}
	var wantClone uintptr
	if wasmGCBuilt {
		wantClone = unsafe.Sizeof(gcCloneRoot{})
	}
	if size := unsafe.Sizeof(state.gcCloneRootState); size != wantClone || (state.cloneRoot() != nil) != (wantClone != 0) {
		t.Fatalf("clone root state has size %d and available storage %t, want size %d", size, state.cloneRoot() != nil, wantClone)
	}
}

func TestBuildProfileObjectTokenIndex(t *testing.T) {
	var store referenceStore
	var want uintptr
	supported := wasmGCBuilt || ownedExceptionReferencesRuntime()
	if supported {
		want = unsafe.Sizeof(map[uint64]gcRefTokenEntry(nil))
	}
	if size := unsafe.Sizeof(store.gcObjectTokenIndex); size != want {
		t.Fatalf("GC object-token index uses %d bytes, want %d", size, want)
	}
	if store.objectTokensLocked() != nil || store.hasObjectTokensLocked() || store.hasObjectTokenLocked(1) {
		t.Fatal("unused GC object-token index has entries")
	}
	if !supported {
		return
	}
	root := gcResultRoot{token: 1, slot: 3, ownerIndex: 2, owner: &Instance{}}
	store.insertObjectTokenLocked(gcRefTokenEntry{gcResultRoot: root})
	if !store.hasObjectTokensLocked() || !store.hasObjectTokenLocked(1) || store.hasObjectTokenLocked(2) {
		t.Fatal("GC object-token lookup did not find exactly the registered token")
	}
	entries := store.takeObjectTokenRootsLocked()
	if len(entries) != 1 || entries[0] != root || store.objectTokensLocked() != nil {
		t.Fatal("draining GC object tokens did not transfer exactly the registered token")
	}
}

func TestBuildProfileCompactExternReleaseBatch(t *testing.T) {
	var want uintptr
	if buildfeatures.NativeRISCV64Collector {
		want = unsafe.Sizeof([]any(nil))
	}
	if size := unsafe.Sizeof(compactExternReleaseBatch{}); size != want {
		t.Fatalf("compact externref release batch uses %d bytes, want %d", size, want)
	}
}

func TestBuildProfileCompiledGCProducts(t *testing.T) {
	var c Compiled
	var cache compiledCodeCache
	var want uintptr
	if wasmGCBuilt {
		want = unsafe.Sizeof(compiledGCProducts{})
	}
	if size := unsafe.Sizeof(cache.compiledGCProductState); size != want {
		t.Fatalf("compiled GC product state uses %d bytes, want %d", size, want)
	}

	cache.setGCProducts(compiledGCProducts{gcStructProduct: stagedGCStructGeneric})
	c.codeCache = &cache
	if got := c.usesGenericGCExecution(); got != wasmGCBuilt {
		t.Fatalf("generic GC execution available = %t, want %t", got, wasmGCBuilt)
	}
	if got := cache.stagedFeatures.IsEnabled(CoreFeatureGC); got != wasmGCBuilt {
		t.Fatalf("cached GC product requires GC = %t, want %t", got, wasmGCBuilt)
	}
}

func TestBuildProfileGCFrameCodeDirectory(t *testing.T) {
	var want uintptr
	if nativeCollectorFrameRootsBuilt {
		want = 2 * unsafe.Sizeof(uintptr(0))
	}
	if got := unsafe.Sizeof(gcDomainTopology{}.gcFrameCodeDirectory); got != want {
		t.Fatalf("frame code directory = %d bytes, want %d", got, want)
	}
}

func TestBuildProfileStoreGCDomainState(t *testing.T) {
	var store referenceStore
	var want uintptr
	if nativeCollectorFrameRootsBuilt {
		want = unsafe.Sizeof((*gcDomainTopology)(nil))
	}
	if size := unsafe.Sizeof(store.gcDomainStoreState); size != want {
		t.Fatalf("store GC domain state uses %d bytes, want %d", size, want)
	}
	if store.gcTopologyLocked() != nil {
		t.Fatal("new store allocated a collector topology")
	}
	topology := store.ensureGCTopology()
	if (topology != nil) != (want != 0) || store.ensureGCTopology() != topology {
		t.Fatal("collector topology availability or identity disagrees with build profile")
	}
}

func TestBuildProfileCollectorReleaseBatch(t *testing.T) {
	var want uintptr
	if nativeCollectorFrameRootsBuilt {
		want = unsafe.Sizeof([]*gc.Collector(nil))
	}
	if size := unsafe.Sizeof(gcReleaseBatch{}); size != want {
		t.Fatalf("collector release batch uses %d bytes, want %d", size, want)
	}
}

func TestBuildProfileGCResultReleaseBatch(t *testing.T) {
	var want uintptr
	if collectorObjectTokensBuilt {
		want = unsafe.Sizeof([]gcResultRoot(nil))
	}
	if size := unsafe.Sizeof(gcResultReleaseBatch{}); size != want {
		t.Fatalf("collector result release batch uses %d bytes, want %d", size, want)
	}
}

func TestBuildProfileGCInstanceRegistration(t *testing.T) {
	var entry referenceStoreInstance
	var wantState uintptr
	wantEntry := unsafe.Sizeof([3]bool{})
	supported := nativeCollectorFrameRootsBuilt
	if supported {
		wantState = unsafe.Sizeof(gcInvocationRegistration{})
		wantEntry = unsafe.Sizeof(struct {
			gcInvocationRegistration
			flags [3]bool
		}{})
	}
	if size := unsafe.Sizeof(entry.gcInstanceRegistration); size != wantState {
		t.Fatalf("collector invocation registration uses %d bytes, want %d", size, wantState)
	}
	if size := unsafe.Sizeof(entry); size != wantEntry {
		t.Fatalf("reference store instance entry uses %d bytes, want %d", size, wantEntry)
	}
	if (entry.gcRegistration() != nil) != supported {
		t.Fatal("collector invocation registration availability disagrees with profile")
	}
}

func TestBuildProfileInstanceBuilderGCState(t *testing.T) {
	var builder instanceBuilder
	var want uintptr
	supported := nativeCollectorFrameRootsBuilt
	if supported {
		want = unsafe.Sizeof(instanceCollectorState{})
	}
	if size := unsafe.Sizeof(builder.instanceBuilderGCState); size != want {
		t.Fatalf("instance builder collector state uses %d bytes, want %d", size, want)
	}
	state := builder.collectorBuildState()
	if (state != nil) != supported || state != builder.collectorBuildState() {
		t.Fatal("instance builder collector state availability or identity disagrees with profile")
	}
}

func TestBuildProfileInstanceCollectorMetadata(t *testing.T) {
	var want uintptr
	supported := nativeCollectorFrameRootsBuilt
	if supported {
		want = 2 * unsafe.Sizeof(uintptr(0))
	}
	if size := unsafe.Sizeof(Instance{}.instanceCollectorMetadata); size != want {
		t.Fatalf("instance collector metadata uses %d bytes, want %d", size, want)
	}
	var wantCache uintptr
	if supported {
		wantCache = unsafe.Sizeof(uintptr(0))
	}
	if size := unsafe.Sizeof(compiledCacheIndexes{}.compiledGCTypeMappingState); size != wantCache {
		t.Fatalf("compiled GC mapping cache uses %d bytes, want %d", size, wantCache)
	}

	var empty Instance
	if empty.gcTypeMap() != nil || empty.gcNativeView() != nil {
		t.Fatal("empty instance unexpectedly has collector metadata")
	}
	mapping := new(gcTypeMapping)
	view := new(gc.NativeInstanceView)
	in := Instance{instanceCollectorMetadata: newInstanceCollectorMetadata(mapping, view)}
	if supported {
		if in.gcTypeMap() != mapping || in.gcNativeView() != view {
			t.Fatal("instance did not retain its collector metadata")
		}
	} else if in.gcTypeMap() != nil || in.gcNativeView() != nil {
		t.Fatal("collector-free build retained supplied collector metadata")
	}
}

func TestBuildProfileGCInvocationRecords(t *testing.T) {
	supported := nativeCollectorFrameRootsBuilt
	if (unsafe.Sizeof(gcInvocationLease{}) != 0) != supported ||
		(unsafe.Sizeof(gcInvocationSuspension{}) != 0) != supported {
		t.Fatal("collector invocation record layout disagrees with build profile")
	}
	var in Instance
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if lease, err := in.lockGCInvocationContext(ctx, 0); !errors.Is(err, context.Canceled) || lease.isAcquired() {
		t.Fatalf("canceled collector admission = %+v, %v", lease, err)
	}
}

func TestBuildProfileTerminalCloseCollection(t *testing.T) {
	var want uintptr
	if terminalDependencyCollectionSupported {
		want = 12
	}
	if size := unsafe.Sizeof(instanceCloseState{}.terminalCloseCollection); size != want {
		t.Fatalf("terminal close collection state uses %d bytes, want %d", size, want)
	}

	var state instanceCloseState
	expected := errors.New("logical close result")
	if err := (*Instance)(nil).finishLogicalClose(&state, expected); err != expected || !state.completed.Load() || state.result != expected {
		t.Fatalf("logical close publication = %v, completed = %t, stored = %v", err, state.completed.Load(), state.result)
	}
}

func TestBuildProfilePublicGCState(t *testing.T) {
	supported := nativeCollectorFrameRootsBuilt
	var wantPointer, wantToken uintptr
	if supported {
		wantPointer = unsafe.Sizeof(uintptr(0))
	}
	if nativeCollectorFrameRootsBuilt {
		wantToken = 2 * unsafe.Sizeof(uintptr(0))
	}
	if size := unsafe.Sizeof(instancePluginState{}.pluginPublicGCState); size != wantPointer {
		t.Fatalf("plugin public GC state uses %d bytes, want %d", size, wantPointer)
	}
	if size := unsafe.Sizeof(gcHostActivationToken{}); size != wantToken {
		t.Fatalf("parked host activation token uses %d bytes, want %d", size, wantToken)
	}

	var in Instance
	if in.existingPublicGCState() != nil || in.pluginState.Load() != nil {
		t.Fatal("zero instance unexpectedly has public GC state")
	}
	state := in.publicGCState()
	if (state != nil) != supported || in.existingPublicGCState() != state || in.publicGCState() != state {
		t.Fatal("public GC state availability or identity disagrees with profile")
	}
	if !supported && in.pluginState.Load() != nil {
		t.Fatal("collector-free build allocated plugin state for GC")
	}
}

func TestBuildProfileExceptionTagState(t *testing.T) {
	var wantState, wantImports uintptr
	if buildfeatures.ExceptionHandling != 0 {
		wantState = 2 * unsafe.Sizeof(uintptr(0))
		wantImports = unsafe.Sizeof(importDedup[*Tag]{})
	}
	if got := unsafe.Sizeof(instancePluginState{}.instanceExceptionTagState); got != wantState {
		t.Fatalf("exception tag state uses %d bytes, want %d", got, wantState)
	}
	if got := unsafe.Sizeof(instanceBuilder{}.tagAttachments); got != wantImports {
		t.Fatalf("exception import attachments use %d bytes, want %d", got, wantImports)
	}

	var in Instance
	if tag, err := in.ExportedTag("missing"); tag != nil || err == nil || in.pluginState.Load() != nil {
		t.Fatalf("missing tag lookup created a handle or plugin state: tag=%v err=%v", tag, err)
	}
}

func TestBuildProfileCompiledExceptionMetadata(t *testing.T) {
	var directory compiledMemoryDirectory
	var want uintptr
	if buildfeatures.ExceptionHandling != 0 {
		want = unsafe.Sizeof([]compiledTagDef(nil)) + unsafe.Sizeof(map[string]int(nil))
	}
	if size := unsafe.Sizeof(directory.compiledExceptionTagMetadata); size != want {
		t.Fatalf("compiled exception tag metadata uses %d bytes, want %d", size, want)
	}

	var writer compiledWriter
	writer.tags(&Compiled{memoryDir: &directory})
	if len(writer.buf) != 2 || writer.buf[0] != 0 || writer.buf[1] != 0 {
		t.Fatalf("empty tag contract = %x, want two zero counts", writer.buf)
	}
	if want != 0 {
		return
	}
	var unsupported *UnsupportedFeatureError
	if err := directory.appendExceptionTag(compiledTagDef{}); !errors.As(err, &unsupported) || unsupported.Requested != CoreFeatureExceptionHandling {
		t.Fatalf("disabled tag storage returned %v, want unsupported exception handling", err)
	}
	// A module with a single empty function type and a local tag of that type.
	source := []byte{0, 'a', 's', 'm', 1, 0, 0, 0, 1, 4, 1, 0x60, 0, 0, 13, 3, 1, 0, 0}
	if c, err := Compile(source); err == nil {
		c.Close()
		t.Fatal("compiled a tag declaration with exception handling disabled")
	}
}

func TestBuildProfileCompiledGCInitializers(t *testing.T) {
	var directory compiledMemoryDirectory
	var wantGC, wantTags uintptr
	if wasmGCBuilt {
		wantGC = unsafe.Sizeof([]gcStructGlobalInit(nil)) + unsafe.Sizeof([]gcArrayGlobalInit(nil)) + unsafe.Sizeof((*gcArrayElementInit)(nil))
	}
	if buildfeatures.ExceptionHandling != 0 {
		wantTags = unsafe.Sizeof([]compiledTagDef(nil)) + unsafe.Sizeof(map[string]int(nil))
	}
	if size := unsafe.Sizeof(directory.compiledGCInitializers); size != wantGC {
		t.Fatalf("compiled GC initializer storage uses %d bytes, want %d", size, wantGC)
	}
	base := unsafe.Sizeof(struct {
		defs    []memoryDef
		exports map[string]int
		flags   [3]bool
	}{})
	if size, want := unsafe.Sizeof(directory), base+wantGC+wantTags; size != want {
		t.Fatalf("compiled memory directory uses %d bytes, want %d", size, want)
	}
	if directory.hasGCInitializers() {
		t.Fatal("empty memory directory has GC initializers")
	}
}

func TestBuildProfileRejectsSIMD(t *testing.T) {
	if buildfeatures.SIMD != 0 {
		t.Skip("requires a build without SIMD")
	}
	// (module (func v128.const i32x4 0 0 0 0 drop))
	source := []byte{0, 0x61, 0x73, 0x6d, 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0, 3, 2, 1, 0,
		10, 23, 1, 21, 0, 0xfd, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, 0x0b}
	compiled, err := Compile(source)
	if compiled != nil || err == nil || !strings.Contains(err.Error(), "simd is disabled in this build") {
		t.Fatalf("Compile SIMD = %v, %v", compiled, err)
	}
}

func TestBuildProfileRejectsMutableGlobalBoundaries(t *testing.T) {
	if buildfeatures.MutableGlobal != 0 {
		t.Skip("requires a build without mutable global imports or exports")
	}
	modules := map[string][]byte{
		"import": wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "cell", wasm.I32, true),
		))),
		"export": wasmtest.Module(
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
	}
	for name, source := range modules {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(source)
			if c != nil {
				defer c.Close()
			}
			var unsupported *UnsupportedFeatureError
			if c != nil || !errors.As(err, &unsupported) || !unsupported.Requested.IsEnabled(CoreFeatureMutableGlobal) {
				t.Fatalf("Compile mutable global %s = %v, %v", name, c, err)
			}
		})
	}
	t.Run("local", func(t *testing.T) {
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set_and_read", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x24, 0, 0x23, 0, 0x0b}))),
		)
		c, err := Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("set_and_read")
		if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("local mutable global = %v, %v", got, err)
		}
	})
}

func TestBuildProfileRejectsRV64ReferenceABI(t *testing.T) {
	if compiledArchitecture != 4 || referenceSignaturesBuilt {
		t.Skip("requires a RISC-V build without reference signatures")
	}
	for name, compiled := range map[string]*Compiled{
		"function": {nativeReferenceFunctionABI: true},
		"call":     {nativeReferenceCallABI: true},
		"control":  {nativeReferenceControlABI: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := railshotValidateCompiled(compiled)
			if err == nil || !strings.Contains(err.Error(), "reference ABI is disabled in this build") {
				t.Fatalf("validate reference ABI = %v", err)
			}
		})
	}
}

func TestV128InitializerBuildFeature(t *testing.T) {
	vector := V128{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	body := append(append([]byte{0xfd, 12}, vector[:]...), 0x0b)
	got, bytesErr := evalConstExprBytes(body, wasm.V128)
	ast := wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrV128Const}}}
	fromAST, astErr := evalConstExprWithContext(ast, wasm.V128, nil)
	c := &Compiled{Globals: []GlobalDef{{Type: ValV128}}}
	want := ValueTypeDescriptor{Kind: ValueTypeV128}
	validatorErr := c.validateGCConstExpr(body, want, 1)
	if buildfeatures.SIMD == 0 {
		global := NewGlobalV128(vector, false)
		defer global.Close()
		_, literalErr := evalCompiledGCConstExpr(body, nil, nil, nil, c, nil, 0, nil, nil)
		copyBody := []byte{0x23, 0, 0x0b}
		_, copyErr := evalCompiledGCConstExpr(copyBody, nil, nil, nil, c, []*Global{global}, 1, nil, nil)
		copyValidationErr := c.validateGCConstExpr(copyBody, want, 1)
		for _, err := range []error{bytesErr, astErr, validatorErr, literalErr, copyErr, copyValidationErr} {
			var unsupported *UnsupportedFeatureError
			if !errors.As(err, &unsupported) || unsupported.Requested != CoreFeatureSIMD {
				t.Fatalf("vector initializer error = %v, want unavailable SIMD", err)
			}
		}
		return
	}
	if bytesErr != nil || got.v128 != vector {
		t.Fatalf("byte-backed vector initializer = %x, error %v, want %x", got.v128, bytesErr, vector)
	}
	if astErr != nil || fromAST.v128 != (V128{}) || fromAST.vtype != wasm.V128 {
		t.Fatalf("AST vector initializer = %x, type %s, error %v", fromAST.v128, fromAST.vtype, astErr)
	}
	if validatorErr != nil {
		t.Fatalf("validate GC vector operand: %v", validatorErr)
	}
}

func TestReferenceInitializerBuildFeatures(t *testing.T) {
	m, err := wasm.DecodeModule([]byte{0, 0x61, 0x73, 0x6d, 1, 0, 0, 0, 1, 4, 1, 0x60, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	indexed := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false))
	for _, tc := range []struct {
		name    string
		body    []byte
		typ     wasm.ValType
		feature CoreFeatures
	}{
		{"null-func", []byte{0xd0, 0x70, 0x0b}, wasm.FuncRef, 0},
		{"null-extern", []byte{0xd0, 0x6f, 0x0b}, wasm.ExternRef, 0},
		{"null-gc", []byte{0xd0, 0x6c, 0x0b}, wasm.RefVal(wasm.AbsRef(wasm.HeapI31)), CoreFeatureGC},
		{"null-bottom", []byte{0xd0, 0x73, 0x0b}, wasm.RefVal(wasm.AbsRef(wasm.HeapNoFunc)), CoreFeatureGC},
		{"null-exn", []byte{0xd0, 0x69, 0x0b}, wasm.RefVal(wasm.AbsRef(wasm.HeapExn)), CoreFeatureExceptionHandling},
		{"null-indexed", []byte{0xd0, 0, 0x0b}, indexed, CoreFeatureTypedFunctionReferences},
		{"ref-func", []byte{0xd2, 0, 0x0b}, wasm.FuncRef, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evalConstExprBytesWithModule(tc.body, tc.typ, m)
			required := tc.feature
			if !referenceSignaturesBuilt {
				required = CoreFeatureReferenceTypes
			}
			if required&^CoreFeatures(buildfeatures.Enabled) != 0 {
				var unsupported *UnsupportedFeatureError
				if !errors.As(err, &unsupported) || unsupported.Requested != required {
					t.Fatalf("initializer error = %v, want unavailable %s", err, required)
				}
				return
			}
			if err != nil || !wasm.EqualValType(got.vtype, tc.typ) {
				t.Fatalf("initializer type = %s, error %v, want %s", got.vtype, err, tc.typ)
			}
			if tc.name == "ref-func" && got.FuncIndex != 0 {
				t.Fatalf("initializer function index = %d, want 0", got.FuncIndex)
			}
		})
	}
}

func TestBuildProfileCompiledGCFrameRootState(t *testing.T) {
	roots := &compiledGCFrameRoots{}
	c := &Compiled{validateMemo: &validateMemo{compiledGCFrameRootState: testCompiledGCFrameRootState(roots)}}
	var wantSize uintptr
	var wantRoots *compiledGCFrameRoots
	if nativeCollectorFrameRootsBuilt {
		wantSize = unsafe.Sizeof(roots)
		wantRoots = roots
	}
	if size := unsafe.Sizeof(c.validateMemo.compiledGCFrameRootState); size != wantSize {
		t.Fatalf("compiled frame-root state = %d bytes, want %d", size, wantSize)
	}
	if got := c.genericGCFrameRoots(); got != wantRoots {
		t.Fatalf("compiled frame-root state returned %p, want %p", got, wantRoots)
	}
}

func TestBuildProfileRejectsGCImportSignature(t *testing.T) {
	if wasmGCBuilt {
		t.Skip("requires a build without Wasm GC")
	}
	anyref, _ := valueTypeDescriptorFromValType(ValAnyRef)
	compiled := &Compiled{
		NumImports:     1,
		Imports:        []string{"env.f"},
		importFuncSigs: []FuncSig{{Params: []ValType{ValAnyRef}, HasTypeIndex: true}},
		dynamicImports: true,
		Types:          []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Final: true, Params: []ValueTypeDescriptor{anyref}}},
	}
	var unsupported *UnsupportedFeatureError
	if err := compiled.validateMetadata(); !errors.As(err, &unsupported) || !unsupported.Requested.IsEnabled(CoreFeatureGC) {
		t.Fatalf("compiled GC import without a declared feature bit = %v, want unavailable GC", err)
	}
}

func TestBuildProfileCollectorRefMatching(t *testing.T) {
	if wasmGCBuilt {
		t.Skip("requires a build without Wasm GC")
	}
	var in Instance
	anyref, _ := valueTypeDescriptorFromValType(ValAnyRef)
	exnref, _ := valueTypeDescriptorFromValType(ValExnRef)
	if in.gcRefMatchesValueType(0, anyref) {
		t.Fatal("GC-disabled build matched a null anyref")
	}
	if got := in.gcRefMatchesValueType(0, exnref); got != ownedExceptionReferencesRuntime() {
		t.Fatalf("null exnref matched = %v with owned exceptions %v", got, ownedExceptionReferencesRuntime())
	}
}

func TestRootedReceiptCacheBuildProfile(t *testing.T) {
	want := uintptr(0)
	if rv64RootedReceiptsBuilt {
		want = unsafe.Sizeof((*rv64RootedReceipt)(nil))
	}
	if got := unsafe.Sizeof(rv64RootedReceiptCache{}); got != want {
		t.Fatalf("RV64 rooted receipt cache uses %d bytes, want %d", got, want)
	}

	c, err := Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.loadValidateMemo().rootedReceipt() != nil {
		t.Fatal("plain scalar module allocated a rooted receipt")
	}
}

func TestParkedHostRootCacheBuildProfile(t *testing.T) {
	var state gcPublicState
	tokensSupported := wasmGCBuilt || ownedExceptionReferencesRuntime()
	if collectorObjectTokensBuilt != tokensSupported {
		t.Fatal("parked host root cache and public object token support disagree")
	}
	want := uintptr(0)
	if tokensSupported {
		want = unsafe.Sizeof((*parkedHostRoots)(nil))
	}
	if got := unsafe.Sizeof(state.parkedHostRootCache); got != want {
		t.Fatalf("parked host root cache uses %d bytes, want %d", got, want)
	}
	if state.parkedRoots(0) != nil {
		t.Fatal("unused activation has a host root journal")
	}
	if tokensSupported {
		roots := state.ensureParkedRoots(0)
		if roots == nil || state.ensureParkedRoots(0) != roots || state.parkedRoots(1) != nil {
			t.Fatal("host root journal was not retained for its activation only")
		}
	}
}

func TestBuildProfileGCResultSlotState(t *testing.T) {
	var state gcPublicState
	size := unsafe.Sizeof(state.gcResultSlotState)
	slots := state.resultSlots()
	if collectorObjectTokensBuilt {
		if size != unsafe.Sizeof(gcResultSlots{}) || slots == nil {
			t.Fatalf("collector result slots missing: state=%d bytes, slots=%p", size, slots)
		}
	} else if size != 0 || slots != nil {
		t.Fatalf("disabled collector result slots: state=%d bytes, slots=%p", size, slots)
	}
	if slots.matches(0, 0, 0) {
		t.Fatal("empty collector result state matched a root")
	}
}

func TestBuildProfileGCNativeFrameState(t *testing.T) {
	var state gcPublicState
	ownerBytes := unsafe.Sizeof(state.frameRoots.owner)
	size := unsafe.Sizeof(state.frameRoots)
	if size < ownerBytes || (size > ownerBytes) != nativeCollectorFrameRootsBuilt {
		t.Fatalf("collector root set uses %d bytes with native frame roots enabled=%t", size, nativeCollectorFrameRootsBuilt)
	}
	overflow := unsafe.Sizeof(state.gcDomainHostRootScratch)
	if (overflow != 0) != (buildfeatures.NativeRISCV64Collector && nativeCollectorFrameRootsBuilt) {
		t.Fatalf("suspended host-owner scratch uses %d bytes in this profile", overflow)
	}
}

func TestBuildProfileGCHostActivationState(t *testing.T) {
	var state gcPublicState
	if size := unsafe.Sizeof(state.gcHostActivationState); (size != 0) != nativeCollectorFrameRootsBuilt {
		t.Fatalf("parked host activation state uses %d bytes with native frame roots enabled=%t", size, nativeCollectorFrameRootsBuilt)
	}
	if state.hasGCHostActivations() || state.hasGCHostActivation(0) {
		t.Fatal("new collector state has a parked host activation")
	}
}

func TestBuildProfileGCHostState(t *testing.T) {
	capability := unsafe.Sizeof(hostGCBridgeCapability{})
	binding := unsafe.Sizeof(hostGCBridgeBinding{})
	imports := unsafe.Sizeof(pluginGCImportState{})
	if !wasmGCBuilt {
		if capability != 0 || binding != 0 || imports != 0 {
			t.Fatalf("disabled GC host state uses %d/%d/%d bytes", capability, binding, imports)
		}
		return
	}
	var wantImports uintptr
	if pluginRuntimeBuilt {
		wantImports = unsafe.Sizeof(uintptr(0))
	}
	if capability != 1 || binding != unsafe.Sizeof(uintptr(0))+unsafe.Sizeof(uint64(0)) || imports != wantImports {
		t.Fatalf("GC host capability/binding/import state uses %d/%d/%d bytes", capability, binding, imports)
	}
}

func TestBuildProfileHostReferenceCallState(t *testing.T) {
	pointerBytes := unsafe.Sizeof(uintptr(0))
	want := pointerBytes
	if wasmGCBuilt {
		want += pointerBytes
	}
	var state hostReferenceCallState
	if got := unsafe.Sizeof(state); got != want {
		t.Fatalf("host reference call state uses %d bytes, want %d", got, want)
	}
	if state.gcResultTokens() != nil {
		t.Fatal("unused result journal must be empty")
	}
}

func TestBuildProfileGCGlobalRootState(t *testing.T) {
	sliceBytes := unsafe.Sizeof([]gcGlobalRootMapping(nil))
	var wantGeneric, wantStaged uintptr
	if wasmGCBuilt || ownedExceptionReferencesRuntime() {
		wantGeneric = sliceBytes
	}
	if wasmGCBuilt {
		wantStaged = sliceBytes
	}
	if got := unsafe.Sizeof(gcPublicState{}.gcGlobalRootState); got != wantGeneric {
		t.Fatalf("generic GC global root state uses %d bytes, want %d", got, wantGeneric)
	}
	if got := unsafe.Sizeof(instancePluginState{}.stagedGCGlobalRootState); got != wantStaged {
		t.Fatalf("staged GC global root state uses %d bytes, want %d", got, wantStaged)
	}
}

func TestBuildProfileStagedGCObjectState(t *testing.T) {
	var state instancePluginState
	var want uintptr
	if wasmGCBuilt {
		want = 2 * unsafe.Sizeof(uintptr(0))
	}
	if got := unsafe.Sizeof(state.stagedGCObjectState); got != want {
		t.Fatalf("staged GC object state uses %d bytes, want %d", got, want)
	}
	roots := state.stagedGCObjects()
	if (roots != nil) != wasmGCBuilt {
		t.Fatalf("staged GC object roots available = %t, GC built = %t", roots != nil, wasmGCBuilt)
	}
	if roots != nil && (roots.arrayElements.Load() != nil || roots.refTestTable.Load() != nil) {
		t.Fatal("unused staged GC object roots were allocated")
	}
}

func TestBuildProfileGCConstructorState(t *testing.T) {
	var state gcPublicState
	var wantArray, wantStruct uintptr
	if wasmGCBuilt {
		wantArray = unsafe.Sizeof(gcArrayConstructorScratch{})
	}
	structScratch := state.structInitializerScratch()
	structsBuilt := wasmGCBuilt || ownedExceptionReferencesRuntime()
	if structsBuilt {
		wantStruct = unsafe.Sizeof(*structScratch)
	}
	if got := unsafe.Sizeof(state.gcArrayConstructorState); got != wantArray {
		t.Fatalf("array constructor state uses %d bytes, want %d", got, wantArray)
	}
	if got := unsafe.Sizeof(state.gcStructConstructorState); got != wantStruct {
		t.Fatalf("struct constructor state uses %d bytes, want %d", got, wantStruct)
	}
	if (structScratch != nil) != structsBuilt || (state.arrayInitializerScratch() != nil) != wasmGCBuilt {
		t.Fatal("initializer scratch availability disagrees with build features")
	}
}

func TestCompiledFunctionGCDescriptorEncoding(t *testing.T) {
	descriptors := []gc.TypeDesc{
		{ID: 0, Kind: gc.KindFunc, Final: true},
		{ID: 1, Kind: gc.KindFunc},
	}
	var full, plain compiledWriter
	full.gcTypeDescs(descriptors)
	if err := plain.gcFunctionTypeDescs(descriptors); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.buf, full.buf) {
		t.Fatalf("function descriptor encoding = %x, want %x", plain.buf, full.buf)
	}

	for _, descriptor := range []gc.TypeDesc{
		{Kind: gc.KindFunc, Super: 1},
		{Kind: gc.KindFunc, HasSuper: true},
		{Kind: gc.KindFunc, Fields: []gc.FieldDesc{}},
		{Kind: gc.KindArray, Elem: gc.StorageI32},
	} {
		var writer compiledWriter
		if err := writer.gcFunctionTypeDescs([]gc.TypeDesc{descriptor}); err == nil {
			t.Fatalf("accepted descriptor requiring full encoding: %+v", descriptor)
		}
	}
}
