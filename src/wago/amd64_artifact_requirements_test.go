//go:build amd64

package wago

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// Artifact-only tests may model instructions the real host cannot execute.
// Keep the compiler's capability cache consistent with the mocked admission
// flags; callers must never invoke the resulting synthetic native images.
func mockAMD64ArtifactCPU(t *testing.T, features shared.AMD64Features) {
	t.Helper()
	cachedAMD64CPUFeatures()
	savedFeatures, savedOK := amd64CPUCache.features, amd64CPUCache.ok
	t.Cleanup(func() { amd64CPUCache.features, amd64CPUCache.ok = savedFeatures, savedOK })
	amd64CPUCache.features, amd64CPUCache.ok = features, true
}

func TestAMD64ArtifactCapabilitySubset(t *testing.T) {
	for bit := shared.AMD64Features(1); bit <= shared.AMD64AVX512; bit <<= 1 {
		if err := checkAMD64Requirements(bit, shared.AMD64KnownFeatures&^bit, true); err == nil {
			t.Fatalf("missing feature %x admitted", bit)
		}
		if err := checkAMD64Requirements(bit, shared.AMD64KnownFeatures, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkAMD64Requirements(0, 0, true); err != nil {
		t.Fatalf("SSE2 artifact rejected on baseline: %v", err)
	}
	if err := checkAMD64Requirements(0, 0, false); err == nil {
		t.Fatal("detection failure admitted")
	}
}

func TestAMD64ArtifactCapabilityRoundTrip(t *testing.T) {
	compiled, err := Compile(nil, benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.requiredAMD64Features != 0 {
		t.Fatalf("integer scalar module requires %x", compiled.requiredAMD64Features)
	}
	host, ok := cachedAMD64CPUFeatures()
	if !ok {
		t.Fatal("host detection failed")
	}
	for _, required := range []shared.AMD64Features{0, host} {
		// Conservative declared requirements on baseline code exercise every
		// supported metadata bit without executing synthetic optimized code.
		compiled, err := Compile(nil, benchAddOneModule())
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		compiled.requiredAMD64Features = required
		data, err := marshalCompiled(compiled)
		if err != nil {
			t.Fatal(err)
		}
		var loaded Compiled
		if err := loaded.UnmarshalBinary(data); err != nil {
			t.Fatal(err)
		}
		if loaded.requiredAMD64Features != required {
			t.Fatalf("roundtrip=%x want=%x", loaded.requiredAMD64Features, required)
		}
		loaded.Close()
		for _, oldVersion := range []byte{2, 3, 4} {
			data[4] = oldVersion
			if err := loaded.UnmarshalBinary(data); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("version %d unsupported", oldVersion)) {
				t.Fatalf("ambiguous version %d artifact admitted: %v", oldVersion, err)
			}
		}
	}
	compiled, err = Compile(nil, benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	compiled.requiredAMD64Features = 1 << 19
	data, err := marshalCompiled(compiled)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(data); err == nil {
		loaded.Close()
		t.Fatal("unknown CPU requirement admitted")
	}
}
