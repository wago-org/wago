package optimization

import "testing"

func TestRetiredARM64PoliciesAreNotAdvertised(t *testing.T) {
	for _, name := range []string{"guarded-test-ccmp", "streaming-i32-reduction", "dot-loop-vector", "crowded-products", "linear-integer-tree", "loop-trap-cell", "prepared-light-entry"} {
		if Exists("arm64", name) {
			t.Fatalf("retired policy %q is advertised", name)
		}
		if _, ok := Lookup("arm64", name); ok {
			t.Fatalf("retired policy %q is selectable", name)
		}
	}
}
