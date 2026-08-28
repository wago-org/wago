package codegen

import "testing"

func TestGCRefTargetEncoding(t *testing.T) {
	for _, test := range []struct {
		heap     int64
		nullable bool
		exact    bool
	}{
		{heap: -22},
		{heap: -18, nullable: true},
		{heap: 0},
		{heap: 1<<32 - 1, nullable: true, exact: true},
		{heap: -(1 << 32), exact: true},
	} {
		encoded, ok := EncodeGCRefTarget(test.heap, test.nullable, test.exact)
		if !ok {
			t.Fatalf("EncodeGCRefTarget(%d, %t, %t) rejected", test.heap, test.nullable, test.exact)
		}
		heap, nullable, exact := DecodeGCRefTarget(encoded)
		if heap != test.heap || nullable != test.nullable || exact != test.exact {
			t.Fatalf("round trip = (%d, %t, %t), want (%d, %t, %t)", heap, nullable, exact, test.heap, test.nullable, test.exact)
		}
	}
	if _, ok := EncodeGCRefTarget(1<<32, false, false); ok {
		t.Fatal("out-of-range positive heap type encoded")
	}
	if _, ok := EncodeGCRefTarget(-(1<<32)-1, false, false); ok {
		t.Fatal("out-of-range negative heap type encoded")
	}
}
