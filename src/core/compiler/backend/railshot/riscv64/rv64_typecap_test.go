//go:build linux && !tinygo

package riscv64

import (
	"strconv"
	"testing"

	amd "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func typeCapU32(n int) []byte {
	var b []byte
	for {
		x := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			x |= 0x80
		}
		b = append(b, x)
		if n == 0 {
			return b
		}
	}
}

func typeCapSection(id byte, payload []byte) []byte {
	section := append([]byte{id}, typeCapU32(len(payload))...)
	return append(section, payload...)
}

// Type zero is () -> (); the remaining declarations are empty structs. The
// shapes cover singleton groups, one large recursive group, cross-group
// subtyping and a function-only control.
func typeCapModule(n int, shape string) []byte {
	groups := n
	if shape == "rec" {
		groups = 2
	}
	if shape == "mixed" {
		groups = n - 1
	}
	payload := append(typeCapU32(groups), 0x60, 0, 0)
	if shape == "rec" {
		payload = append(payload, 0x4e)
		payload = append(payload, typeCapU32(n-1)...)
	}
	start := 1
	if shape == "mixed" {
		payload = append(payload, 0x50, 0, 0x5f, 0, 0x4f, 1, 1, 0x5f, 0, 0x4e, 2, 0x5f, 0, 0x5f, 0)
		start = 5
	}
	for i := start; i < n; i++ {
		if shape == "numeric" {
			payload = append(payload, 0x60, 0, 0)
		} else {
			payload = append(payload, 0x5f, 0)
		}
	}
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	module = append(module, typeCapSection(1, payload)...)
	module = append(module, typeCapSection(3, []byte{1, 0})...)
	return append(module, typeCapSection(10, []byte{1, 2, 0, 0x0b})...)
}

func TestRV64TypeDeclarationBoundary(t *testing.T) {
	for _, shape := range []string{"single", "rec", "mixed", "numeric"} {
		for _, count := range []int{MaxFunctions, MaxFunctions + 1} {
			t.Run(shape+"/"+strconv.Itoa(count), func(t *testing.T) {
				source := typeCapModule(count, shape)
				if err := wasm.ValidateByteBackedModule(source); err != nil {
					t.Fatal("validation control", err)
				}
				decoded, err := wasm.DecodeModuleByteBacked(source)
				if err != nil {
					t.Fatal("default decode control", err)
				}
				m := decoded.Module
				if directory, ok := newInternalReferenceTypeDirectory(m.Types); !ok || len(directory.flat) != count {
					t.Fatalf("directory count: ok=%t length=%d", ok, len(directory.flat))
				}
				if _, err := amd.CompileModuleWith(m, amd.CompileOptions{DeferCodeMapping: true}); err != nil {
					t.Fatal("amd64 control", err)
				}
				if shape != "numeric" && !admitRV64GCRootedScalarModule(m) {
					t.Fatal("rooted decoded admission")
				}
				if shape != "numeric" && !AdmitGCRootedScalarSource(source) {
					t.Fatal("rooted wire admission")
				}
				options := CompileOptions{production: true, publicStarts: true, gcRootedScalar: true, DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
				if shape != "numeric" {
					if _, err := compileDecodedModule(m, scalarRegisters[:], options); err != nil {
						t.Fatal("generic rooted compilation", err)
					}
					mode := emissionMode{production: true, interruptible: true, invocationContext: true, foreignTableCalls: true, gcRootedScalar: true}
					if _, err := compileSWARDecodedModuleProfile(m, mode, swarModulePlain, swarStateProfile{publicStarts: true, exports: true}); err != nil {
						t.Fatal("SWAR rooted compilation", err)
					}
				}
				if err := admitSWARDecodedBudgetReferences(m, true, true, true, true); err != nil {
					t.Fatal("SWAR decoded budget", err)
				}
				if _, err := compileSWARProductionModule(m, options); err != nil {
					t.Fatal("hybrid production compilation", err)
				}
				if _, _, _, _, err := admitProductionDynamicTails(m, true, false, false, false); err != nil {
					t.Fatal("control declaration admission", err)
				}
				if _, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true}); err != nil {
					t.Fatal("public decoded compilation", err)
				}
			})
		}
	}
}

func TestRV64TypeIndex1024DynamicTail(t *testing.T) {
	types := typeCapU32(MaxFunctions + 1)
	for range MaxFunctions + 1 {
		types = append(types, 0x60, 0, 0)
	}
	name := func(value string) []byte {
		return append(typeCapU32(len(value)), value...)
	}
	entry := append(name("peer"), name("run")...)
	entry = append(entry, 0, 0)
	imports := append(typeCapU32(1), entry...)
	body := append([]byte{0, 0x41, 0, 0x13}, typeCapU32(MaxFunctions)...)
	body = append(body, 0, 0x0b)
	codes := append(typeCapU32(1), typeCapU32(len(body))...)
	codes = append(codes, body...)

	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	module = append(module, typeCapSection(1, types)...)
	module = append(module, typeCapSection(2, imports)...)
	module = append(module, typeCapSection(3, []byte{1, 0})...)
	module = append(module, typeCapSection(4, []byte{1, 0x70, 1, 2, 2})...)
	module = append(module, typeCapSection(9, []byte{2, 0, 0x41, 0, 0x0b, 1, 0, 3, 0, 1, 1})...)
	module = append(module, typeCapSection(10, codes)...)

	image, err := compileDynamicTailSource(module, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(image.plans) != 1 || image.plans[0].typeIndex != MaxFunctions {
		t.Fatal("tail plan did not retain local type index 1024", image.plans)
	}
}

func TestRV64TypeIndex1024LiveReferences(t *testing.T) {
	structType := func(fields ...wasm.FieldType) wasm.SubType {
		return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: fields}}
	}
	refField := func(index uint32, rec bool) wasm.FieldType {
		ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: index, Rec: rec}), false))
		return wasm.NewFieldType(wasm.StorageVal(ref), wasm.Const)
	}

	t.Run("recursive-member", func(t *testing.T) {
		members := make([]wasm.SubType, MaxFunctions+1)
		for i := range members {
			members[i] = structType()
		}
		members[MaxFunctions] = structType(refField(MaxFunctions, true))
		m := &wasm.Module{Types: []wasm.RecType{{SubTypes: members}}}
		if !admitRV64GCRootedScalarModule(m) {
			t.Fatal("rooted recursive member at local index 1024 rejected")
		}
		directory, ok := newInternalReferenceTypeDirectory(m.Types)
		if !ok || !directory.lookupMemberReference(MaxFunctions) {
			t.Fatal("recursive member directory lookup failed")
		}
	})

	t.Run("cross-group-subtype", func(t *testing.T) {
		groups := make([]wasm.RecType, MaxFunctions+2)
		for i := range groups {
			groups[i] = wasm.RecType{SubTypes: []wasm.SubType{structType()}}
		}
		groups[MaxFunctions].SubTypes[0].Final = false
		groups[MaxFunctions].SubTypes[0].HasPrefix = true
		groups[MaxFunctions+1].SubTypes[0].HasPrefix = true
		groups[MaxFunctions+1].SubTypes[0].Supers = []wasm.TypeIdx{{Index: MaxFunctions}}
		m := &wasm.Module{Types: groups}
		if !admitRV64GCRootedScalarModule(m) {
			t.Fatal("cross-group subtype of local index 1024 rejected")
		}
		if directory, ok := newInternalReferenceTypeDirectory(m.Types); !ok || len(directory.flat) != MaxFunctions+2 {
			t.Fatal("cross-group subtype directory rejected")
		}
	})
}

func (directory internalReferenceTypeDirectory) lookupMemberReference(index uint32) bool {
	sub, _, _, ok := directory.lookup(index)
	if !ok || len(sub.Comp.Fields) != 1 {
		return false
	}
	heap := sub.Comp.Fields[0].Storage().Val().Ref().Heap()
	return heap.Kind() == wasm.HeapTypeIndex && heap.Type().Rec && heap.Type().Index == index
}
