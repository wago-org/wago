package wasm

import (
	"fmt"
	"testing"
)

// BenchmarkSupertypeChainQueries measures the repeated ancestry checks made
// while validating references to one supertype in a long type chain.
func BenchmarkSupertypeChainQueries(b *testing.B) {
	for _, count := range []int{125, 250, 500, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := &Module{Types: make([]RecType, count)}
			for i := range m.Types {
				var supers []TypeIdx
				if i != 0 {
					supers = []TypeIdx{{Index: uint32(i - 1)}}
				}
				m.Types[i] = openStructType(nil, supers...)
			}
			v := &moduleValidator{m: m}
			if err := v.validateModule(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for child := 1; child < count; child++ {
					if !v.typeIdxSuperSubtype(TypeIdx{Index: uint32(child)}, TypeIdx{Index: 0}) {
						b.Fatal("missing supertype")
					}
				}
			}
		})
	}
}

func TestSupertypeAncestryKeepsStructuralAndRecursiveChecks(t *testing.T) {
	m := &Module{Types: make([]RecType, 67)}
	for i := 0; i < 64; i++ {
		var supers []TypeIdx
		if i != 0 {
			supers = []TypeIdx{{Index: uint32(i - 1)}}
		}
		m.Types[i] = openStructType(nil, supers...)
	}
	m.Types[64] = openArrayType(field(I32, Var))
	m.Types[65] = openStructType(nil)
	m.Types[66] = RecType{SubTypes: []SubType{
		{Final: false, Comp: CompType{Kind: CompStruct}},
		{Final: false, Supers: []TypeIdx{{Index: 0, Rec: true}}, Comp: CompType{Kind: CompStruct}},
	}}
	v := &moduleValidator{m: m, funcIndex: -1}
	if err := v.validateModule(); err != nil {
		t.Fatal(err)
	}
	if len(v.superEnter) != 68 || len(v.superExit) != 68 {
		t.Fatalf("ancestry index sizes = %d, %d", len(v.superEnter), len(v.superExit))
	}
	for _, tc := range []struct {
		a, b uint32
		want bool
	}{
		{63, 0, true},   // declared chain
		{0, 63, false},  // reverse direction
		{63, 64, false}, // different kind
		{63, 65, true},  // structural equivalence through a root
		{67, 66, true},  // recursive-local supertype
	} {
		if got := v.typeIdxSuperSubtype(TypeIdx{Index: tc.a}, TypeIdx{Index: tc.b}); got != tc.want {
			t.Errorf("type %d <: type %d = %t, want %t", tc.a, tc.b, got, tc.want)
		}
	}
	m.Types = append(m.Types, ft(nil, []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0}), false))}))
	m.FuncTypes = make([]TypeIdx, 8)
	m.Code = make([]Func, 8)
	for i := range m.Code {
		m.FuncTypes[i] = TypeIdx{Index: 68}
		m.Code[i].Body.Instrs = []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: Ref(true, IndexedHeap(TypeIdx{Index: 63}), false)}}}
	}
	if err := ValidateModuleWithWorkers(m, 4); err != nil {
		t.Fatalf("parallel validation: %v", err)
	}
}

func BenchmarkSupertypeChainMetadata(b *testing.B) {
	const count = 1000
	m := &Module{Types: make([]RecType, count)}
	for i := range m.Types {
		var supers []TypeIdx
		if i != 0 {
			supers = []TypeIdx{{Index: uint32(i - 1)}}
		}
		m.Types[i] = openStructType(nil, supers...)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := &moduleValidator{m: m}
		if err := v.validateModule(); err != nil {
			b.Fatal(err)
		}
	}
}

// Shallow declared chains use the existing walker without allocating a full
// ancestry index. Deep chains retain the index for repeated reference checks.
func TestSupertypeAncestryIndexRequiresDeepChain(t *testing.T) {
	for _, tc := range []struct {
		count, depth int
		indexed      bool
	}{
		{64, 1, false}, {1024, 1, false}, {64, 31, false},
		{64, 32, true}, {1024, 1023, true},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.count, tc.depth), func(t *testing.T) {
			m := &Module{Types: make([]RecType, tc.count)}
			for i := range m.Types {
				var supers []TypeIdx
				if i > 0 && i <= tc.depth {
					supers = []TypeIdx{{Index: uint32(i - 1)}}
				}
				m.Types[i] = openStructType(nil, supers...)
			}
			v := &moduleValidator{m: m, funcIndex: -1}
			if err := v.validateModule(); err != nil {
				t.Fatal(err)
			}
			if got := len(v.superEnter) != 0; got != tc.indexed {
				t.Fatalf("ancestry index present=%t, want %t", got, tc.indexed)
			}
			if !v.typeIdxSuperSubtype(TypeIdx{Index: uint32(tc.depth)}, TypeIdx{Index: 0}) {
				t.Fatal("lost declared ancestry")
			}
		})
	}
}
