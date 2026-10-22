//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Reference-valued exports use callable descriptors. The identity wrapper
// transports an authenticated descriptor through the public token boundary.
func rv64PublicReferenceFunctionBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("id", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
		)),
	)
}

func rv64PublicReferenceFunctionCallBoundaryModule(result byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			[]byte{0x60, 1, 0x63, 0, 1, 0x7f}, // (ref null type 0) -> i32
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, result, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func rv64AssertPublicReferenceArgumentScopesClear(t *testing.T, in *Instance) {
	t.Helper()
	state := in.pluginState.Load()
	if state == nil {
		t.Fatal("reference argument owner state was not initialized")
	}
	pool := &state.railshotInvocation
	if pool.tableDepth != 0 {
		t.Fatal("reference argument native entry leaked", pool.tableDepth)
	}
	for slot, scope := range pool.argumentOwners {
		if scope != nil && (scope.active || scope.count != 0 || scope.externrefCount != 0 || scope.externrefStore != nil) {
			t.Fatal("reference argument owner scope leaked", slot, scope.active, scope.count, scope.externrefCount, scope.externrefStore != nil)
		}
	}
}

func TestRV64PublicReferenceFunctionBoundarySourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-boundary", rv64PublicReferenceFunctionBoundaryModule(), artifact)
			if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.tableCount() != 0 {
				t.Fatal("reference boundary metadata", c.requiredFeatures)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if len(in.funcRefDescs) != 4*coreruntime.FuncRefDescBytes {
				t.Fatal("reference boundary descriptor extent", len(in.funcRefDescs))
			}
			for _, index := range []int{1, 2} {
				row := in.funcRefDescs[(index+1)*coreruntime.FuncRefDescBytes : (index+2)*coreruntime.FuncRefDescBytes]
				if slices.Equal(row, make([]byte, len(row))) || railshotNonCallableFunction(in.c, index) {
					t.Fatal("reference export lacks callable descriptor", index, row)
				}
			}

			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("reference result", got, err)
			}
			token := got[0]
			if !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, token).FuncRef(), 0) {
				t.Fatal("reference result identity")
			}
			for _, arg := range []uint64{0, token, 0, token} {
				got, err = in.Invoke("id", arg)
				if err != nil || !slices.Equal(got, []uint64{arg}) {
					t.Fatal("reference boundary round trip", got, arg, err)
				}
			}
			typed, err := in.InvokeValues(context.Background(), "id", ValueFuncRef(ValueOf(ValFuncRef, token).FuncRef()))
			if err != nil || len(typed) != 1 || typed[0].Type() != ValFuncRef || typed[0].Bits() != token {
				t.Fatal("typed reference boundary", typed, err)
			}
			if got, err = in.Invoke("id", token^0x9e3779b97f4a7c15); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatal("forged reference ingress", got, err)
			}
			if got, err = in.Invoke("id", token); err != nil || !slices.Equal(got, []uint64{token}) {
				t.Fatal("reuse after rejected ingress", got, err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionBoundaryAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	call := rv64NativeTailCompile(t, "reference-function-call-boundary-admission", rv64PublicReferenceFunctionCallBoundaryModule(42), false)
	if !rv64PublicReferenceCallBoundary(call) {
		t.Fatal("exact public reference call boundary rejected")
	}
	base := rv64NativeTailCompile(t, "reference-function-boundary-admission", rv64PublicReferenceFunctionBoundaryModule(), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"table", func(c *Compiled) { c.HasTable, c.TableType, c.TableSize = true, ValFuncRef, 1 }},
		{"reference-global", func(c *Compiled) { c.Globals = []GlobalDef{{Type: ValFuncRef}} }},
		{"dynamic-reference-call", func(c *Compiled) { c.nativeReferenceCallABI = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-ephemeral reference export admitted", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"missing-reference-marker", func(c *Compiled) { c.nativeReferenceFunctionABI = false }},
		{"missing-control-marker", func(c *Compiled) { c.nativeReferenceControlABI = false }},
		{"table", func(c *Compiled) { c.HasTable, c.TableType, c.TableSize = true, ValFuncRef, 1 }},
		{"reference-global", func(c *Compiled) { c.Globals = []GlobalDef{{Type: ValFuncRef}} }},
		{"missing-descriptors", func(c *Compiled) { c.NeedsFuncRefDescs = false }},
		{"extra-feature", func(c *Compiled) { c.requiredFeatures |= CoreFeatureTailCall }},
		{"value-pool", func(c *Compiled) { c.ValueTypes = []ValueTypeDescriptor{{Kind: ValueTypeI32}} }},
		{"rec-group", func(c *Compiled) { c.Types[2].RecGroup = 7 }},
		{"wrong-type-binding", func(c *Compiled) { c.Funcs[2].TypeIndex = 1 }},
		{"wrong-caller-parameter", func(c *Compiled) { c.Types[2].Params[0].Ref.Heap.TypeIndex = 1 }},
		{"substituted-export", func(c *Compiled) {
			delete(c.Exports, "target")
			c.Exports["other"] = 0
		}},
	} {
		t.Run("call-"+tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(call)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-exact public reference call boundary admitted", err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionCallBoundarySourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-call-boundary", rv64PublicReferenceFunctionCallBoundaryModule(42), artifact)
			if !rv64PublicReferenceCallBoundary(c) || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences) || c.tableCount() != 0 || len(c.Globals) != 0 {
				t.Fatal("public reference call boundary metadata", c.requiredFeatures)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("reference target token", got, err)
			}
			token := got[0]
			if got, err = in.Invoke("call", token); err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("public call_ref boundary", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", 0); got != nil {
				t.Fatal("null call_ref returned values", got, err)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("null call_ref trap", err)
				}
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", token^0x9e3779b97f4a7c15); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatal("forged call_ref ingress", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", token); err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("reuse after rejected call_ref ingress", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("target"); err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("reference-free entry after owner scope reuse", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			fn, err := in.WasmFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if got, err = session.Invoke(token); err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("prepared reference argument owner scope", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
		})
	}
}

func TestRV64PublicReferenceFunctionCallBoundaryForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			producerCompiled := rv64NativeTailCompile(t, "reference-function-call-owner-producer", rv64PublicReferenceFunctionCallBoundaryModule(41), artifact)
			consumerCompiled := rv64NativeTailCompile(t, "reference-function-call-owner-consumer", rv64PublicReferenceFunctionCallBoundaryModule(42), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			producer := rv64NativeTokenInstantiate(t, rt, producerCompiled, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, consumerCompiled, NewImports())
			got, err := producer.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("foreign producer token", got, err)
			}
			foreignToken := got[0]
			own, err := consumer.Invoke("get")
			if err != nil || len(own) != 1 || own[0] == 0 {
				t.Fatal("consumer token", own, err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("public token did not retain foreign target")
			}
			if !producer.usesIndependentExecution() || !consumer.usesIndependentExecution() {
				t.Fatal("foreign owner fixture was already shared")
			}
			for _, tc := range []struct {
				token uint64
				want  uint64
			}{{foreignToken, 41}, {own[0], 42}, {foreignToken, 41}} {
				if got, err = consumer.Invoke("call", tc.token); err != nil || !slices.Equal(got, []uint64{tc.want}) {
					t.Fatal("invocation-scoped reference owner transport", got, tc.want, err)
				}
				rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			}
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign call_ref did not quiesce both native owners")
			}

			outsideCompiled := rv64NativeTailCompile(t, "reference-function-call-owner-outside", rv64PublicReferenceFunctionBoundaryModule(), artifact)
			outside := rv64NativeTokenInstantiate(t, rt, outsideCompiled, NewImports())
			outsideResult, err := outside.Invoke("get")
			if err != nil || len(outsideResult) != 1 || outsideResult[0] == 0 {
				t.Fatal("outside owner token", outsideResult, err)
			}
			if got, err = consumer.Invoke("call", outsideResult[0]); err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("additional same-runtime callable owner", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)

			foreignRT := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, foreignRT)
			foreign := rv64NativeTokenInstantiate(t, foreignRT, consumerCompiled, NewImports())
			if got, err = foreign.Invoke("call", foreignToken); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatal("cross-runtime call_ref token accepted", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, foreign)
			if err := foreign.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicReferenceArgumentScopeFailedBegin(t *testing.T) {
	in := &Instance{c: &Compiled{nativeReferenceCallABI: true}}
	pool := &in.ensurePluginState().railshotInvocation
	pool.tableDepth = len(pool.argumentOwners)
	outer := &rv64InvocationArgumentOwners{active: true}
	pool.argumentOwners[len(pool.argumentOwners)-1] = outer
	scope, err := railshotBeginPublicFuncrefArguments(in)
	if err == nil || scope.in != nil || scope.scope != nil {
		t.Fatal("reference argument nesting bound was not fail-closed", scope, err)
	}
	railshotReleasePublicFuncrefArguments(scope)
	if !outer.active || outer.count != 0 || pool.argumentOwners[len(pool.argumentOwners)-1] != outer {
		t.Fatal("failed scope begin disturbed the outer owner scope")
	}
}

func TestRV64TableFreeReferenceArgumentOwnerScope(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "table-free-argument-owner", rv64PublicReferenceFunctionBoundaryModule(), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			producer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer consumer.Close()
			if consumer.c.NumImports != 0 || consumer.c.tableCount() != 0 || consumer.c.nativeReferenceCallABI {
				t.Fatal("fixture has an import, table or call_ref")
			}
			values, err := producer.Invoke("get")
			if err != nil || len(values) != 1 || values[0] == 0 {
				t.Fatal("producer reference", values, err)
			}
			token := values[0]
			descriptor, ok := consumer.refStore.resolve(token)
			if !ok || descriptor == 0 {
				t.Fatal("producer descriptor unavailable")
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			refs := func() int {
				producer.lifeMu.Lock()
				defer producer.lifeMu.Unlock()
				return producer.resourceRefs
			}
			before := refs()
			func() {
				arguments, err := railshotBeginPublicFuncrefArguments(consumer)
				if err != nil || arguments.scope == nil {
					t.Fatal("table-free argument scope unavailable", err)
				}
				defer railshotReleasePublicFuncrefArguments(arguments)
				for i := 0; i < 2; i++ {
					if err := railshotStagePublicFuncrefArgument(consumer, &arguments, token, descriptor); err != nil {
						t.Fatal(err)
					}
				}
				if refs() != before+1 || arguments.scope.count != 1 {
					t.Fatal("argument scope did not retain exactly one producer root")
				}
			}()
			if refs() != before {
				t.Fatal("argument scope retained its producer pin after release")
			}
			if got, err := consumer.Invoke("id", token); err != nil || !slices.Equal(got, []uint64{token}) {
				t.Fatal("table-free reference identity", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
		})
	}
}

func TestRV64PublicReferenceFunctionBoundaryOwnership(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	cfg := rv64NativeTailConfig()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rv64HostCertificateCloseRuntime(t, rt)
	c := rv64NativeTailCompile(t, "reference-function-boundary-owner", rv64PublicReferenceFunctionBoundaryModule(), false)
	first := rv64NativeTokenInstantiate(t, rt, c, NewImports())
	second := rv64NativeTokenInstantiate(t, rt, c, NewImports())
	got, err := first.Invoke("get")
	if err != nil || len(got) != 1 || got[0] == 0 {
		t.Fatal("producer reference", got, err)
	}
	token := got[0]
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if !first.hasPhysicalResources() {
		t.Fatal("public token did not retain producer")
	}
	if got, err = second.Invoke("id", token); err != nil || !slices.Equal(got, []uint64{token}) {
		t.Fatal("same-runtime foreign descriptor round trip", got, err)
	}
	foreignRT := NewRuntime(WithRuntimeConfig(cfg))
	defer rv64HostCertificateCloseRuntime(t, foreignRT)
	foreign := rv64NativeTokenInstantiate(t, foreignRT, c, NewImports())
	if got, err = foreign.Invoke("id", token); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatal("cross-runtime token accepted", got, err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	if err := foreignRT.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	rv64HostCertificateCloseRuntime(t, rt)
	if first.hasPhysicalResources() || second.hasPhysicalResources() {
		t.Fatalf("reference boundary owner retained after Runtime close: first=%t second=%t", first.hasPhysicalResources(), second.hasPhysicalResources())
	}
}
