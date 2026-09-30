package profile

import (
	"github.com/wago-org/wago/internal/jitprofile"
	"testing"
)

func TestTimelineReentryAndOverlappingChildren(t *testing.T) {
	spans := []jitprofile.Span{
		{ID: 1, Kind: "guest", Start: 100, End: 200},
		{ID: 2, ParentID: 1, Kind: "host", Start: 110, End: 190},
		{ID: 3, ParentID: 2, Kind: "guest", Start: 120, End: 150},
		{ID: 4, ParentID: 2, Kind: "guest", Start: 140, End: 170},
		{ID: 5, ParentID: 3, Kind: "host", Start: 125, End: 135},
	}
	r, err := AnalyzeTimeline(spans, jitprofile.Status{Clock: "monotonic"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint64]uint64{1: 20, 2: 30, 3: 20, 4: 30, 5: 10}
	if !r.Complete {
		t.Fatal(r)
	}
	for _, row := range r.Rows {
		if row.ExclusiveNS == nil || *row.ExclusiveNS != want[row.ID] {
			t.Fatal(row)
		}
	}
}

func TestTimelineIncompleteNeverInventsExclusiveCost(t *testing.T) {
	for _, spans := range [][]jitprofile.Span{
		{{ID: 1, Start: 1, End: 10}, {ID: 2, ParentID: 1, Start: 2}},
		{{ID: 1, ParentID: 99, Start: 1, End: 10}},
	} {
		r, err := AnalyzeTimeline(spans, jitprofile.Status{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Complete {
			t.Fatal("partial capture marked complete")
		}
		for _, row := range r.Rows {
			if row.ExclusiveNS != nil {
				t.Fatal("invented exclusive duration")
			}
		}
	}
	r, err := AnalyzeTimeline([]jitprofile.Span{{ID: 1, Start: 1, End: 10}}, jitprofile.Status{DroppedSpans: 1})
	if err != nil || r.Complete || r.Rows[0].ExclusiveNS != nil {
		t.Fatal(r, err)
	}
}

func TestTimelineRejectsInvalidAncestry(t *testing.T) {
	for _, spans := range [][]jitprofile.Span{
		{{ID: 1, ParentID: 2}, {ID: 2, ParentID: 1}},
		{{ID: 1}, {ID: 1}},
		{{ID: 1, Start: 10, End: 9}},
		{{ID: 1, Start: 10, End: 20}, {ID: 2, ParentID: 1, Start: 19, End: 21}},
	} {
		if _, err := AnalyzeTimeline(spans, jitprofile.Status{}); err == nil {
			t.Fatal("accepted invalid spans", spans)
		}
	}
}
