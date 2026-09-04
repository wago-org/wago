package compiler

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

func testFunctionArtifact(t *testing.T) FunctionArtifact {
	t.Helper()
	input := Input{Runtime: RuntimeContract{ABIRevision: runtimeabi.Revision}, Target: Target{GOOS: "linux", GOARCH: "arm64", Mode: TargetCompatibility}, Objective: ObjectiveSpeed, Bounds: BoundsExplicit, ConfigurationFingerprint: [32]byte{2}}
	identity, err := NewFunctionArtifactIdentity(input, EngineDragline, 7, []byte{0x20, 0, 0x0b}, [32]byte{1}, [32]byte{3}, [32]byte{4}, [32]byte{5}, 1)
	if err != nil {
		t.Fatal(err)
	}
	artifact := NewFunctionArtifact(identity, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	artifact.PrivateEntry = 4
	artifact.Relocations = []FunctionRelocation{{Offset: 1, Target: 9, Kind: RelocationCall}}
	artifact.Traps = []FunctionTrap{{Offset: 2, WasmOffset: 3, Code: 1}}
	artifact.Roots = []RootLocation{{Index: 16, Kind: RootLocationStack, Bank: RootBankCollector}}
	artifact.Safepoints = []FunctionSafepoint{{Offset: 3, RootCount: 1}}
	artifact.Sources = []FunctionSourceMap{{NativeOffset: 0, WasmOffset: 1}, {NativeOffset: 4, WasmOffset: 2}}
	return artifact
}

func TestFunctionArtifactStrictCanonicalRoundTrip(t *testing.T) {
	want := testFunctionArtifact(t)
	encoded, err := MarshalFunctionArtifact(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalFunctionArtifact(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := MarshalFunctionArtifact(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("artifact encoding is not canonical:\n%s\n%s", encoded, reencoded)
	}
	got.Identity.Function++
	if _, err := MarshalFunctionArtifact(got); err == nil {
		t.Fatal("corrupt identity fingerprint accepted")
	}
}

func TestFunctionArtifactRejectsMalformedMetadata(t *testing.T) {
	tests := []FunctionArtifact{
		func() FunctionArtifact {
			a := testFunctionArtifact(t)
			a.AdapterReturnOffset = uint32(len(a.Code))
			return a
		}(),
		func() FunctionArtifact {
			a := testFunctionArtifact(t)
			a.Relocations[0].Offset = uint32(len(a.Code))
			return a
		}(),
		func() FunctionArtifact { a := testFunctionArtifact(t); a.Safepoints[0].RootCount = 2; return a }(),
		func() FunctionArtifact {
			a := testFunctionArtifact(t)
			a.Safepoints[0].ID = codegen.GCSafepointIDMax + 1
			return a
		}(),
		func() FunctionArtifact {
			a := testFunctionArtifact(t)
			a.Roots = append(a.Roots, RootLocation{Index: 3, Kind: 1, Bank: 1})
			return a
		}(),
		func() FunctionArtifact {
			a := testFunctionArtifact(t)
			a.Roots = append(a.Roots, RootLocation{Index: 3, Kind: 1, Bank: 1})
			a.Safepoints[0].RootStart = 1
			return a
		}(),
		func() FunctionArtifact {
			a := testFunctionArtifact(t)
			a.Safepoints = append(a.Safepoints, FunctionSafepoint{Offset: 5, RootCount: 1})
			return a
		}(),
		func() FunctionArtifact { a := testFunctionArtifact(t); a.Sources[1].NativeOffset = 0; return a }(),
	}
	for _, artifact := range tests {
		if _, err := MarshalFunctionArtifact(artifact); err == nil {
			t.Fatalf("malformed artifact accepted: %#v", artifact)
		}
	}
	encoded, err := MarshalFunctionArtifact(testFunctionArtifact(t))
	if err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"unknown":1}`)...)
	if _, err := UnmarshalFunctionArtifact(unknown); err == nil {
		t.Fatal("unknown artifact field accepted")
	}
}
