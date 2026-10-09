//go:build amd64 && wago_profile

package main

import (
	"encoding/json"
	"github.com/wago-org/wago/internal/jitprofile"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCaptureExistingFib(t *testing.T) {
	data, err := os.ReadFile("../../fixtures/wasm/fib.wasm")
	if err != nil {
		t.Fatal(err)
	}
	s, err := captureBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	// Test binaries omit VCS build info; supply an explicit test identity only.
	s.Provenance.CompilerRevision = "test-build"
	r, err := Compare(s, s)
	if err != nil || !r.Complete || r.Known != 12 || r.Unknown != 0 || len(s.Regions) != 8 {
		t.Fatal(r, err)
	}
	if s.Capture.NativeBytes != 105 || s.Capture.MappedBytes != 51 || s.Capture.UnmappedBytes != 54 {
		t.Fatal(s.Capture)
	}
	// Back edge to the mapped instruction start is a stable source anchor.
	in := s.Regions[6].Instructions[0]
	if in.Relocation != "f0.pc14.0.i0" {
		t.Fatalf("bad branch anchor %+v", in)
	}
	unknown := fixture("amd64", "c3")
	unknown.Provenance = s.Provenance
	r, err = Compare(unknown, unknown)
	if err != nil || r.Complete || r.Unknown != 1 {
		t.Fatal(r, err)
	}
}
func TestCaptureBoundaryAndTargetControls(t *testing.T) {
	code := []byte{0x31, 0xc0, 0x90}
	got, err := parseListing("0: 31 c0 xor eax,eax\n2: 90 nop\n", code)
	if err != nil || !reflect.DeepEqual(got, []Instruction{{0, "31c0", ""}, {2, "90", ""}}) {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"0: 31 c0 xor eax,eax", "0: 31 c1 xor eax,ecx\n2: 90 nop", "1: 31 c0 xor eax,eax\n2: 90 nop"} {
		if _, err := parseListing(bad, code); err == nil {
			t.Fatal("bad boundaries accepted")
		}
	}
	s := fixture("amd64", "eb00")
	s.Regions[0].Instructions = append(s.Regions[0].Instructions, Instruction{2, "31c0", ""})
	assignTargets(&s)
	if s.Regions[0].Instructions[0].Relocation != "f0.pc5.i1" {
		t.Fatal(s)
	}
	s.Regions[0].Instructions[0].Hex = "ebfd"
	assignTargets(&s)
	if s.Regions[0].Instructions[0].Relocation != "" {
		t.Fatal("stale target retained")
	}
	s.Regions[0].Instructions[0].Hex = "eb01"
	assignTargets(&s)
	if s.Regions[0].Instructions[0].Relocation != "" {
		t.Fatal("mid-instruction target normalized")
	}
	s.Regions[0].Instructions[0].Hex = "ebfd"
	assignTargets(&s)
	if s.Regions[0].Instructions[0].Relocation != "" {
		t.Fatal("negative target normalized")
	}
	if _, err := captureBytes(make([]byte, maxCaptureWasm+1)); err == nil {
		t.Fatal("input limit not applied")
	}
	var out boundedListing
	if _, err := out.Write([]byte(strings.Repeat("x", maxInputBytes+1))); err == nil || out.Len() != 0 {
		t.Fatal("output budget not applied")
	}
}

func TestCaptureSourceRangeRejection(t *testing.T) {
	instructions := []Instruction{{0, "31c0", ""}, {2, "31c0", ""}, {4, "90", ""}}
	for _, ranges := range [][]jitprofile.SourceRange{
		{{Offset: 1, Size: 1}}, {{Offset: 0, Size: 1}},
		{{Offset: 2, Size: 2}, {Offset: 0, Size: 2}},
		{{Offset: 0, Size: 4}, {Offset: 2, Size: 2}}, {{Offset: 4, Size: 2}},
	} {
		s := Snapshot{Capture: &CaptureMetadata{NativeBytes: 5}}
		if err := populateRegions(&s, ranges, instructions); err == nil {
			t.Fatal("source range incorrectly qualified", ranges)
		}
	}
	s := Snapshot{Capture: &CaptureMetadata{NativeBytes: 5}}
	if err := populateRegions(&s, []jitprofile.SourceRange{{Offset: 0, Size: 4}}, instructions); err != nil || s.Capture.MappedBytes != 4 || s.Capture.UnmappedBytes != 1 {
		t.Fatal(s, err)
	}
}

func BenchmarkCaptureExistingFib(b *testing.B) {
	data, err := os.ReadFile("../../fixtures/wasm/fib.wasm")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := captureBytes(data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(s.Capture.NativeBytes), "native-bytes")
	}
}
func BenchmarkCompareCapturedFib(b *testing.B) {
	data, err := os.ReadFile("../../fixtures/wasm/fib.wasm")
	if err != nil {
		b.Fatal(err)
	}
	s, err := captureBytes(data)
	if err != nil {
		b.Fatal(err)
	}
	s.Provenance.CompilerRevision = "test-build"
	encoded, err := json.Marshal(s)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(encoded)), "snapshot-bytes")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := Compare(s, s)
		if err != nil || !r.Complete || r.Known != 12 {
			b.Fatal(r, err)
		}
	}
}
