//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// rv64Core3RefIsNullDeclarationsSource is byte-identical to the no-debug
// ref_is_null.1.wasm emitted by WABT 1.0.41 for pinned Release 3 revision
// 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
func rv64Core3RefIsNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x64, 0, 0},
			[]byte{0x60, 1, 0x64, 0x70, 0},
			[]byte{0x60, 1, 0x64, 0x6f, 0},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x1a, 0x0b}),
		)),
	)
}

func TestRV64Core3RefIsNullPinnedDeclarations(t *testing.T) {
	source := rv64Core3RefIsNullDeclarationsSource()
	if len(source) != 59 || fmt.Sprintf("%x", sha256.Sum256(source)) != "34acce8bbb453baa539d7fa152076d6cbe6b09f789bee05725efa4612ba7e9a5" {
		t.Fatal("pinned declaration source identity", len(source))
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" && !artifact {
				t.Skip("compiler-free artifact case only")
			}
			c := rv64NativeTailCompile(t, "core3-ref-is-null-declarations", source, artifact)
			view := c.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.NumImports != 0 || len(c.Funcs) != 3 || len(c.Types) != 4 || len(c.Exports) != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasStart {
				t.Fatalf("pinned ref.is_null declaration-only source/artifact witnesses: contract=%t validation=%v function=%t control=%t call=%t features=%#x imports=%d funcs=%d types=%d exports=%d tables=%d memories=%d globals=%d elems=%d passive=%d start=%t", rv64SourceContractValid(view), railshotValidateCompiled(view), c.nativeReferenceFunctionABI, c.nativeReferenceControlABI, c.nativeReferenceCallABI, c.requiredFeatures, c.NumImports, len(c.Funcs), len(c.Types), len(c.Exports), c.tableCount(), c.memoryCount(), len(c.Globals), len(c.Elems), len(c.passiveElems), c.HasStart)
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

func TestRV64Core3RefIsNullPinnedDeclarationSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64Core3RefIsNullDeclarationsSource()
	debug := append(bytes.Clone(base),
		0x00, 0x1d, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x02,
		0x10, 0x03, 0x00, 0x01, 0x00, 0x01, 0x72, 0x01,
		0x01, 0x00, 0x01, 0x72, 0x02, 0x01, 0x00, 0x01,
		0x72, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if len(debug) != 90 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "f61f8b317236822d33571492f714c3627072c6b6985754e2f3020f464d0fad74" {
		t.Fatal("canonical debug-name fixture")
	}
	mutations := map[string][]byte{
		"debug-name":      debug,
		"extern-nullable": bytes.Replace(bytes.Clone(base), []byte{0x64, 0x6f, 0}, []byte{0x63, 0x6f, 0}, 1),
		"extern-heap":     bytes.Replace(bytes.Clone(base), []byte{0x64, 0x6f, 0}, []byte{0x64, 0x6e, 0}, 1),
		"binding":         bytes.Replace(bytes.Clone(base), []byte{3, 1, 2, 3}, []byte{3, 1, 2, 2}, 1),
		"opcode-ref-null": bytes.Replace(bytes.Clone(base), []byte{0xd1, 0x1a}, []byte{0xd0, 0x1a}, 1),
		"body-order":      bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0xd1, 0x1a}, []byte{0x20, 0, 0x1a, 0xd1}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			rv64RequireCore3SourceValidation(t, source)
		})
	}
}
