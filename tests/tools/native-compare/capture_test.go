//go:build wago_nativecompare && amd64 && wago_profile

package main

import (
	"encoding/json"
	"github.com/wago-org/wago/internal/jitprofile"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCaptureInlineCallerLocations(t *testing.T) {
	// Same bounded shape as the backend's inline-source contract test. The
	// imported function shifts full Wasm indices; no guest code is executed.
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec([]byte{1, 'm', 1, 'f', 0, 0})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("first", 0, 2), wasmtest.ExportEntry("second", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x73, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 3, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	code, err := compileCapture(m)
	if err != nil {
		t.Fatal(err)
	}
	defer code.close()
	seen := map[uint32]bool{}
	for _, source := range code.sources {
		if source.InlineParent == 0 {
			continue
		}
		if source.Function != 1 && source.Function != 3 || source.WasmOffset != 5 {
			t.Fatal("unexpected logical inline location", source)
		}
		owned := false
		for _, owner := range code.owners {
			if owner.Function == int(source.Function+1) && source.Offset >= owner.Offset && source.Offset+source.Size <= owner.Offset+owner.Size {
				owned = true
			}
		}
		if !owned {
			t.Fatal("inline source escaped physical caller", source)
		}
		seen[source.Function] = true
	}
	if len(seen) != 2 {
		t.Fatal("fixture did not exercise both inlined callees", seen)
	}
	s, err := captureBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	s.Provenance.CompilerRevision = "inline-control"
	if len(s.InlineFrames) != 2 {
		t.Fatal("caller table omitted", s.InlineFrames)
	}
	seen = map[uint32]bool{}
	for _, r := range s.Regions {
		if r.InlineParent == 0 {
			continue
		}
		frame := s.InlineFrames[r.InlineParent-1]
		if frame.Parent != 0 || frame.Function != r.Function+1 || frame.WasmOffset != 3 || r.WasmOffset == nil || *r.WasmOffset != 5 {
			t.Fatal("logical callee/caller location lost", r, frame)
		}
		seen[r.Function] = true
	}
	if len(seen) != 2 {
		t.Fatal("inlined regions omitted", seen)
	}
	r, err := Compare(s, s)
	if err != nil || !r.RawComplete || r.Unknown != 0 && r.Complete {
		t.Fatal(r, err)
	}
	t.Logf("inline capture: %d bytes, mapped=%d raw=%d; supported=%d unknown=%d complete=%v", s.Capture.NativeBytes, s.Capture.MappedBytes, s.Capture.UnmappedBytes, r.Known, r.Unknown, r.Complete)
}

func TestCaptureInlineOwnerRejection(t *testing.T) {
	for _, owner := range []int{2, 1, -1} {
		s := fixture("amd64", "31c0")
		s.Regions[0].Function = 1
		s.Regions[0].InlineParent = 1
		s.InlineFrames = []InlineFrame{{Function: 2, WasmOffset: 3}}
		s.Capture = &CaptureMetadata{NativeBytes: 3, MappedBytes: 2, UnmappedBytes: 1}
		err := populateRaw(&s, []jitprofile.Region{{Offset: 0, Size: 3, Kind: "guest-body", Function: owner}}, []byte{0x31, 0xc0, 0xc3})
		if (err == nil) != (owner == 2) {
			t.Fatal("incorrect physical ownership qualification", owner, err)
		}
	}
}

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
	if err != nil || !r.Complete || r.Known != 12 || r.Unknown != 0 || len(s.Regions) != 8 || !r.RawComplete || r.RawCompared != 3 {
		t.Fatal(r, err)
	}
	if s.Capture.NativeBytes != 105 || s.Capture.MappedBytes != 51 || s.Capture.UnmappedBytes != 54 {
		t.Fatal(s.Capture)
	}
	if len(s.RawRegions) != 3 || len(s.RawRegions[0].Hex)/2 != 24 || len(s.RawRegions[1].Hex)/2 != 22 || len(s.RawRegions[2].Hex)/2 != 8 || s.RawRegions[0].Kind != "entry-adapter" || s.RawRegions[1].Kind != "guest-body" {
		t.Fatal(s.RawRegions)
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

func TestOpaqueOwnershipCanCutDisassemblyData(t *testing.T) {
	s := fixture("amd64", "31c0")
	s.Regions[0].Instructions[0].Offset = 2
	s.Capture = &CaptureMetadata{NativeBytes: 5, MappedBytes: 2, UnmappedBytes: 3, NativeSHA256: strings.Repeat("a", 64)}
	code := []byte{0x31, 0xc0, 0x31, 0xc0, 0xc3}
	owners := []jitprofile.Region{{Offset: 0, Size: 1, Kind: "literal-data", Function: -1}, {Offset: 1, Size: 1, Kind: "padding", Function: -1}, {Offset: 2, Size: 3, Kind: "guest-body", Function: 0}}
	if err := populateRaw(&s, owners, code); err != nil {
		t.Fatal(err)
	}
	if err := validate(s); err != nil {
		t.Fatal(err)
	}
	if s.RawRegions[0].Function != -1 || s.RawRegions[0].Hex != "31" || s.RawRegions[1].Hex != "c0" {
		t.Fatal("data forced into instructions", s.RawRegions)
	}
	for _, bad := range [][]jitprofile.Region{
		{{Offset: 0, Size: 5, Kind: "guest-body", Function: 1}},
		{{Offset: 0, Size: 3, Kind: "guest-body", Function: 0}, {Offset: 3, Size: 2, Kind: "guest-body", Function: 0}},
		{{Offset: 0, Size: 4, Kind: "guest-body", Function: 0}},
	} {
		s.RawRegions = nil
		if err := populateRaw(&s, bad, code); err == nil {
			t.Fatal("owner mismatch/cut/gap accepted", bad)
		}
	}
}
