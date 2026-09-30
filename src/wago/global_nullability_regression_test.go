//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import "testing"

func TestReviewNonNullGCGlobalSetNull(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSharedGlobalProviderModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, exported := range []bool{false, true} {
		if exported {
			if _, err := in.ExportedGlobalObject("g"); err != nil {
				t.Fatal(err)
			}
		}
		if err := in.SetGlobalValue("g", ValueGCRef(NullGCRef())); err == nil {
			t.Fatal("SetGlobalValue accepted null for non-null (ref 0) global")
		}
		got, err := in.Invoke("read")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("global changed after rejected write: %v", got)
		}
	}
}
