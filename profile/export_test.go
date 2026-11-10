package profile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func fixture() []jitprofile.Event {
	return []jitprofile.Event{{Sequence: 1, Timestamp: 10, Kind: "load", ImageID: 7, Image: &jitprofile.Image{ID: 7, ModuleID: "mod", ArtifactID: "variant", Base: 0x1000, Size: 4, Code: []byte{1, 2, 3, 4}, Regions: []jitprofile.Region{{Offset: 0, Size: 2, Kind: "guest-body", Function: 3, Name: "duplicate\nname"}, {Offset: 2, Size: 2, Kind: "literal-data", Function: -1}}, Functions: []jitprofile.Function{{Index: 3, Name: "duplicate", Spills: 9}}}}}
}
func TestJITDumpRecordsAndRetirement(t *testing.T) {
	var b bytes.Buffer
	j, err := NewJITDump(&b, 123, "arm64", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Write(fixture()); err != nil {
		t.Fatal(err)
	}
	size := b.Len()
	if err = j.Write([]jitprofile.Event{{Kind: "retire", ImageID: 7}}); err != nil {
		t.Fatal(err)
	}
	if b.Len() != size {
		t.Fatal("retirement emitted fake unload")
	}
	if err = j.Close(99); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	u32 := func(i int) uint32 { return binary.LittleEndian.Uint32(data[i:]) }
	u64 := func(i int) uint64 { return binary.LittleEndian.Uint64(data[i:]) }
	if u32(0) != 0x4a695444 || u32(12) != 183 || u32(20) != 123 {
		t.Fatal("bad header")
	}
	if u32(40) != 0 || u64(48) != 10 || u64(72) != 0x1000 || u64(80) != 2 || u64(88) != 1 {
		t.Fatal("bad load record")
	}
	end := 40 + int(u32(44))
	if !bytes.Equal(data[end-2:end], []byte{1, 2}) || u32(end) != 3 || u64(end+8) != 99 {
		t.Fatal("bad code/close")
	}
	if bytes.Contains(data[96:end-2], []byte{'\n'}) {
		t.Fatal("symbol format injection")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestExportErrorsAndAddressReuse(t *testing.T) {
	if _, err := NewJITDump(brokenWriter{}, 1, "amd64", 1); err == nil {
		t.Fatal("ignored write error")
	}
	if _, err := NewJITDump(shortWriter{}, 1, "amd64", 1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	p := NewPerfMap(io.Discard)
	if err := p.Write(fixture()); err != nil {
		t.Fatal(err)
	}
	if err := p.Write(fixture()); err == nil {
		t.Fatal("accepted address reuse")
	}
	var b bytes.Buffer
	j, _ := NewJITDump(&b, 1, "amd64", 1)
	events := fixture()
	events[0].Image.Code = nil
	if err := j.Write(events); err == nil {
		t.Fatal("accepted missing code")
	}
}

func TestPerfMapEscapesAllSymbolFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*jitprofile.Image)
		want   string
	}{
		{"module", func(im *jitprofile.Image) { im.ModuleID = "mod\npart" }, "wago:mod_part:variant:g7:f3:r0:guest-body:duplicate_name"},
		{"artifact", func(im *jitprofile.Image) { im.ArtifactID = "variant\npart" }, "wago:mod:variant_part:g7:f3:r0:guest-body:duplicate_name"},
		{"region kind", func(im *jitprofile.Image) { im.Regions[0].Kind = "guest\nbody" }, "wago:mod:variant:g7:f3:r0:guest_body:duplicate_name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := fixture()
			tc.mutate(events[0].Image)
			var out bytes.Buffer
			if err := NewPerfMap(&out).Write(events); err != nil {
				t.Fatal(err)
			}
			want := "1000 2 " + tc.want + "\n"
			if got := out.String(); got != want {
				t.Fatalf("perf-map record = %q, want %q", got, want)
			}
		})
	}
}

func TestTemporalResolutionAndRegionBoundaries(t *testing.T) {
	events := fixture()
	events = append(events, jitprofile.Event{Sequence: 2, Timestamp: 20, Kind: "retire", ImageID: 7})
	second := fixture()[0]
	second.Sequence = 3
	second.Timestamp = 30
	second.ImageID = 8
	second.Image.ID = 8
	second.Image.ModuleID = "other"
	events = append(events, second)
	samples := []Sample{{9, 0x1000, 1}, {10, 0x1000, 2}, {11, 0x1001, 3}, {12, 0x1002, 4}, {13, 0x1004, 5}, {20, 0x1000, 6}, {31, 0x1000, 7}}
	r, err := Resolve(events, samples, "nanoseconds")
	if err != nil {
		t.Fatal(err)
	}
	if r.Samples != 7 || r.UnknownSamples != 4 || r.UnknownWeight != 16 || len(r.Rows) != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Rows[0].ModuleID != "other" || r.Rows[0].Weight != 7 || r.Rows[1].Weight != 5 {
		t.Fatal(r.Rows)
	}
	if r.Rows[1].Static.Spills != 9 {
		t.Fatal("lost compiler join")
	}
}
func TestParsePerfScript(t *testing.T) {
	s, err := ParsePerfScript(bytes.NewBufferString(" 12.000000003:  2000000  000000000000abcd\n"), 1)
	if err != nil || len(s) != 1 || s[0].Timestamp != 12000000003 || s[0].PC != 0xabcd {
		t.Fatal(s, err)
	}
	if _, err := ParsePerfScript(bytes.NewBufferString("PERF_RECORD_LOST 100\n"), 1); err == nil {
		t.Fatal("accepted lost records")
	}
}

func TestSamplyLeafOnlyAttribution(t *testing.T) {
	events := fixture()
	symbol := Symbol(*events[0].Image, events[0].Image.Regions[0])
	raw := fmt.Sprintf(`{"threads":[{"stringArray":[%q,"unknown"],"samples":{"stack":[0,1],"weight":[1,1]},"stackTable":{"frame":[0,1],"prefix":[null,0]},"frameTable":{"func":[0,1]},"funcTable":{"name":[0,1]}}]}`, symbol)
	r, err := ReadSamply(strings.NewReader(raw), events)
	if err != nil {
		t.Fatal(err)
	}
	if r.Unit != "observations" || r.Samples != 2 || r.UnknownSamples != 1 || len(r.Rows) != 1 || r.Rows[0].Samples != 1 {
		t.Fatal(r)
	}
}

func TestSharedRegionsHaveDistinctSymbols(t *testing.T) {
	im := fixture()[0].Image
	first := jitprofile.Region{Offset: 0, Size: 1, Kind: "shared-trap", Function: -1}
	second := first
	second.Offset = 2
	if Symbol(*im, first) == Symbol(*im, second) {
		t.Fatal("distinct shared regions collapsed into one symbol")
	}
}
