package shared

import "testing"

func TestAdapterShapeHashNormalizesRelocation(t *testing.T) {
	a := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	b := append([]byte(nil), a...)
	copy(b[2:6], []byte{9, 10, 11, 12})
	if got, want := AdapterShapeHash(b, 2, 4), AdapterShapeHash(a, 2, 4); got != want {
		t.Fatalf("relocation changed shape hash: got %#x, want %#x", got, want)
	}
	b[0]++
	if AdapterShapeHash(b, 2, 4) == AdapterShapeHash(a, 2, 4) {
		t.Fatal("non-relocation byte did not change shape hash")
	}
}
