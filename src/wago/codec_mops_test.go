package wago

import "testing"

func TestCompiledCodecPreservesARM64MOPSRequirement(t *testing.T) {
	encoded, err := marshalCompiled(&Compiled{requiresARM64MOPS: true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err := unmarshalCompiled(&decoded, encoded[5:]); err != nil {
		t.Fatal(err)
	}
	if !decoded.RequiresARM64MOPS() {
		t.Fatal("round trip lost ARM64 MOPS requirement")
	}
	if decoded.requiredFeatures != 0 {
		t.Fatalf("MOPS requirement leaked into WebAssembly feature bits: %#x", decoded.requiredFeatures)
	}
}

func TestCompiledCodecPreservesARM64SHA2Requirement(t *testing.T) {
	encoded, err := marshalCompiled(&Compiled{requiresARM64SHA2: true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err := unmarshalCompiled(&decoded, encoded[5:]); err != nil {
		t.Fatal(err)
	}
	if !decoded.RequiresARM64SHA2() {
		t.Fatal("round trip lost ARM64 SHA2 requirement")
	}
	if decoded.requiredFeatures != 0 {
		t.Fatalf("SHA2 requirement leaked into WebAssembly feature bits: %#x", decoded.requiredFeatures)
	}
}
