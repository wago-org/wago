package profile

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func TestSavedInputLimits(t *testing.T) {
	l := DefaultLimits()
	l.Samples = 1
	if v, err := ReadSamplesJSON(strings.NewReader(`[{"pc":1},{"pc":2}]`), l); err == nil || v != nil {
		t.Fatal("accepted excessive samples")
	}
	l = DefaultLimits()
	l.DecodedBytes = 4
	if v, err := ReadSamplesJSON(strings.NewReader(`[{},{}]`), l); err == nil || v != nil {
		t.Fatal("accepted excessive JSON bytes")
	}
	l = DefaultLimits()
	l.Events = 1
	if v, err := ReadEventsJSON(strings.NewReader(`[{},{}]`), l); err == nil || v != nil {
		t.Fatal("accepted excessive events")
	}
	l = DefaultLimits()
	l.CodeBytes = 1
	data, _ := json.Marshal([]jitprofile.Event{{Image: &jitprofile.Image{Code: []byte{1, 2}}}})
	if v, err := ReadEventsJSON(bytes.NewReader(data), l); err == nil || v != nil {
		t.Fatal("accepted excessive retained code")
	}
	l = DefaultLimits()
	l.Images = 1
	if err := ValidateEvents([]jitprofile.Event{{Image: &jitprofile.Image{}}, {Image: &jitprofile.Image{}}}, l); err == nil {
		t.Fatal("accepted excessive images")
	}
	if _, err := ReadSamplesJSON(strings.NewReader(`[] {}`), l); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}
func TestSamplyDecompressionAndTableLimits(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write([]byte(`{"ignored":"` + strings.Repeat("a", 4096) + `"}`))
	gz.Close()
	r, err := gzip.NewReader(&b)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	l := DefaultLimits()
	l.DecodedBytes = 128
	if _, err := ReadSamplyWithLimits(r, nil, l); err == nil {
		t.Fatal("accepted decompression expansion")
	}
	l = DefaultLimits()
	l.Metadata = 1
	if _, err := ReadSamplyWithLimits(strings.NewReader(`{"threads":[{"stringArray":["a","b"]}]}`), nil, l); err == nil {
		t.Fatal("accepted excessive tables")
	}
	l = DefaultLimits()
	l.Samples = 1
	if _, err := ReadSamplyWithLimits(strings.NewReader(`{"threads":[{"samples":{"stack":[null,null]}}]}`), nil, l); err == nil {
		t.Fatal("accepted excessive observations")
	}
}
func TestResolutionLimitsBeforeCopyAndAggregation(t *testing.T) {
	l := DefaultLimits()
	l.Samples = 1
	if _, err := ResolveWithLimits(nil, []Sample{{}, {}}, "nanoseconds", l); err == nil {
		t.Fatal("accepted excessive sample copy")
	}
	events := []jitprofile.Event{{Kind: "load", ImageID: 1, Image: &jitprofile.Image{ID: 1, Base: 100, Size: 2, Regions: []jitprofile.Region{{Offset: 0, Size: 2, Function: 0, Kind: "body"}}}}}
	l = DefaultLimits()
	l.HotPCs = 1
	if _, err := ResolveWithLimits(events, []Sample{{PC: 100}, {PC: 101}}, "nanoseconds", l); err == nil || !strings.Contains(err.Error(), "hot-PC") {
		t.Fatalf("unbounded aggregation: %v", err)
	}
}

func TestInputFileBudgetAndMetadataCardinality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversize.json")
	if err := os.WriteFile(path, []byte("[{},{}]"), 0600); err != nil {
		t.Fatal(err)
	}
	if f, _, err := OpenInput(path, 4); err == nil {
		f.Close()
		t.Fatal("oversize file opened")
	}
	l := DefaultLimits()
	l.Metadata = 1
	events := []jitprofile.Event{{Image: &jitprofile.Image{Functions: []jitprofile.Function{{Calls: map[string]int{"direct": 1}, Decisions: map[string]int{"inline": 1}}}}}}
	if err := ValidateEvents(events, l); err == nil {
		t.Fatal("compiler metadata escaped cardinality budget")
	}
	l = DefaultLimits()
	l.Rows = 1
	events = []jitprofile.Event{{Kind: "load", ImageID: 1, Image: &jitprofile.Image{ID: 1, Base: 100, Size: 2, Regions: []jitprofile.Region{{Offset: 0, Size: 1, Function: 0, Kind: "guest-body"}, {Offset: 1, Size: 1, Function: 1, Kind: "guest-body"}}}}}
	if _, err := ResolveWithLimits(events, []Sample{{PC: 100}, {PC: 101}}, "nanoseconds", l); err == nil || !strings.Contains(err.Error(), "row limit") {
		t.Fatalf("row budget not enforced: %v", err)
	}
}
