package profile

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

// Inspect only the wire invariants needed to distinguish inline debug lines
// from fabricated sampled stack entries. The standard viewer independently
// parses and validates the entire profile below.
func wireFields(t *testing.T, b []byte) map[uint64][][]byte {
	t.Helper()
	fields := map[uint64][][]byte{}
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			t.Fatal("invalid tag")
		}
		b = b[n:]
		switch tag & 7 {
		case 0:
			_, n = binary.Uvarint(b)
			if n <= 0 {
				t.Fatal("invalid varint")
			}
			fields[tag>>3] = append(fields[tag>>3], b[:n])
			b = b[n:]
		case 2:
			size, n := binary.Uvarint(b)
			if n <= 0 || size > uint64(len(b)-n) {
				t.Fatal("invalid message")
			}
			b = b[n:]
			fields[tag>>3] = append(fields[tag>>3], b[:size])
			b = b[size:]
		default:
			t.Fatalf("unexpected wire type %d", tag&7)
		}
	}
	return fields
}

func TestPprofInlineLocationsInStandardViewer(t *testing.T) {
	report := Report{Unit: "nanoseconds", Samples: 6, Weight: 60, UnknownSamples: 1, UnknownWeight: 10, Rows: []Row{
		{ModuleID: "module", ArtifactID: "build-a", Function: 7, Kind: "function", Name: "outer", Samples: 2, Weight: 20, PCs: []HotPC{{Offset: 4, Samples: 2, Weight: 20, Source: &jitprofile.SourceRange{Offset: 4, Size: 4, Function: 3, WasmOffset: 9, InlineParent: 2}, SourceName: "leaf", InlineCallers: []InlineCaller{{Function: 5, WasmOffset: 6, Name: "middle"}, {Function: 7, WasmOffset: 12, Name: "outer"}}}}},
		{ModuleID: "module", ArtifactID: "build-b", Function: 3, Kind: "function", Name: "leaf", Samples: 3, Weight: 30, PCs: []HotPC{{Offset: 4, Samples: 3, Weight: 30}}},
	}}
	var buf bytes.Buffer
	if err := WritePprof(&buf, report, 100); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	fields := wireFields(t, payload)
	if len(fields[2]) != 3 || len(fields[4]) != 3 {
		t.Fatal("sample or location counts changed")
	}
	for _, sample := range fields[2] {
		if len(wireFields(t, sample)[1]) != 1 {
			t.Fatal("invented sampled caller locations")
		}
	}
	location := wireFields(t, fields[4][0])
	if len(location[4]) != 3 {
		t.Fatal("static inline ancestry missing")
	}
	for _, line := range location[4] {
		if len(wireFields(t, line)[2]) != 0 {
			t.Fatal("Wasm offsets misrepresented as source lines")
		}
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go pprof unavailable; wire invariants verified")
	}
	path := filepath.Join(t.TempDir(), "inline.pprof")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(goTool, "tool", "pprof", "-raw", "-symbolize=none", path).CombinedOutput()
	if err != nil {
		t.Fatalf("standard viewer rejected profile: %v\n%s", err, out)
	}
	for _, want := range []string{"module/build-a:f3:leaf", "module/build-a:f5:middle", "module/build-a:f7:outer", "module/build-b:f3:leaf", "wasm_offset", "inline_call_sites", "f5+0x6 <- f7+0xc"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("viewer lost %q:\n%s", want, out)
		}
	}
	top, err := exec.Command(goTool, "tool", "pprof", "-top", "-sample_index=samples", "-symbolize=none", path).CombinedOutput()
	if err != nil {
		t.Fatalf("standard top failed: %v\n%s", err, top)
	}
	if !strings.Contains(string(top), "module/build-a:f3:leaf") || !strings.Contains(string(top), "module/build-b:f3:leaf") {
		t.Fatalf("viewer merged artifact identities:\n%s", top)
	}
}

func TestPprofRejectsIncompatibleAndUnrepresentableValues(t *testing.T) {
	for _, r := range []Report{
		{Unit: "cycles"},
		{Unit: "nanoseconds", Samples: 1},
		{Unit: "nanoseconds", Rows: []Row{{Samples: 1}}},
		{Unit: "nanoseconds", UnknownSamples: 1, UnknownWeight: math.MaxUint64},
		{Unit: "nanoseconds", UnknownWeight: 1},
		{Unit: "nanoseconds", UnknownSamples: 1, UnknownWeight: math.MaxInt64, Rows: []Row{{PCs: []HotPC{{Samples: 1, Weight: 1}}}}},
		{Unit: "nanoseconds", Rows: []Row{{PCs: []HotPC{{Samples: math.MaxUint64}}}}},
		{Unit: "nanoseconds", Rows: []Row{{PCs: []HotPC{{InlineCallers: []InlineCaller{{Function: 1}}}}}}},
	} {
		var b bytes.Buffer
		if WritePprof(&b, r, 0) == nil || b.Len() != 0 {
			t.Fatal("invalid profile emitted", r)
		}
	}
	if WritePprof(io.Discard, Report{Unit: "nanoseconds"}, -1) == nil {
		t.Fatal("negative duration accepted")
	}
}

func TestResolveRejectsWeightOverflow(t *testing.T) {
	_, err := Resolve(nil, []Sample{{Period: math.MaxUint64}, {Period: 1}}, "nanoseconds")
	if err == nil {
		t.Fatal("overflowed sample weights accepted")
	}
}
