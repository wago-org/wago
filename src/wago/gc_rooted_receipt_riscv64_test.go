//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"
	"testing"
)

func TestRV64RootedTableFreeI31SegmentsExecute(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarSegmentArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{
		GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for range 32 {
		got, err := in.Invoke("run")
		if err != nil || !slices.Equal(got, []uint64{0xfffffffe, 1, 4, 0xfffffffd}) {
			t.Fatalf("table-free i31 array segments returned %v: %v", got, err)
		}
	}
}

func TestRV64RootedTableFreeSegmentReceiptRejectsInvalidPayload(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarSegmentArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if err := rv64ValidateOwnedTable(view); err != nil {
		t.Fatalf("valid table-free i31 elements: %v", err)
	}
	for name, mutate := range map[string]func(*ElemInit){
		"even-i31":    func(segment *ElemInit) { segment.Values[0].FuncIndex &^= 1 },
		"table-index": func(segment *ElemInit) { segment.TableIndex = 1 },
		"heap-type":   func(segment *ElemInit) { segment.RefType = ValAnyRef },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.passiveElems = slices.Clone(view.passiveElems)
			candidate.passiveElems[0].Values = slices.Clone(view.passiveElems[0].Values)
			mutate(&candidate.passiveElems[0])
			if rv64FreshRootedGC(candidate) || rv64ValidateOwnedTable(candidate) == nil {
				t.Fatal("changed table-free GC elements retained admission")
			}
		})
	}
	for name, mutate := range map[string]func(*Compiled){
		"context-abi":   func(candidate *Compiled) { candidate.nativeTableContextABI = false },
		"reference-abi": func(candidate *Compiled) { candidate.nativeTableReferenceABI = false },
		"register-abi":  func(candidate *Compiled) { candidate.registerABIDisabled = false },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64ValidateOwnedTable(candidate) == nil {
				t.Fatal("changed element ABI retained admission")
			}
		})
	}
}

func TestRV64RootedEmptyFrameMapRequiresSourceProof(t *testing.T) {
	for name, source := range map[string][]byte{
		"initializer-only": rootedScalarSharedAbstractGlobalProviderModule(false),
		"collecting-body":  rootedScalarI31GlobalModule(),
	} {
		t.Run(name, func(t *testing.T) {
			compiled, err := Compile(rv64RootedScalarConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			view := compiled.executionView()
			err = validateCompiledGCFrameRoots(view, &compiledGCFrameRoots{})
			if (err == nil) != (name == "initializer-only") {
				t.Fatalf("empty frame map validation: %v", err)
			}
			candidate := cloneCompiledMetadata(view)
			candidate.code = slices.Clone(view.code)
			candidate.code[0] ^= 1
			if validateCompiledGCFrameRoots(candidate, &compiledGCFrameRoots{}) == nil {
				t.Fatal("changed code retained the empty frame map proof")
			}
		})
	}
}

func TestRV64RootedSharedAbstractGlobalSourceRetainsOwner(t *testing.T) {
	for _, mutable := range []bool{false, true} {
		providerCode, err := Compile(rv64RootedScalarConfig(), rootedScalarSharedAbstractGlobalProviderModule(mutable))
		if err != nil {
			t.Fatal(err)
		}
		defer providerCode.Close()
		consumerCode, err := Compile(rv64RootedScalarConfig(), rootedScalarSharedAbstractGlobalConsumerModule(mutable))
		if err != nil {
			t.Fatal(err)
		}
		defer consumerCode.Close()
		store := newReferenceStore(false)
		defer store.closeRuntime()
		cfg := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}
		provider, err := instantiateCore(providerCode, InstantiateOptions{GC: cfg, store: store})
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		global, err := provider.ExportedGlobalObject("root")
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: cfg, store: store, Imports: testImports("provider.root", global)})
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		if provider.gc == nil || provider.gc != consumer.gc {
			t.Fatal("shared abstract global did not share its collector")
		}
		if err := provider.Close(); err != nil {
			t.Fatal(err)
		}
		if err := consumer.CollectGC(); err != nil {
			t.Fatal(err)
		}
		got, err := consumer.Invoke("read_object")
		if err != nil || !slices.Equal(got, []uint64{40}) {
			t.Fatalf("shared abstract global after provider close: %v, %v", got, err)
		}
	}
}

func TestRV64RootedFunctionGlobalReceipt(t *testing.T) {
	function := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	c := &Compiled{
		Types:             []DefinedTypeDescriptor{{Kind: CompositeTypeFunction}, {Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{{Kind: ValueTypeI32}}}},
		ValueTypes:        []ValueTypeDescriptor{function},
		Funcs:             []FuncSig{{HasTypeIndex: true, TypeIndex: 0}},
		Globals:           []GlobalDef{{Type: ValFuncRef, HasValueType: true, HasInitFunc: true}},
		NeedsFuncRefDescs: true,
	}
	baseline, ok := rv64RootedGlobalDigest(c)
	if !ok {
		t.Fatal("valid indexed ref.func global rejected")
	}
	for name, mutate := range map[string]func(*Compiled){
		"target-index":        func(c *Compiled) { c.Globals[0].InitFunc = 1 },
		"null-nonnullable":    func(c *Compiled) { c.Globals[0].HasInitFunc = false },
		"raw-bits":            func(c *Compiled) { c.Globals[0].Bits = 8 },
		"mixed-initializers":  func(c *Compiled) { c.Globals[0].HasInitGlobal = true },
		"target-type":         func(c *Compiled) { c.Funcs[0].TypeIndex = 1 },
		"missing-descriptors": func(c *Compiled) { c.NeedsFuncRefDescs = false },
		"mutability":          func(c *Compiled) { c.Globals[0].Mutable = true },
		"nullable":            func(c *Compiled) { c.ValueTypes[0].Ref.Nullable = true },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *c
			candidate.Globals = slices.Clone(c.Globals)
			candidate.Funcs = slices.Clone(c.Funcs)
			candidate.ValueTypes = slices.Clone(c.ValueTypes)
			mutate(&candidate)
			if digest, valid := rv64RootedGlobalDigest(&candidate); valid && digest == baseline {
				t.Fatal("changed function global retained receipt")
			}
		})
	}
	c.requiredFeatures |= CoreFeatureExtendedConstExpressions
	c.Globals = append(c.Globals, GlobalDef{Type: ValFuncRef, HasValueType: true, HasInitGlobal: true})
	if _, valid := rv64RootedGlobalDigest(c); !valid {
		t.Fatal("typed prior immutable global.get rejected")
	}
	c.Globals[0].Mutable = true
	if _, valid := rv64RootedGlobalDigest(c); valid {
		t.Fatal("mutable global.get initializer accepted")
	}
}
