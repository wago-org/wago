//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// rv64Core3BrOnNullBasicSource is byte-identical to the first module in the
// pinned Release 3 core/br_on_null.wast at revision 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
func rv64Core3BrOnNullBasicSource(target byte) []byte {
	types := wasmtest.Vec(
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		[]byte{0x60, 1, 0x64, 0, 1, 0x7f}, // ((ref 0)) -> i32
		[]byte{0x60, 1, 0x63, 0, 1, 0x7f}, // ((ref null 0)) -> i32
	)
	body := []byte{0x02, 0x40, 0x20, 0, 0xd5, 0, 0x14, 0, 0x0f, 0x0b, 0x41, 0x7f, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("nullable-null", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("nonnullable-f", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("nullable-f", byte(wasm.ExternFunc), 5),
			wasmtest.ExportEntry("unreachable", byte(wasm.ExternFunc), 6),
		)),
		wasmtest.Section(9, []byte{1, 1, 0, 1, target}), // one legacy passive declaration
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(body),
			wasmtest.Code(body),
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 2, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 2, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x40, 0x00, 0xd5, 0, 0x14, 0, 0x0f, 0x0b, 0x41, 0x7f, 0x0b}),
		)),
	)
}

// rv64Core3BrOnNullArgsSource is byte-identical to the third module in the
// same pinned file. It carries one i32 block argument beside the reference.
func rv64Core3BrOnNullArgsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			[]byte{0x60, 2, 0x7f, 0x63, 0, 1, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("args-null", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("args-f", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(9, []byte{1, 1, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x6c, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x7f, 0x20, 0, 0x20, 1, 0xd5, 0, 0x14, 0, 0x0f, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd0, 0, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x10, 1, 0x0b}),
		)),
	)
}

func rv64AssertCore3PassiveTarget(t *testing.T, in *Instance, target int) {
	t.Helper()
	if in.c.tableCount() != 0 || len(in.c.passiveElems) != 1 || in.c.passiveElems[0].Mode != ElemModePassive || len(in.c.passiveElems[0].Values) != 1 || int(in.c.passiveElems[0].Values[0].FuncIndex) != target || in.jm.PassiveElemPtr() == 0 {
		t.Fatal("pinned table-free passive declaration state")
	}
	directory := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr())), coreruntime.PassiveElemDescBytes)
	pointer := binary.LittleEndian.Uint64(directory)
	if pointer == 0 || binary.LittleEndian.Uint32(directory[8:]) != 1 || binary.LittleEndian.Uint32(directory[12:]) != 0 {
		t.Fatal("pinned passive descriptor extent/state", directory)
	}
	want, ok := in.localFuncrefDescriptor(target)
	if !ok {
		t.Fatal("pinned passive target descriptor")
	}
	payload := unsafe.Slice((*byte)(offHeapPtr(uintptr(pointer))), coreruntime.TableEntryBytes)
	wantRow := unsafe.Slice((*byte)(offHeapPtr(uintptr(want))), coreruntime.TableEntryBytes)
	if !bytes.Equal(payload, wantRow) {
		t.Fatal("pinned passive payload changed canonical target identity")
	}
}

func TestRV64Core3BrOnNullPinnedDelta(t *testing.T) {
	tests := []struct {
		name   string
		source []byte
		hash   string
		target int
		run    func(*testing.T, *Instance)
	}{
		{
			name: "basic", source: rv64Core3BrOnNullBasicSource(2), hash: "f09b6889c9dce04626fe23bbc6e77089ebc779e8596bc7aad69c9a6555fca89b", target: 2,
			run: func(t *testing.T, in *Instance) {
				if got, err := in.Invoke("unreachable"); got != nil || err == nil {
					t.Fatal("pinned unreachable result", got, err)
				} else {
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable {
						t.Fatal("pinned unreachable trap", err)
					}
				}
				for name, want := range map[string]uint64{"nullable-null": uint64(^uint32(0)), "nonnullable-f": 7, "nullable-f": 7} {
					if got, err := in.Invoke(name); err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal("pinned br_on_null result/reuse", name, got, err)
					}
				}
			},
		},
		{
			name: "args", source: rv64Core3BrOnNullArgsSource(), hash: "7d3b317fcdedb31f02ca449422d2368a4fa8e2ed8315d95c6af985f9e2965eaf", target: 0,
			run: func(t *testing.T, in *Instance) {
				const dirtyThree = uint64(0xdeadbeef00000003)
				for name, want := range map[string]uint64{"args-null": 3, "args-f": 9} {
					if got, err := in.Invoke(name, dirtyThree); err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal("pinned block argument normalization", name, got, err)
					}
				}
			},
		},
	}
	for _, tc := range tests {
		if got := fmt.Sprintf("%x", sha256.Sum256(tc.source)); got != tc.hash {
			t.Fatalf("%s pinned fixture hash = %s, want %s", tc.name, got, tc.hash)
		}
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", tc.name, artifact), func(t *testing.T) {
				if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" && !artifact {
					t.Skip("compiler-free artifact case only")
				}
				c := rv64NativeTailCompile(t, "core3-br-on-null-"+tc.name, tc.source, artifact)
				wantFeatures := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
				if !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.requiredFeatures != wantFeatures || !c.needsFuncRefDescs() || c.tableCount() != 0 {
					t.Fatal("pinned br_on_null source/artifact witnesses", c.requiredFeatures)
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				c.Close()
				rv64AssertCore3PassiveTarget(t, in, tc.target)
				tc.run(t, in)
			})
		}
	}
}

func TestRV64Core3BrOnNullPinnedDeltaGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source and hostile artifact admission")
	}
	// Changing the declaration target to reference-valued function zero is a
	// valid Core module, but it escapes the selected numeric descriptor profile.
	if c, err := rv64NativeTailConfig().Compile(rv64Core3BrOnNullBasicSource(0)); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("reference-valued passive target escaped the pinned delta", err)
	}
	typedPassive := rv64ReplaceTableStageSection(t, rv64Core3BrOnNullBasicSource(2), 9, wasmtest.Section(9, []byte{1, 5, 0x70, 1, 0xd2, 2, 0x0b}))
	if c, err := rv64NativeTailConfig().Compile(typedPassive); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("typed-expression passive shape escaped the pinned legacy delta", err)
	}
	base := rv64NativeTailCompile(t, "core3-br-on-null-basic", rv64Core3BrOnNullBasicSource(2), true)
	mutations := map[string]struct {
		mutate func(*Compiled)
		repair bool
	}{
		"missing-bulk-feature": {mutate: func(c *Compiled) { c.requiredFeatures &^= CoreFeatureBulkMemoryOperations }, repair: true},
		"active-without-table": {mutate: func(c *Compiled) { c.passiveElems[0].Mode = ElemModeActive }},
		"empty-passive":        {mutate: func(c *Compiled) { c.passiveElems[0].Values = nil }},
		"private-target":       {mutate: func(c *Compiled) { c.passiveElems[0].Values[0].FuncIndex = 0 }},
		"foreign-table":        {mutate: func(c *Compiled) { c.passiveElems[0].TableIndex = 1 }},
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			bad := *base.executionView()
			bad.passiveElems = slices.Clone(bad.passiveElems)
			bad.passiveElems[0].Values = slices.Clone(bad.passiveElems[0].Values)
			mutation.mutate(&bad)
			if err := railshotValidateCompiled(&bad); err == nil {
				t.Fatal("hostile retained Core 3 delta admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, loadErr := LoadTrustedArtifact(blob)
			if loaded != nil {
				defer loaded.Close()
			}
			if mutation.repair {
				if loadErr != nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
					t.Fatal("serialized Core 3 delta did not reconstruct structural features", loadErr)
				}
				return
			}
			if loadErr == nil {
				t.Fatal("hostile serialized Core 3 delta admitted")
			}
		})
	}
}

func TestRV64Core3BrOnNullCompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	if c, err := Compile(nil, rv64Core3BrOnNullBasicSource(2)); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			c.Close()
		}
		t.Fatal("compiler-free pinned Core 3 delta used source compilation", err)
	}
}
