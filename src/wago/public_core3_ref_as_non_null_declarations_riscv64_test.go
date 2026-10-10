//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

// rv64Core3RefAsNonNullDeclarationsSource is byte-identical to the no-debug
// ref_as_non_null.2.wasm emitted by WABT 1.0.41 for pinned Release 3 revision
// 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
func rv64Core3RefAsNonNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x64, 0, 0},
			[]byte{0x60, 1, 0x64, 0x70, 0},
			[]byte{0x60, 1, 0x64, 0x6f, 0},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xd4, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd4, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd4, 0x1a, 0x0b}),
		)),
	)
}

func TestRV64Core3RefAsNonNullPinnedDeclarations(t *testing.T) {
	source := rv64Core3RefAsNonNullDeclarationsSource()
	if len(source) != 59 || fmt.Sprintf("%x", sha256.Sum256(source)) != "45fee4fd0cc00312b4760a7159290fe1c5c5dfd23d6d80de5eb18fc6c5985eb7" {
		t.Fatal("pinned declaration source identity", len(source))
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" && !artifact {
				t.Skip("compiler-free artifact case only")
			}
			c := rv64NativeTailCompile(t, "core3-ref-as-non-null-declarations", source, artifact)
			if !rv64Core3RefAsNonNullDeclarations(c) || rv64Core3ReferenceDeclarations(c) || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.needsFuncRefDescs() || c.NumImports != 0 || len(c.Funcs) != 3 || len(c.Types) != 4 || len(c.Exports) != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasStart {
				t.Fatal("pinned ref.as_non_null declaration-only source/artifact witnesses")
			}
			for i := 1; i < 4; i++ {
				if len(c.Types[i].Params) != 1 || c.Types[i].Params[0].Kind != ValueTypeReference || c.Types[i].Params[0].Ref.Nullable {
					t.Fatal("pinned non-null type descriptor", i)
				}
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

func TestRV64Core3RefAsNonNullPinnedDeclarationSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64Core3RefAsNonNullDeclarationsSource()
	debug := append(bytes.Clone(base),
		0x00, 0x1d, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x02,
		0x10, 0x03, 0x00, 0x01, 0x00, 0x01, 0x72, 0x01,
		0x01, 0x00, 0x01, 0x72, 0x02, 0x01, 0x00, 0x01,
		0x72, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if len(debug) != 90 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "61a237b98e3b2f2cd603ea05a02bf11fce31f7186c5f7972b0ba822e49b5c149" {
		t.Fatal("canonical debug-name fixture")
	}
	mutations := map[string][]byte{
		"debug-name":      debug,
		"extern-nullable": bytes.Replace(bytes.Clone(base), []byte{0x64, 0x6f, 0}, []byte{0x63, 0x6f, 0}, 1),
		"extern-heap":     bytes.Replace(bytes.Clone(base), []byte{0x64, 0x6f, 0}, []byte{0x64, 0x6e, 0}, 1),
		"binding":         bytes.Replace(bytes.Clone(base), []byte{3, 1, 2, 3}, []byte{3, 1, 2, 2}, 1),
		"opcode":          bytes.Replace(bytes.Clone(base), []byte{0xd4, 0x1a}, []byte{0xd1, 0x1a}, 1),
		"body-order":      bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0xd4, 0x1a}, []byte{0x20, 0, 0x1a, 0xd4}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			c, err := rv64NativeTailConfig().Compile(source)
			if c != nil {
				c.Close()
			}
			if err == nil {
				t.Fatal("nearby declaration source admitted")
			}
		})
	}
}

func TestRV64Core3RefAsNonNullPinnedDeclarationArtifactGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source and hostile artifact admission")
	}
	base := rv64NativeTailCompile(t, "core3-ref-as-non-null-declarations-gates", rv64Core3RefAsNonNullDeclarationsSource(), false)
	defer base.Close()
	mutations := map[string]func(*Compiled){
		"function-abi":      func(c *Compiled) { c.nativeReferenceFunctionABI = false },
		"control-abi":       func(c *Compiled) { c.nativeReferenceControlABI = false },
		"call-abi":          func(c *Compiled) { c.nativeReferenceCallABI = true },
		"context-abi":       func(c *Compiled) { c.nativeContextTrapABI = false },
		"table-context-abi": func(c *Compiled) { c.nativeTableContextABI = false },
		"table-ref-abi":     func(c *Compiled) { c.nativeTableReferenceABI = false },
		"register-abi":      func(c *Compiled) { c.registerABIDisabled = false },
		"entry":             func(c *Compiled) { c.Entry[2] = c.Entry[1] },
		"internal-entry":    func(c *Compiled) { c.InternalEntry[2]++ },
		"paired-entry": func(c *Compiled) {
			c.Entry[2] = c.Entry[1]
			c.InternalEntry[2] = c.InternalEntry[1]
		},
		"function-binding": func(c *Compiled) { c.Funcs[2].TypeIndex = 2 },
		"type-id":          func(c *Compiled) { c.FuncTypeID[2] = 0 },
		"wrong-type-id":    func(c *Compiled) { c.FuncTypeID[2] ^= 1 },
		"feature":          func(c *Compiled) { c.requiredFeatures |= CoreFeatureTailCall },
		"indexed-nullable": func(c *Compiled) { c.Types[1].Params[0].Ref.Nullable = true },
		"func-nullable":    func(c *Compiled) { c.Types[2].Params[0].Ref.Nullable = true },
		"type-nullability": func(c *Compiled) { c.Types[3].Params[0].Ref.Nullable = true },
		"all-nullable": func(c *Compiled) {
			for i := 1; i < 4; i++ {
				c.Types[i].Params[0].Ref.Nullable = true
			}
		},
		"all-nullable-code-byte": func(c *Compiled) {
			for i := 1; i < 4; i++ {
				c.Types[i].Params[0].Ref.Nullable = true
			}
			c.code = bytes.Clone(c.code)
			c.code[0] ^= 1
		},
		"type-heap":  func(c *Compiled) { c.Types[3].Params[0].Ref.Heap.Abstract = AbstractHeapFunc },
		"type-index": func(c *Compiled) { c.Types[1].Params[0].Ref.Heap.TypeIndex = 1 },
		"type-result": func(c *Compiled) {
			c.Types[3].Results = []ValueTypeDescriptor{c.Types[3].Params[0]}
			c.Funcs[2].Results = []ValType{ValExternRef}
		},
		"gc-id":            func(c *Compiled) { c.GCTypeDescs[3].ID-- },
		"gc-layout":        func(c *Compiled) { c.GCTypeDescs[3].Size = 8 },
		"descriptor-arena": func(c *Compiled) { c.NeedsFuncRefDescs = true },
		"export":           func(c *Compiled) { c.Exports["escape"] = 2 },
		"passive":          func(c *Compiled) { c.passiveElems = []ElemInit{{Mode: ElemModePassive, RefType: ValExternRef}} },
		"directory":        func(c *Compiled) { c.memoryDir.defs = []memoryDef{{}} },
		"code":             func(c *Compiled) { c.code = nil },
		"code-byte": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[0] ^= 1
		},
		"names": func(c *Compiled) { c.Names = &wasm.NameSec{} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			bad := rv64Core3DeclarationCompiledCopy(base)
			mutate(bad)
			if rv64Core3RefAsNonNullDeclarations(bad) || railshotValidateCompiled(bad) == nil {
				t.Fatal("hostile ref.as_non_null declaration metadata admitted")
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
				t.Fatal("hostile serialized ref.as_non_null declaration metadata admitted")
			}
		})
	}
	i31 := rv64Core3DeclarationCompiledCopy(base)
	i31.memoryDir.gcI31TableInit = &gcI31TableInitializer{}
	if rv64Core3RefAsNonNullDeclarations(i31) || railshotValidateCompiled(i31) == nil {
		t.Fatal("hostile i31 directory admitted by exact retained witness")
	}
}

func TestRV64Core3RefAsNonNullDeclarationCompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	if c, err := Compile(nil, rv64Core3RefAsNonNullDeclarationsSource()); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			c.Close()
		}
		t.Fatal("compiler-free pinned declaration used source compilation", err)
	}
}
