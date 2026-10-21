//go:build wago_profile && !wago_precompiled

package wago

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCodeProfileSidecarSurvivesArtifactReload(t *testing.T) {
	s := NewCodeProfile(CodeProfileOptions{SourceMaps: true, UnwindMaps: true})
	defer s.Close()
	c, err := Compile(NewRuntimeConfig().WithCodeProfile(s), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	sidecar, err := c.MarshalCodeProfileSidecar(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if err := loaded.AttachCodeProfileSidecar(s, artifact, sidecar); err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("identity", 37)
	if err != nil || len(got) != 1 || got[0] != 37 {
		t.Fatalf("reload invocation: %v %v", got, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Close(); err != nil {
		t.Fatal(err)
	}
	events, status := s.Read(0)
	if status.Dropped != 0 || len(events) < 2 || events[0].Image == nil {
		t.Fatalf("reload events: %+v %+v", events, status)
	}
	im := events[0].Image
	if im.ModuleID == "" || len(im.Functions) != 1 || len(im.Regions) == 0 || im.Regions[0].Kind == "unknown" {
		t.Fatalf("sidecar attribution lost: %+v", im)
	}
}

func TestCodeProfileSidecarRejectsMismatchWithoutAttaching(t *testing.T) {
	s := NewCodeProfile(CodeProfileOptions{SourceMaps: true})
	defer s.Close()
	c, err := Compile(NewRuntimeConfig().WithCodeProfile(s), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	sidecar, err := c.MarshalCodeProfileSidecar(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	badArtifact := bytes.Clone(artifact)
	badArtifact[len(badArtifact)-1] ^= 1
	if err := loaded.AttachCodeProfileSidecar(s, badArtifact, sidecar); err == nil {
		t.Fatal("accepted artifact digest mismatch")
	}
	var decoded codeProfileSidecar
	if err := json.Unmarshal(sidecar, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded.Image.Regions[0].Size++
	badRegions, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.AttachCodeProfileSidecar(s, artifact, badRegions); err == nil {
		t.Fatal("accepted out-of-range metadata")
	}
	if err := loaded.AttachCodeProfileSidecar(s, artifact, sidecar); err != nil {
		t.Fatalf("failed after rejected sidecar: %v", err)
	}
}
