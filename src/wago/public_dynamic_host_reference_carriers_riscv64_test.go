//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestRV64PublicDynamicHostReferenceCarriers(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, owned := range []bool{false, true} {
				t.Run(fmt.Sprintf("ordinary%t-artifact%t-owned%t", ordinary, artifact, owned), func(t *testing.T) {
					if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
						t.Skip("precompiled artifact path only")
					}
					types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
					input := []uint64{0xfeedface80000001, 0, 0, 0xfedcba9876543210, 0xcafe12347f800123, 0xfff8000000000123}
					want := []uint64{0x80000001, 0, 0, input[3], 0x7f800123, input[5]}
					source := rv64PublicReferenceTailModule(types, true, ordinary)
					c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-reference-carriers-ordinary%t", ordinary), source, artifact)
					m := rv64NativeTailModule(c)
					if !rv64PublicDynamicHostReferenceBoundary(c) || ordinary && m != nil || !ordinary && (m == nil || len(m.Plans) != 2 || !rv64PublicDynamicHostReferencePlan(c, m.Plans[0]) || !rv64PublicDynamicHostReferencePlan(c, m.Plans[1])) {
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
						t.Fatal("dynamic null reference target did not trap", got, calls, err)
					}
					rv64AssertPublicReferenceCarrierStateClear(t, in)
					if !rt.ReleaseExternRef(ref) {
						t.Fatal("dynamic host reference externref remained pinned")
					}
				})
			}
		}
	}
}

func TestRV64PublicDynamicHostReferenceCarrierArtifactAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "dynamic-host-reference-carrier-admission", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}, true), false)
	if len(base.code) != 2736 || sha256.Sum256(base.code) != [sha256.Size]byte{0x6a, 0x15, 0x65, 0xa6, 0xcd, 0xf7, 0x76, 0x44, 0x72, 0x04, 0xe6, 0x9c, 0x41, 0xd3, 0xcc, 0x5a, 0xde, 0xab, 0xfd, 0x84, 0xb7, 0xad, 0x32, 0xbd, 0xfd, 0x0e, 0x85, 0x8c, 0x61, 0xfa, 0x18, 0x6b} {
		t.Fatal("dynamic host reference tail code changed")
	}
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
		"entries-together": func(c *Compiled, _ *encoder.NativeTailModule) {
			c.Entry[1], c.InternalEntry[1] = 0, 0
		},
		"code": func(c *Compiled, _ *encoder.NativeTailModule) {
			c.code = slices.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
		"memory-export":    func(c *Compiled, _ *encoder.NativeTailModule) { c.memoryDir.exports = map[string]int{"memory": 0} },
		"missing-plan":     func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans = m.Plans[:1] },
		"plan-kind":        func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].Kind = encoder.NativeTailDirectImport },
		"plan-type":        func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].TypeIndex = 0 },
		"plan-function":    func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].FunctionIndex = 1 },
		"plan-source-pc":   func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].SourcePC++ },
		"plan-frame":       func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].FrameBytes += 16 },
		"plan-source-base": func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].SourceBase += 8 },
		"plan-stride":      func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].SourceStride = 16 },
		"plan-reference":   func(_ *Compiled, m *encoder.NativeTailModule) { m.Plans[0].ReferenceBase -= 8 },
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
	downgraded := cloneCompiledMetadata(base)
	downgraded.requiredFeatures &^= CoreFeatureTailCall
	downgraded.ensureCompileIndexes().railshotTail.module = nil
	if rv64PublicDynamicHostReferenceBoundary(downgraded) {
		t.Fatal("tail code admitted as ordinary dynamic host reference call")
	}
	if _, _, err := rv64NativeTailOwnerCounts(&Instance{c: base}); err == nil {
		// A detached instance must never be sufficient to mint the dynamic owner.
		t.Fatal("detached dynamic host reference owner admitted", err)
	}
}

func TestRV64PublicDynamicHostReferenceCallArtifactAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "dynamic-host-reference-call-admission", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}, true, true), false)
	if len(base.code) != 7344 || sha256.Sum256(base.code) != [sha256.Size]byte{0xe9, 0x2f, 0xfd, 0x31, 0x95, 0x0a, 0x11, 0xc4, 0x28, 0x97, 0x36, 0x4c, 0xf7, 0x30, 0x3f, 0x13, 0x0d, 0xd5, 0xcc, 0x30, 0x6b, 0x78, 0xa5, 0x83, 0x74, 0x1f, 0x93, 0x1e, 0x48, 0xa0, 0xe5, 0x11} {
		t.Fatal("dynamic host reference call code changed")
	}
	if !rv64PublicDynamicHostReferenceBoundary(base) || rv64NativeTailModule(base) != nil || rv64ValidateReferenceFunctions(base) != nil || railshotValidateCompiled(base) != nil {
		t.Fatal("dynamic host reference call baseline")
	}
	mutations := map[string]func(*Compiled){
		"import-name":        func(c *Compiled) { c.Imports[0] = "env.other" },
		"import-signature":   func(c *Compiled) { c.importFuncSigs[0].Results[0] = ValI64 },
		"identity-signature": func(c *Compiled) { c.Funcs[0].Params[0] = ValI64 },
		"caller-selector":    func(c *Compiled) { c.Funcs[1].Params[6] = ValI64 },
		"type-binding":       func(c *Compiled) { c.Funcs[1].TypeIndex = 2 },
		"type-descriptor":    func(c *Compiled) { c.Types[2].Results[1] = c.Types[2].Results[0] },
		"export":             func(c *Compiled) { c.Exports["call"] = 1 },
		"feature":            func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
		"tail-feature":       func(c *Compiled) { c.requiredFeatures |= CoreFeatureTailCall },
		"dynamic-import":     func(c *Compiled) { c.dynamicImports = false },
		"dynamic-escape":     func(c *Compiled) { c.dynamicFuncrefEscape = false },
		"descriptor":         func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"gc-descriptor":      func(c *Compiled) { c.GCTypeDescs[2].Final = false },
		"function-type-id":   func(c *Compiled) { c.FuncTypeID[1]++ },
		"entry":              func(c *Compiled) { c.Entry[1] += 16 },
		"entries-together": func(c *Compiled) {
			c.Entry[1], c.InternalEntry[1] = 0, 0
		},
		"code": func(c *Compiled) {
			c.code = slices.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
		"memory-export": func(c *Compiled) { c.memoryDir.exports = map[string]int{"memory": 0} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo, candidate.codeCache = &validateMemo{}, nil
			mutate(candidate)
			if rv64PublicDynamicHostReferenceBoundary(candidate) || railshotValidateCompiled(candidate) == nil {
				t.Fatal("dynamic host reference call artifact mutation admitted")
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
				t.Fatal("hostile dynamic host reference call artifact loaded")
			}
		})
	}
	withPlan := cloneCompiledMetadata(base)
	withPlan.validateMemo, withPlan.codeCache = &validateMemo{}, nil
	rv64StoreNativeTailModule(withPlan, &encoder.NativeTailModule{Plans: []encoder.NativeTailPlan{{Kind: encoder.NativeTailReference, TypeIndex: 2, FunctionIndex: 2, SourcePC: 19, FrameBytes: 160, SourceBase: 104, SourceStride: 8, ReferenceBase: 152}}})
	if rv64PublicDynamicHostReferenceBoundary(withPlan) || railshotValidateCompiled(withPlan) == nil {
		t.Fatal("ordinary dynamic host reference call admitted tail metadata")
	}
}
