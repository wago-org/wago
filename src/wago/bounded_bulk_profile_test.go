//go:build wago_profile && linux && amd64 && !tinygo

package wago

import "testing"

func TestBoundedBulkRejectsProfileScheduling(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true})
	defer session.Close()
	c, err := NewRuntimeConfig().WithCodeProfile(session).Compile(boundedBulkFixture(64))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.boundedBulkPlans) != 0 {
		t.Fatal("profiled compilation retained bulk scheduling proof")
	}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("performance")
	if err != nil {
		t.Fatal(err)
	}
	got, err := fn.Invoke(65536)
	if err != nil || len(got) != 1 || got[0] != 896 {
		t.Fatalf("result=%v err=%v", got, err)
	}
}
