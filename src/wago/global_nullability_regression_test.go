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
	if err := in.SetGlobalValue("g", ValueGCRef(NullGCRef())); err == nil {
		t.Fatal("SetGlobalValue accepted null for non-null (ref 0) global")
	}
}
