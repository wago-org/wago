package compiler

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/profile"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

func TestReplayArtifactStrictCanonicalRoundTrip(t *testing.T) {
	source := []byte("\x00asm\x01\x00\x00\x00")
	compilerProfile := &profile.Module{Version: profile.Version, ModuleHash: sha256.Sum256(source), Source: profile.SourceStatic, Phase: profile.PhaseStartup, FunctionCounts: []uint64{3}}
	input := Input{
		Source:            source,
		Runtime:           RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target:            Target{GOOS: "linux", GOARCH: "amd64", Mode: TargetNative, CPUModel: "zen5", FeatureBits: [4]uint64{3}},
		Profile:           compilerProfile,
		HostEffects:       []HostEffectBinding{{Declared: true, Contract: HostEffectContract{Reads: HostHeapGlobal}}},
		SelectedFunctions: []uint32{7, 9},
	}
	want := NewReplayArtifact(EngineDragline, input, 7, "lower", "unsupported opcode")
	encoded, err := MarshalReplay(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalReplay(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := MarshalReplay(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("replay encoding is not canonical:\n%s\n%s", encoded, reencoded)
	}
	if got.Profile == nil || got.Profile.FunctionCounts[0] != 3 {
		t.Fatalf("replay profile = %#v", got.Profile)
	}
	if len(got.HostEffects) != 1 || !got.HostEffects[0].Declared || got.HostEffects[0].Contract.Reads != HostHeapGlobal {
		t.Fatalf("replay host effects = %#v", got.HostEffects)
	}
	if len(got.SelectedFunctions) != 2 || got.SelectedFunctions[0] != 7 || got.SelectedFunctions[1] != 9 {
		t.Fatalf("replay selected functions = %v", got.SelectedFunctions)
	}
	got.Module[0]++
	if _, err := MarshalReplay(got); err == nil {
		t.Fatal("corrupt module hash accepted")
	}
}

func TestReplayArtifactRejectsUnknownAndTrailingFields(t *testing.T) {
	input := Input{Source: []byte{1}, Target: Target{GOOS: "x", GOARCH: "y"}}
	encoded, err := MarshalReplay(NewReplayArtifact(EngineDragline, input, 0, "emit", "failed"))
	if err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"unknown":true}`)...)
	if _, err := UnmarshalReplay(unknown); err == nil {
		t.Fatal("unknown replay field accepted")
	}
	if _, err := UnmarshalReplay(append(encoded, encoded...)); err == nil {
		t.Fatal("trailing replay accepted")
	}
}
