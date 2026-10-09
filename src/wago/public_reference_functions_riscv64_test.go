//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ClosedReferenceSource(vector, tails, indexed, nullable bool) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	heap := wasm.AbsHeap(wasm.HeapFunc)
	heapByte := byte(0x70)
	if indexed {
		heap = wasm.IndexedHeap(wasm.TypeIdx{})
		heapByte = 0
	}
	ref := wasm.RefVal(wasm.Ref(nullable, heap, false))
	signature := func(params, results []wasm.ValType) []byte {
		out := []byte{0x60}
		for _, values := range [][]wasm.ValType{params, results} {
			out = append(out, wasmtest.ULEB(uint32(len(values)))...)
			for _, v := range values {
				if v.Kind() != wasm.ValRef {
					out = append(out, wasm.MustEncodeValType(v))
					continue
				}
				lead := byte(0x64)
				if v.Ref().Nullable() {
					lead = 0x63
				}
				out = append(out, lead, heapByte)
			}
		}
		return out
	}
	types := [][]byte{wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.I32}), signature([]wasm.ValType{ref, wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, ref}), signature([]wasm.ValType{ref}, []wasm.ValType{ref})}
	lead := byte(0x64)
	if nullable {
		lead = 0x63
	}
	// Numeric descriptor targets 1 and 4 surround the internal reference
	// functions 2 and 3. Directory rows must retain those function-index holes.
	root := []byte{0x20, 0, 0x41, 1, 0x71, 4, lead, heapByte, 0xd2, 1, 5, 0xd2, 4, 0x0b, 0x20, 0, 0x20, 1, 0x10, 2, 2, 3, 0x0c, 0, 0x0b, 0xd1, 0x0b}
	if nullable {
		at := bytes.Index(root, []byte{0x20, 0, 0x20, 1, 0x10, 2})
		prefix := []byte{0x20, 0, 0x41, 2, 0x71, 4, 0x63, heapByte, 0xd0, heapByte, 5}
		prefix = append(prefix, root[:at]...)
		prefix = append(prefix, 0x0b)
		root = append(prefix, root[at:]...)
	}
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	forward := []byte{0x20, 0, 0x20, 1, 0x20, 2, call, 3, 0x0b}
	leaf := []byte{0x20, 2, 0x20, 1, 0x41, 5, 0x6a, 0x20, 0, 0x0b}
	export := append(wasmtest.Name("run"), 0, 0)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{5, 1, 0, 2, 2, 0}), wasmtest.Section(7, wasmtest.Vec(export)), wasmtest.Section(9, []byte{1, 3, 0, 2, 1, 4}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(root), wasmtest.Code([]byte{0x0b}), wasmtest.Code(forward), wasmtest.Code(leaf), wasmtest.Code([]byte{0x0b}))))
}

func TestRV64PublicReferenceFunctionsSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, indexed := range []bool{false, true} {
					for _, nullable := range []bool{false, true} {
						name := fmt.Sprintf("reference-functions-v%t-t%t-i%t-n%t", vector, tails, indexed, nullable)
						t.Run(fmt.Sprintf("artifact%t-%s", artifact, name), func(t *testing.T) {
							c := rv64NativeTailCompile(t, name, rv64ClosedReferenceSource(vector, tails, indexed, nullable), artifact)
							if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI != (indexed || !nullable) || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != (indexed || !nullable) || c.requiredFeatures.IsEnabled(CoreFeatureTailCall) != tails {
								t.Fatal("closed source/artifact witnesses", c.requiredFeatures)
							}
							if p, r, err := railshotImportedCallSlots(c, 0, 0); err != nil || p != 0 || r != 0 {
								t.Fatal("closed functions enlarged declared call transport", p, r, err)
							}
							rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
							defer rt.Close()
							in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
							if len(in.funcRefDescs) != 6*coreruntime.FuncRefDescBytes {
								t.Fatal("physical descriptor extent", len(in.funcRefDescs))
							}
							for _, index := range []int{2, 3} {
								row := in.funcRefDescs[(index+1)*coreruntime.FuncRefDescBytes : (index+2)*coreruntime.FuncRefDescBytes]
								if !bytes.Equal(row, make([]byte, len(row))) {
									t.Fatal("noncallable descriptor row", index, row)
								}
								if _, ok := in.localFuncrefDescriptor(index); ok {
									t.Fatal("hole acquired local identity", index)
								}
								pointer := uint64(offHeapSlicePtr(row))
								if _, ok := in.funcrefDescriptorIndex(pointer); ok {
									t.Fatal("hole acquired indexed identity", index)
								}
								if _, ok := rv64PublishedDescriptor(in, pointer); ok {
									t.Fatal("hole published", index)
								}
							}
							for _, index := range []int{1, 4} {
								pointer, ok := in.localFuncrefDescriptor(index)
								if !ok {
									t.Fatal("numeric descriptor missing", index)
								}
								if _, valid := rv64PublishedDescriptor(in, pointer); !valid {
									t.Fatal("numeric descriptor identity", index)
								}
							}
							before := bytes.Clone(in.funcRefDescs)
							args := []uint64{0, 0x7ff0000000000001}
							want := []uint64{args[1], 0, 0}
							if vector {
								args = append(args, 0x8000000000000000)
								want = []uint64{args[1], args[2], 0, 0}
							}
							for _, n := range []uint64{0xdeadbeef00000000, 0xffffffff00000001, 0x8000000000000002, 0x12345678ffffffff, 0x80000000} {
								args[0] = n
								want[len(want)-2] = uint64(uint32(n) + 5)
								want[len(want)-1] = 0
								if nullable && n&2 != 0 {
									want[len(want)-1] = 1
								}
								if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
									t.Fatal("closed retained transport", got, want, err)
								}
							}
							if !bytes.Equal(before, in.funcRefDescs) {
								t.Fatal("descriptor rows changed")
							}
							if a := testing.AllocsPerRun(10, func() {
								got, err := in.Invoke("run", args...)
								if err != nil || !slices.Equal(got, want) {
									panic("closed warm invoke")
								}
							}); a != 0 {
								t.Fatal("warm allocations", a)
							}
							if !in.retainResourceRoot() {
								t.Fatal("physical owner lease")
							}
							func() {
								defer in.releaseResourceRoot()
								nativeExecutionMu.Lock()
								defer nativeExecutionMu.Unlock()
								counts, _, err := rv64NativeTailOwnerCounts(in)
								if err != nil || counts.Functions != 5 || counts.MaskSlots != 5 || counts.NonCallable != 2 {
									t.Fatal("sparse generation counts", counts, err)
								}
								owner, err := rv64NativeDefinedTailOwner(in)
								if err == nil {
									err = owner.Validate()
								}
								if err != nil {
									t.Fatal("cached sparse owner", err)
								}
							}()
						})
					}
				}
			}
		}
	}
}

func rv64BareReferenceFunctionSource() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x10, 1, 0xd1, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}
func TestRV64PublicReferenceFunctionsBaselineAndDestination(t *testing.T) {
	if unsafe.Sizeof(Compiled{}) != 784 {
		t.Fatal("compiled footprint", unsafe.Sizeof(Compiled{}))
	}
	source := rv64BareReferenceFunctionSource()
	if c, err := compatibilityDefaultConfig().Compile(source); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("default source gate", err)
	}
	cfg := rv64NativeTailConfig().WithCoreFeatures(CoreFeatureReferenceTypes)
	c, err := cfg.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.requiredFeatures != CoreFeatureReferenceTypes || !c.nativeReferenceFunctionABI || c.nativeReferenceControlABI || c.nativeReferenceCallABI {
		t.Fatal("baseline feature metadata", c.requiredFeatures)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.codeCache.sealed || loaded.codeCache.flags&compiledCacheWritableCode == 0 || loaded.codeCache.base == 0 {
		t.Fatal("decode did not retain unpublished RW staging")
	}
	beforeBase, beforeMem, beforeRefs, beforeFlags := loaded.codeCache.base, offHeapSlicePtr(loaded.codeCache.mem), loaded.codeCache.refs, loaded.codeCache.flags
	unchanged := func() bool {
		return !loaded.codeCache.sealed && loaded.codeCache.base == beforeBase && offHeapSlicePtr(loaded.codeCache.mem) == beforeMem && loaded.codeCache.refs == beforeRefs && loaded.codeCache.flags == beforeFlags
	}
	if in, err := Instantiate(loaded); in != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("standalone private authority", err)
	}
	if !unchanged() {
		t.Fatal("rejected standalone changed/published code")
	}
	ordinary := NewRuntime()
	defer ordinary.Close()
	mod, err := ordinary.Module(loaded)
	if err != nil {
		t.Fatal("configuration-free binding", err)
	}
	if in, err := ordinary.Instantiate(context.Background(), mod); in != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("default destination permission", err)
	}
	mod.Close()
	if !unchanged() {
		t.Fatal("rejected Runtime changed/published code")
	}
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	in := rv64NativeTokenInstantiate(t, rt, loaded, NewImports())
	if in.c.needsFuncRefDescs() || len(in.funcRefDescs) != coreruntime.FuncRefDescBytes || rv64TailTargetsArenaBytes(in.c) != 0 {
		t.Fatal("null-only module acquired indexed arenas", len(in.funcRefDescs))
	}
	if got, err := in.Invoke("run"); err != nil || !slices.Equal(got, []uint64{1}) {
		t.Fatal("baseline closed execution", got, err)
	}
}
