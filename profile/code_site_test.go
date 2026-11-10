package profile

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func TestCompilerSitesJoinSamplesWithoutFillingGaps(t *testing.T) {
	events := fixture()
	events[0].Image.CodeSites = []jitprofile.CodeSite{{Size: 1, Kind: "gp-spill"}}
	samples := []Sample{{Timestamp: 11, PC: 0x1000, Period: 10}, {Timestamp: 11, PC: 0x1001, Period: 20}}
	report, err := Resolve(events, samples, "nanoseconds")
	if err != nil {
		t.Fatal(err)
	}
	pcs := report.Rows[0].PCs
	if len(pcs) != 2 || pcs[0].CompilerSite == nil || pcs[0].CompilerSite.Kind != "gp-spill" || pcs[1].CompilerSite != nil {
		t.Fatal(pcs)
	}
	var buf bytes.Buffer
	if err := WritePprof(&buf, report, 0); err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	z.Close()
	if !bytes.Contains(data, []byte("compiler_site_kind")) || !bytes.Contains(data, []byte("gp-spill")) {
		t.Fatal("pprof lost static site label")
	}
	pcs[0].CompilerSite.Offset = 1
	if WritePprof(io.Discard, report, 0) == nil {
		t.Fatal("accepted a sample outside its compiler site")
	}
	events[0].Image.CodeSites[0].Size = 3
	if _, err := Resolve(events, samples, "nanoseconds"); err == nil {
		t.Fatal("accepted site crossing into literal data")
	}
}
