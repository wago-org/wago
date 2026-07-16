package wago

import (
	"strings"
	"testing"
)

func TestPreparedFunctionInvokeAndCacheIndependence(t *testing.T) {
	if _, err := (*PreparedFunction)(nil).Invoke(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("nil prepared invoke error = %v", err)
	}
	in, err := Instantiate(MustCompile(benchAddOneModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Replace every tiny Instance cache slot; the prepared signature must own its
	// result-width metadata rather than aliasing a round-robin cache slot.
	for i := range in.ic {
		in.ic[i] = invokeCache{export: "other", valid: true, resultWide: []bool{true, true}}
	}
	got, err := fn.Invoke(I32(41))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("result = %v, want [42]", got)
	}
	if _, err := fn.Invoke(); err == nil || !strings.Contains(err.Error(), "expects 1") {
		t.Fatalf("arity error = %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := fn.Invoke(I32(1)); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("invoke after close error = %v", err)
	}
}

func BenchmarkPreparedInvokeAddOne(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := fn.Invoke(I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}
