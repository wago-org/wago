package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func FuzzLowerGCTypeDescs(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0},
		{1, 0, 1, 0, 2, 0, 3},
		{2, 1, 2, 3, 4, 5, 6, 7, 8},
		{0xff, 0x80, 0x40, 0x20, 0x10, 0x08},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 128 {
			data = data[:128]
		}
		types := fuzzGCRecTypes(data)
		descs, err := LowerGCTypeDescs(types)
		if err != nil {
			return
		}
		want := 0
		for _, rt := range types {
			want += len(rt.SubTypes)
		}
		if len(descs) != want {
			t.Fatalf("descriptor count = %d, want flattened type count %d", len(descs), want)
		}
		for i, d := range descs {
			if d.ID != gc.TypeID(i) {
				t.Fatalf("descriptor %d ID = %d, want %d", i, d.ID, i)
			}
		}
		if err := gc.ValidateTypeDescs(descs); err != nil {
			t.Fatalf("lowered descriptors failed runtime validation: %v", err)
		}
	})
}

type gcDescFuzzBytes struct {
	data []byte
	pos  int
}

func (r *gcDescFuzzBytes) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *gcDescFuzzBytes) n(limit int) int {
	if limit <= 0 {
		return 0
	}
	return int(r.next()) % limit
}

func fuzzGCRecTypes(data []byte) []wasm.RecType {
	r := gcDescFuzzBytes{data: data}
	groupCount := r.n(4)
	groupLens := make([]int, groupCount)
	total := 0
	for i := range groupLens {
		groupLens[i] = r.n(4)
		total += groupLens[i]
	}

	types := make([]wasm.RecType, groupCount)
	flatBase := 0
	for i, recLen := range groupLens {
		subs := make([]wasm.SubType, recLen)
		for j := range subs {
			subs[j] = fuzzGCSubType(&r, total, flatBase, recLen)
		}
		types[i] = wasm.RecType{SubTypes: subs}
		flatBase += recLen
	}
	return types
}

func fuzzGCSubType(r *gcDescFuzzBytes, total, recBase, recLen int) wasm.SubType {
	_ = recBase
	switch r.n(4) {
	case 0:
		return fn()
	case 1:
		fields := make([]wasm.FieldType, r.n(5))
		for i := range fields {
			fields[i] = field(fuzzGCStorage(r, total, recLen))
		}
		return st(fields...)
	case 2:
		return arr(fuzzGCStorage(r, total, recLen))
	default:
		return wasm.SubType{Final: true, Comp: wasm.CompType{Kind: wasm.CompTypeKind(255)}}
	}
}

func fuzzGCStorage(r *gcDescFuzzBytes, total, recLen int) wasm.StorageType {
	nullable := r.next()&1 == 0
	switch r.n(14) {
	case 0:
		return val(wasm.I32)
	case 1:
		return val(wasm.I64)
	case 2:
		return val(wasm.F32)
	case 3:
		return val(wasm.F64)
	case 4:
		return packed(wasm.PackI8)
	case 5:
		return packed(wasm.PackI16)
	case 6:
		return ref(nullable, wasm.HeapAny)
	case 7:
		return ref(nullable, wasm.HeapEq)
	case 8:
		return ref(nullable, wasm.HeapI31)
	case 9:
		return ref(nullable, wasm.HeapFunc)
	case 10:
		if total == 0 {
			return concrete(nullable, 0)
		}
		return concrete(nullable, uint32(r.n(total)))
	case 11:
		return concrete(nullable, uint32(total+r.n(3)))
	case 12:
		if recLen == 0 {
			return concreteRec(nullable, 0)
		}
		return concreteRec(nullable, uint32(r.n(recLen+2)))
	default:
		return wasm.StorageType{Packed: true, Pack: wasm.PackType(255)}
	}
}
