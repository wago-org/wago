//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestRV64PublicDynamicHostReferenceCarriers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-owned%t", artifact, owned), func(t *testing.T) {
				if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
					t.Skip("precompiled artifact path only")
				}
				types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
				input := []uint64{0xfeedface80000001, 0, 0, 0xfedcba9876543210, 0xcafe12347f800123, 0xfff8000000000123}
				want := []uint64{0x80000001, 0, 0, input[3], 0x7f800123, input[5]}
				source := rv64PublicReferenceTailModule(types, true)
				c := rv64NativeTailCompile(t, "dynamic-host-reference-carriers", source, artifact)
				m := rv64NativeTailModule(c)
				if !rv64PublicDynamicHostReferenceBoundary(c) || m == nil || len(m.Plans) != 2 || !rv64PublicDynamicHostReferencePlan(c, m.Plans[0]) || !rv64PublicDynamicHostReferencePlan(c, m.Plans[1]) {
					t.Fatal("dynamic host reference retained boundary", m)
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				ref, err := rt.NewExternRef("dynamic-host-reference-carrier")
				if err != nil {
					t.Fatal(err)
				}
				producerCode := rv64NativeTailCompile(t, "dynamic-host-reference-carrier-producer", rv64PublicReferenceCarrierModule(false), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
				callable, err := producer.Invoke("get")
				if err != nil || len(callable) != 1 || callable[0] == 0 {
					t.Fatal("dynamic host reference producer", callable, err)
				}
				input[1], input[2] = callable[0], ValueExternRef(ref).Bits()
				want[1], want[2] = input[1], input[2]
				calls := 0
				host := HostCallFunc(func(call HostCall) {
					calls++
					if uint32(call.I32(0)) != uint32(want[0]) || call.FuncRef(1).IsNull() || call.ExternRef(2) != ref || call.I64(3) != int64(want[3]) || call.ParamSlots()[4] != want[4] || call.ParamSlots()[5] != want[5] {
						panic("dynamic host reference carrier arguments")
					}
					call.ResultSlots()[0] = 0xeeeeeeee00000000 | uint64(uint32(call.I32(0)))
					call.SetFuncRef(1, call.FuncRef(1))
					call.SetExternRef(2, call.ExternRef(2))
					call.SetI64(3, call.I64(3))
					call.ResultSlots()[4] = 0xdddddddd00000000 | call.ParamSlots()[4]
					call.ResultSlots()[5] = call.ParamSlots()[5]
				})
				binding := any(host)
				if owned {
					owner, err := rt.NewHostFuncRef(host, c.importFuncSigs[0])
					if err != nil {
						t.Fatal(err)
					}
					defer owner.Close()
					binding = owner
				}
				in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
				if rv64StaticTailPlanCount(m) != 0 || rv64EntryCapability(in) != 0 || in.pluginState.Load().railshotInvocation.tailDispatcher != nil {
					t.Fatal("dynamic host reference acquired competing static capability")
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("call", append(slices.Clone(input), 0)...)
				if err != nil || !slices.Equal(got, want) || calls != 1 {
					t.Fatal("dynamic host reference carrier transport", got, calls, err)
				}
				rv64AssertPublicReferenceCarrierStateClear(t, in)
				if got, err = in.Invoke("call", 0, 0, 0, 9, 11, 13, 1); err == nil || len(got) != 0 || calls != 1 {
					t.Fatal("dynamic null tail target did not trap", got, calls, err)
				}
				rv64AssertPublicReferenceCarrierStateClear(t, in)
				if !rt.ReleaseExternRef(ref) {
					t.Fatal("dynamic host reference externref remained pinned")
				}
			})
		}
	}
}

func TestRV64PublicDynamicHostReferenceCarrierArtifactAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "dynamic-host-reference-carrier-admission", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}, true), false)
	if !rv64PublicDynamicHostReferenceBoundary(base) || rv64ValidateReferenceFunctions(base) != nil || railshotValidateCompiled(base) != nil {
		t.Fatal("dynamic host reference carrier baseline")
	}
	mutations := map[string]func(*Compiled, *encoder.NativeTailModule){
		"import-name":        func(c *Compiled, _ *encoder.NativeTailModule) { c.Imports[0] = "env.other" },
		"import-signature":   func(c *Compiled, _ *encoder.NativeTailModule) { c.importFuncSigs[0].Results[0] = ValI64 },
		"identity-signature": func(c *Compiled, _ *encoder.NativeTailModule) { c.Funcs[0].Params[0] = ValI64 },
		"caller-selector":    func(c *Compiled, _ *encoder.NativeTailModule) { c.Funcs[1].Params[6] = ValI64 },
		"type-binding":       func(c *Compiled, _ *encoder.NativeTailModule) { c.Funcs[1].TypeIndex = 2 },
		"type-descriptor":    func(c *Compiled, _ *encoder.NativeTailModule) { c.Types[2].Results[1] = c.Types[2].Results[0] },
		"export":             func(c *Compiled, _ *encoder.NativeTailModule) { c.Exports["call"] = 1 },
		"feature":            func(c *Compiled, _ *encoder.NativeTailModule) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"dynamic-import":     func(c *Compiled, _ *encoder.NativeTailModule) { c.dynamicImports = false },
		"dynamic-escape":     func(c *Compiled, _ *encoder.NativeTailModule) { c.dynamicFuncrefEscape = false },
		"descriptor":         func(c *Compiled, _ *encoder.NativeTailModule) { c.NeedsFuncRefDescs = false },
		"gc-descriptor":      func(c *Compiled, _ *encoder.NativeTailModule) { c.GCTypeDescs[2].Final = false },
		"function-type-id":   func(c *Compiled, _ *encoder.NativeTailModule) { c.FuncTypeID[1]++ },
		"entry":              func(c *Compiled, _ *encoder.NativeTailModule) { c.Entry[1] += 16 },
		"memory-export":      func(c *Compiled, _ *encoder.NativeTailModule) { c.memoryDir.exports = map[string]int{"memory": 0} },
		"missing-plan":       func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans = m.Plans[:1] },
		"plan-kind":          func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].Kind = encoder.NativeTailDirectImport },
		"plan-type":          func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].TypeIndex = 0 },
		"plan-function":      func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].FunctionIndex = 1 },
		"plan-source-pc":     func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].SourcePC++ },
		"plan-frame":         func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].FrameBytes += 16 },
		"plan-source-base":   func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].SourceBase += 8 },
		"plan-stride":        func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].SourceStride = 16 },
		"plan-reference":     func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].ReferenceBase -= 8 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo, candidate.codeCache = &validateMemo{}, nil
			module := &encoder.NativeTailModule{Plans: slices.Clone(rv64NativeTailModule(base).Plans)}
			rv64StoreNativeTailModule(candidate, module)
			mutate(candidate, module)
			if rv64PublicDynamicHostReferenceBoundary(candidate) || railshotValidateCompiled(candidate) == nil {
				t.Fatal("dynamic host reference carrier artifact mutation admitted")
			}
			blob, err := marshalCompiled(candidate)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile dynamic host reference carrier artifact loaded")
			}
		})
	}

	unsupported := rv64PublicReferenceTailModule([]wasm.ValType{wasm.FuncRef, wasm.ExternRef}, true)
	unsupported[len(unsupported)-1] ^= 1
	if c, err := rv64NativeTailConfig().Compile(unsupported); err == nil || c != nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("near-miss dynamic host reference source compiled", err)
	}
	if !slices.Equal(rv64NativeTailModule(base).Plans, []encoder.NativeTailPlan{{Kind: encoder.NativeTailReference, TypeIndex: 2, FunctionIndex: 2, SourcePC: 19, FrameBytes: 160, SourceBase: 104, SourceStride: 8, ReferenceBase: 152}, {Kind: encoder.NativeTailReference, TypeIndex: 2, FunctionIndex: 2, SourcePC: 36, FrameBytes: 160, SourceBase: 104, SourceStride: 8, ReferenceBase: 152}}) {
		t.Fatal("dynamic host reference canonical plan changed")
	}
	if _, _, err := rv64NativeTailOwnerCounts(&Instance{c: base}); err == nil {
		// A detached instance must never be sufficient to mint the dynamic owner.
		t.Fatal("detached dynamic host reference owner admitted", err)
	}
}
