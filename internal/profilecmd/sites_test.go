//go:build wago_profile

package profilecmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/profile"
)

func TestSiteSummaryPreservesSparseAttributionAndTotals(t *testing.T) {
	pc := func(kind string, n uint64) profile.HotPC {
		p := profile.HotPC{Samples: n, Weight: n * 10}
		if kind != "" {
			p.CompilerSite = &jitprofile.CodeSite{Kind: kind}
		}
		return p
	}
	r := profile.Report{Unit: "nanoseconds", Samples: 12, Weight: 120, UnknownSamples: 2, UnknownWeight: 20, Rows: []profile.Row{
		{Name: "first", PCs: []profile.HotPC{pc("gp-local-load", 3), pc("", 1)}},
		{Name: "hidden", PCs: []profile.HotPC{pc("gp-local-load", 2), pc("gp-spill", 1)}},
	}}
	sites, err := summarizeSites(r)
	if err != nil {
		t.Fatal(err)
	}
	var samples, weight uint64
	byCategory := make(map[string]uint64)
	for _, s := range sites {
		samples += s.Samples
		weight += s.Weight
		byCategory[s.Category+"/"+s.Kind] = s.Samples
	}
	if samples != 12 || weight != 120 || byCategory["compiler-site/gp-local-load"] != 5 || byCategory["compiler-site/gp-spill"] != 1 || byCategory["unclassified-guest/"] != 4 || byCategory["unknown-or-non-guest/"] != 2 {
		t.Fatal(sites)
	}
	top, err := makeTop(capture{manifest: profcapture.Manifest{Backend: "perf"}, report: r}, 1)
	if err != nil {
		t.Fatal(err)
	}
	top.SiteSamples = sites
	var text, js bytes.Buffer
	if err := writeTop(&text, top, false); err != nil {
		t.Fatal(err)
	}
	if err := writeTop(&js, top, true); err != nil {
		t.Fatal(err)
	}
	var decoded topReport
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Rows) != 1 || len(decoded.SiteSamples) != len(sites) || !strings.Contains(text.String(), "gp-local-load") || strings.Contains(text.String(), "hidden") {
		t.Fatal(text.String(), js.String())
	}
	// The ordering is stable for equal weights, including unknown categories.
	for i := 0; i < 10; i++ {
		again, err := summarizeSites(r)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(sites)
		after, _ := json.Marshal(again)
		if !bytes.Equal(before, after) {
			t.Fatal(string(before), string(after))
		}
	}
}

func TestSiteSummaryRejectsInconsistentTotals(t *testing.T) {
	for _, r := range []profile.Report{
		{UnknownSamples: 1},
		{UnknownWeight: 1},
		{Rows: []profile.Row{{PCs: []profile.HotPC{{Samples: 1}}}}},
		{Rows: []profile.Row{{PCs: []profile.HotPC{{Weight: 1}}}}},
	} {
		if _, err := summarizeSites(r); err == nil {
			t.Fatal("accepted inconsistent report", r)
		}
	}
}

func TestSitesFlagIsLimitedToTop(t *testing.T) {
	for _, command := range []string{"annotate", "diff"} {
		err := Run([]string{command, "--sites", "missing-capture"}, nil)
		if err == nil || !strings.Contains(err.Error(), "--sites is only supported by top") {
			t.Fatal(command, err)
		}
	}
	if f := FlagSet("top"); f == nil || f.Lookup("sites") == nil {
		t.Fatal("top help does not expose --sites")
	}
}
