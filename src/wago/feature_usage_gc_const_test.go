package wago

import "testing"

func TestConstNullHeapRequiredFeatures(t *testing.T) {
	for _, tc := range []struct {
		heap byte
		want CoreFeatures
	}{
		{0x70, CoreFeatureReferenceTypes},
		{0x6f, CoreFeatureReferenceTypes},
		{0, CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences},
		{0x73, CoreFeatureReferenceTypes | CoreFeatureGC},
		{0x72, CoreFeatureReferenceTypes | CoreFeatureGC},
		{0x6b, CoreFeatureReferenceTypes | CoreFeatureGC},
		{0x69, CoreFeatureReferenceTypes | CoreFeatureExceptionHandling},
	} {
		if got := requiredFeaturesForConstExprBytes([]byte{0xd0, tc.heap, 0x0b}, 0); got != tc.want {
			t.Fatalf("ref.null heap %#x features = %s, want %s", tc.heap, got, tc.want)
		}
	}
	if got, want := requiredFeaturesForConstExprBytes([]byte{0xd0, 0x62, 0x00, 0x0b}, 0), CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences; got != want {
		t.Fatalf("exact ref.null features = %s, want %s", got, want)
	}
}

func TestExactReferenceBodyRequiredFeatures(t *testing.T) {
	want := CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"ref.null", []byte{0xd0, 0x62, 0x00, 0x1a, 0x0b}},
		{"block", []byte{0x02, 0x63, 0x62, 0x00, 0xd0, 0x62, 0x00, 0x0b, 0x1a, 0x0b}},
		{"typed select", []byte{0xd0, 0x62, 0x00, 0xd0, 0x62, 0x00, 0x41, 0x00, 0x1c, 0x01, 0x63, 0x62, 0x00, 0x1a, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiredFeaturesForBodyBytes(tc.body); got != want {
				t.Fatalf("features = %s, want %s", got, want)
			}
		})
	}
}

func TestGCConstRequiredFeaturesGlobalScope(t *testing.T) {
	for _, tc := range []struct {
		name           string
		body           []byte
		wantExtended   bool
		wantArithmetic bool
	}{
		{name: "constructor", body: []byte{0x41, 1, 0xfb, 0, 0, 0x0b}},
		{name: "imported-global-constructor", body: []byte{0x23, 0, 0xfb, 0, 0, 0x0b}},
		{name: "local-global-constructor", body: []byte{0x23, 1, 0xfb, 0, 0, 0x0b}, wantExtended: true},
		{name: "arithmetic-constructor", body: []byte{0x41, 1, 0x41, 2, 0x6a, 0xfb, 0, 0, 0x0b}, wantArithmetic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			features := requiredFeaturesForConstExprBytes(tc.body, 1)
			if !features.IsEnabled(CoreFeatureGC) || features.IsEnabled(CoreFeatureExtendedConstExpressions) != tc.wantExtended || features.IsEnabled(CoreFeatureExtendedConst) != tc.wantArithmetic {
				t.Fatalf("features=%s, want GC, extended globals=%t, arithmetic=%t", features, tc.wantExtended, tc.wantArithmetic)
			}
		})
	}
}
