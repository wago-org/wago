package wasm

import "testing"

func TestReaderRefTypeForNull(t *testing.T) {
	for _, test := range []struct {
		name  string
		bytes []byte
		want  RefType
	}{
		{name: "abstract", bytes: []byte{byte(HeapAny)}, want: Ref(true, AbsHeap(HeapAny), false)},
		{name: "indexed", bytes: []byte{0x07}, want: Ref(true, IndexedHeap(TypeIdx{Index: 7}), false)},
		{name: "exact", bytes: []byte{0x62, 0x07}, want: Ref(true, IndexedHeap(TypeIdx{Index: 7}), true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := NewReader(test.bytes)
			got, err := r.RefTypeForNull()
			if err != nil || got != test.want || r.HasNext() {
				t.Fatalf("RefTypeForNull(%x) = %v, %v, remaining=%d", test.bytes, got, err, r.BytesLeft())
			}
		})
	}
	t.Run("exact-abstract", func(t *testing.T) {
		r := NewReader([]byte{0x62, byte(HeapAny)})
		if _, err := r.RefTypeForNull(); err == nil {
			t.Fatal("exact abstract heap type was accepted")
		}
	})
}
