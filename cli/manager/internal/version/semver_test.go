package version

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.5.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
		{"1.0", "1.0.0", -1},
		{"1.x", "1.2", 1},
	}
	for _, test := range tests {
		if got := Compare(test.a, test.b); got != test.want {
			t.Fatalf("Compare(%q, %q) = %d, want %d", test.a, test.b, got, test.want)
		}
	}
}

func TestParseNumeric(t *testing.T) {
	if number, ok := ParseNumeric("012"); !ok || number != 12 {
		t.Fatalf("ParseNumeric = %d, %v", number, ok)
	}
	if _, ok := ParseNumeric("1x"); ok {
		t.Fatal("ParseNumeric accepted a non-numeric component")
	}
}
