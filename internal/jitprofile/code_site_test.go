package jitprofile

import "testing"

func TestCompilerSiteJournalCopiesBudgetsAndOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		im := testImage()
		im.CodeSites = []CodeSite{{Size: 1, Kind: "gp-spill"}}
		im.SiteCoverage = "fixture"
		s := New(Options{SourceMaps: enabled})
		id := s.Register(im, nil)
		if id == 0 {
			t.Fatal(s.Status())
		}
		im.CodeSites[0].Kind = "mutated"
		images, _, status := s.Snapshot()
		if enabled {
			if len(images[0].CodeSites) != 1 || images[0].CodeSites[0].Kind != "gp-spill" {
				t.Fatal(images)
			}
			images[0].CodeSites[0].Kind = "mutated again"
		} else if len(images[0].CodeSites) != 0 || images[0].SiteCoverage != "" {
			t.Fatal("unrequested compiler sites retained")
		}
		s.Retire(id)
		events, _ := s.Read(0)
		if enabled && events[0].Image.CodeSites[0].Kind != "gp-spill" {
			t.Fatal("compiler site history is mutable")
		}
		im.CodeSites[0].Kind = "gp-spill"
		limited := New(Options{SourceMaps: enabled, MaxBytes: status.RetainedBytes - 1})
		if limited.Register(im, nil) != 0 || limited.Status().Dropped != 1 {
			t.Fatal("compiler sites escaped retention budget")
		}
	}
}

func TestCompilerSiteBoundariesAndOwnership(t *testing.T) {
	sites := []CodeSite{{Offset: 1, Size: 2, Kind: "gp-spill"}, {Offset: 5, Size: 1, Kind: "gp-reload"}}
	if err := ValidateCodeSites(sites, 8); err != nil {
		t.Fatal(err)
	}
	for pc := uint64(0); pc <= 8; pc++ {
		_, ok := LookupCodeSite(sites, pc)
		if ok != (pc == 1 || pc == 2 || pc == 5) {
			t.Fatal("filled a gap or missed a half-open boundary", pc)
		}
	}
	for _, bad := range []CodeSite{{Size: 0, Kind: "x"}, {Size: 1}, {Offset: ^uint64(0), Size: 2, Kind: "x"}, {Offset: 7, Size: 2, Kind: "x"}} {
		if ValidateCodeSites([]CodeSite{bad}, 8) == nil {
			t.Fatal("accepted invalid site", bad)
		}
	}
	if ValidateCodeSites([]CodeSite{{Size: 2, Kind: "x"}, {Offset: 1, Size: 2, Kind: "y"}}, 8) == nil {
		t.Fatal("accepted overlapping sites")
	}
	regions := []Region{{Size: 4, Kind: "guest-body", Function: 3}, {Offset: 4, Size: 4, Kind: "guest-body", Function: 4}}
	if err := ValidateCodeSiteRegions(sites, regions); err != nil {
		t.Fatal(err)
	}
	if ValidateCodeSiteRegions([]CodeSite{{Offset: 3, Size: 2, Kind: "gp-spill"}}, regions) == nil {
		t.Fatal("site crossed logical owners")
	}
	for _, kind := range []string{"padding", "literal-data", "unknown"} {
		regions[1].Kind = kind
		if ValidateCodeSiteRegions(sites, regions) == nil {
			t.Fatal("site covered", kind)
		}
	}
}
