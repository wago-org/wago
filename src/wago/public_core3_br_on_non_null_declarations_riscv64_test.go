//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// rv64Core3BrOnNonNullDeclarationsSource is byte-identical to the no-debug
// br_on_non_null.1.wasm emitted by WABT 1.0.41 for pinned Release 3 revision
// 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
func rv64Core3BrOnNonNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x63, 0, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x02, 0x64, 0, 0x20, 0, 0xd6, 0, 0x00, 0x0b, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x64, 0x70, 0x20, 0, 0xd6, 0, 0x00, 0x0b, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x64, 0x6f, 0x20, 0, 0xd6, 0, 0x00, 0x0b, 0x1a, 0x0b}),
		)),
	)
}

func TestRV64Core3BrOnNonNullPinnedDeclarations(t *testing.T) {
	source := rv64Core3BrOnNonNullDeclarationsSource()
	if len(source) != 75 || fmt.Sprintf("%x", sha256.Sum256(source)) != "fd36a463ea49ff5ce0eae90789b44bab38b09ca0bc83083e125b351a1b01010a" {
		t.Fatal("pinned declaration source identity", len(source))
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" && !artifact {
				t.Skip("compiler-free artifact case only")
			}
			c := rv64NativeTailCompile(t, "core3-br-on-non-null-declarations", source, artifact)
			view := c.executionView()
			if !rv64ArtifactContractValid(view) || railshotValidateCompiled(view) != nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.NumImports != 0 || len(c.Funcs) != 3 || len(c.Types) != 4 || len(c.Exports) != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasStart {
				t.Fatalf("pinned declaration-only source/artifact witnesses: contract=%t validation=%v function=%t control=%t call=%t features=%#x imports=%d funcs=%d types=%d exports=%d tables=%d memories=%d globals=%d elems=%d passive=%d start=%t", rv64ArtifactContractValid(view), railshotValidateCompiled(view), c.nativeReferenceFunctionABI, c.nativeReferenceControlABI, c.nativeReferenceCallABI, c.requiredFeatures, c.NumImports, len(c.Funcs), len(c.Types), len(c.Exports), c.tableCount(), c.memoryCount(), len(c.Globals), len(c.Elems), len(c.passiveElems), c.HasStart)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("missing"); got != nil || err == nil {
				t.Fatal("declaration-only module published a callable surface", got, err)
			}
			if err := in.Close(); err != nil {
				t.Fatal("declaration-only immediate cleanup", err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if in.hasPhysicalResources() || c.codeCache.mem != nil || c.codeCache.refs != 0 {
				t.Fatal("declaration-only resource cleanup")
			}
		})
	}
}

func TestRV64Core3BrOnNonNullPinnedDeclarationSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64Core3BrOnNonNullDeclarationsSource()
	debug := append(bytes.Clone(base),
		0x00, 0x1d, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x02,
		0x10, 0x03, 0x00, 0x01, 0x00, 0x01, 0x72, 0x01,
		0x01, 0x00, 0x01, 0x72, 0x02, 0x01, 0x00, 0x01,
		0x72, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if fmt.Sprintf("%x", sha256.Sum256(debug)) != "3905f10de5be56a7f4d65f7e85aaffffa76be69306719d5c4d6fac09f122f813" {
		t.Fatal("canonical debug-name fixture")
	}
	mutations := map[string][]byte{
		"debug-name":  debug,
		"opcode":      bytes.Replace(bytes.Clone(base), []byte{0xd6, 0}, []byte{0xd5, 0}, 1),
		"depth":       bytes.Replace(bytes.Clone(base), []byte{0xd6, 0}, []byte{0xd6, 1}, 1),
		"nullable":    bytes.Replace(bytes.Clone(base), []byte{0x02, 0x64, 0x6f}, []byte{0x02, 0x63, 0x6f}, 1),
		"heap":        bytes.Replace(bytes.Clone(base), []byte{0x02, 0x64, 0x6f}, []byte{0x02, 0x64, 0x6e}, 1),
		"unreachable": bytes.Replace(bytes.Clone(base), []byte{0xd6, 0, 0, 0x0b}, []byte{0xd6, 0, 1, 0x0b}, 1),
		"drop":        bytes.Replace(bytes.Clone(base), []byte{0x0b, 0x1a, 0x0b}, []byte{0x0b, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			rv64RequireCore3SourceValidation(t, source)
		})
	}
}

func TestRV64Core3BrOnNonNullPinnedDeclarationArtifactGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source and hostile artifact admission")
	}
	base := rv64NativeTailCompile(t, "core3-br-on-non-null-declarations-gates", rv64Core3BrOnNonNullDeclarationsSource(), false)
	defer base.Close()
	mutations := map[string]func(*Compiled){
		"function-abi":     func(c *Compiled) { c.nativeReferenceFunctionABI = false },
		"control-abi":      func(c *Compiled) { c.nativeReferenceControlABI = false },
		"call-abi":         func(c *Compiled) { c.nativeReferenceCallABI = true },
		"context-abi":      func(c *Compiled) { c.nativeContextTrapABI = false },
		"register-abi":     func(c *Compiled) { c.registerABIDisabled = false },
		"entry":            func(c *Compiled) { c.Entry[2] = c.Entry[1] },
		"internal-entry":   func(c *Compiled) { c.InternalEntry[2]++ },
		"type-id":          func(c *Compiled) { c.FuncTypeID[2] = 0 },
		"gc-id":            func(c *Compiled) { c.GCTypeDescs[3].ID-- },
		"gc-layout":        func(c *Compiled) { c.GCTypeDescs[3].Size = 8 },
		"descriptor-arena": func(c *Compiled) { c.NeedsFuncRefDescs = true },
		"passive":          func(c *Compiled) { c.passiveElems = []ElemInit{{Mode: ElemModePassive, RefType: ValExternRef}} },
		"extra-feature":    func(c *Compiled) { c.requiredFeatures |= CoreFeatureTailCall },
		"extern-result": func(c *Compiled) {
			c.Types[3].Results = []ValueTypeDescriptor{c.Types[3].Params[0]}
			c.Types[3].Params = nil
			c.Funcs[2].Results = []ValType{ValExternRef}
			c.Funcs[2].Params = nil
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			bad := rv64Core3DeclarationCompiledCopy(base)
			mutate(bad)
			if rv64Core3ReferenceDeclarations(bad) || railshotValidateCompiled(bad) == nil {
				t.Fatal("hostile declaration metadata admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile serialized declaration metadata admitted")
			}
		})
	}
	blob, err := marshalCompiled(base.executionView())
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Clone(blob)
	corrupt[0] ^= 0x80
	if loaded, err := LoadTrustedArtifact(corrupt); loaded != nil || err == nil {
		if loaded != nil {
			loaded.Close()
		}
		t.Fatal("corrupt declaration artifact admitted")
	}
}

func TestRV64Core3BrOnNonNullDeclarationCompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	if c, err := Compile(nil, rv64Core3BrOnNonNullDeclarationsSource()); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			c.Close()
		}
		t.Fatal("compiler-free pinned declaration used source compilation", err)
	}
}

func TestRV64Core3ReferenceDeclarationClassifiersRemainDistinct(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	nullDecl := rv64NativeTailCompile(t, "core3-br-on-null-declarations-distinct", rv64Core3BrOnNullDeclarationsSource(), false)
	nonNullDecl := rv64NativeTailCompile(t, "core3-br-on-non-null-declarations-distinct", rv64Core3BrOnNonNullDeclarationsSource(), false)
	defer nullDecl.Close()
	defer nonNullDecl.Close()
	if !rv64ArtifactContractValid(nullDecl.executionView()) || !rv64ArtifactContractValid(nonNullDecl.executionView()) {
		t.Fatal("retained declaration metadata lost the general artifact contract")
	}
	if err := railshotValidateCompiled(nullDecl); err != nil {
		t.Fatal("exact br_on_null retained artifact rejected", err)
	}
	if err := railshotValidateCompiled(nonNullDecl); err != nil {
		t.Fatal("exact br_on_non_null retained artifact rejected", err)
	}
}
