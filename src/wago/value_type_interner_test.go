package wago

import (
	"fmt"
	"slices"
	"testing"
	"unsafe"
)

func internerDescriptor(i uint32) ValueTypeDescriptor {
	return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: i}}}
}

func checkInternerSequence(t *testing.T, sequence []ValueTypeDescriptor) {
	t.Helper()
	var got, want []ValueTypeDescriptor
	var cache valueTypeInterner
	for step, d := range sequence {
		a, b := cache.intern(&got, d), internValueType(&want, d)
		if a != b || !slices.Equal(got, want) {
			t.Fatalf("step %d: index %d, want %d; pools equal: %v", step, a, b, slices.Equal(got, want))
		}
	}
}

func TestValueTypeInternerTransitions(t *testing.T) {
	checkInternerSequence(t, nil)
	for _, n := range []int{0, 1, 4, 31, 32, 33, 128} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var sequence []ValueTypeDescriptor
			for i := 0; i < n; i++ {
				sequence = append(sequence, internerDescriptor(uint32(i)))
			}
			// Hit, miss, hit, alternating values, and growth past the recent index.
			for _, i := range []uint32{0, 0, 129, 129, 0, 129, 0, 130, 129, 129} {
				sequence = append(sequence, internerDescriptor(i))
			}
			for i := 0; i < 256; i++ {
				sequence = append(sequence, internerDescriptor(129))
			}
			checkInternerSequence(t, sequence)
		})
	}
}

func TestValueTypeInternerExactFields(t *testing.T) {
	var sequence []ValueTypeDescriptor
	for i := uint32(0); i < 33; i++ {
		sequence = append(sequence, internerDescriptor(i))
	}
	base := internerDescriptor(128)
	variants := []ValueTypeDescriptor{base, base, base, base, base, base}
	variants[0].Kind = ValueTypeI32
	variants[1].Ref.Nullable = false
	variants[2].Ref.Exact = true
	variants[3].Ref.Heap.Abstract = AbstractHeapStruct
	variants[4].Ref.Heap.Defined = false
	variants[5].Ref.Heap.TypeIndex++
	for _, d := range variants {
		sequence = append(sequence, base, d, d, base)
	}
	checkInternerSequence(t, sequence)
}

func FuzzValueTypeInterner(f *testing.F) {
	f.Add([]byte{0, 1, 31, 32, 33, 128, 255})
	f.Add([]byte{1, 1, 1, 2, 1, 2})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			data = data[:512]
		}
		sequence := make([]ValueTypeDescriptor, 0, 33+len(data))
		for i := uint32(0); i < 33; i++ {
			sequence = append(sequence, internerDescriptor(i))
		}
		for _, v := range data {
			d := internerDescriptor(uint32(v))
			d.Kind = ValueTypeKind(v % 6)
			d.Ref.Nullable = v&1 != 0
			d.Ref.Exact = v&2 != 0
			d.Ref.Heap.Defined = v&4 != 0
			d.Ref.Heap.Abstract = AbstractHeapType(v % 13)
			sequence = append(sequence, d)
		}
		checkInternerSequence(t, sequence)
	})
}

func TestValueTypeInternerNoAllocations(t *testing.T) {
	pool := make([]ValueTypeDescriptor, 0, 128)
	if n := testing.AllocsPerRun(100, func() {
		pool = pool[:0]
		var cache valueTypeInterner
		for i := uint32(0); i < 128; i++ {
			d := internerDescriptor(i)
			cache.intern(&pool, d)
			cache.intern(&pool, d)
		}
	}); n != 0 {
		t.Fatalf("allocations = %g, want 0", n)
	}
}

func TestValueTypeInternerLayout(t *testing.T) {
	if unsafe.Sizeof(valueTypeInterner(0)) != 4 {
		t.Fatal("cache is not one uint32")
	}
	t.Logf("Compiled=%d Module=%d Instance=%d descriptor=%d", unsafe.Sizeof(Compiled{}), unsafe.Sizeof(Module{}), unsafe.Sizeof(Instance{}), unsafe.Sizeof(ValueTypeDescriptor{}))
}
