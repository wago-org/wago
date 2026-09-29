package profile

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/internal/jitprofile"
)

// TimelineRow measures elapsed time. Exclusive time subtracts the union of
// immediate child spans, so nested guest re-entry is not charged to a callback
// twice and overlapping child work cannot produce negative durations. A nil
// duration is unavailable, not zero. None of these values estimates CPU time.
type TimelineRow struct {
	jitprofile.Span
	InclusiveNS *uint64 `json:"inclusive_elapsed_ns,omitempty"`
	ExclusiveNS *uint64 `json:"exclusive_elapsed_ns,omitempty"`
}

type TimelineReport struct {
	Complete    bool          `json:"complete"`
	Clock       string        `json:"clock"`
	Dropped     uint64        `json:"dropped_spans"`
	Diagnostics []string      `json:"diagnostics,omitempty"`
	Rows        []TimelineRow `json:"spans"`
}

// AnalyzeTimeline validates identities and nesting before doing elapsed-time
// arithmetic. With record loss, exclusive durations are withheld: a missing
// child could otherwise be confidently misreported as parent work.
func AnalyzeTimeline(spans []jitprofile.Span, status jitprofile.Status) (TimelineReport, error) {
	r := TimelineReport{Complete: status.DroppedSpans == 0, Clock: status.Clock, Dropped: status.DroppedSpans}
	index := make(map[uint64]int, len(spans))
	children := make(map[uint64][]int)
	for i, s := range spans {
		if s.ID == 0 {
			return r, fmt.Errorf("timeline span %d has no identity", i)
		}
		if _, ok := index[s.ID]; ok {
			return r, fmt.Errorf("duplicate timeline span %d", s.ID)
		}
		if s.End != 0 && s.End < s.Start {
			return r, fmt.Errorf("timeline clock moved backwards in span %d", s.ID)
		}
		index[s.ID] = i
		children[s.ParentID] = append(children[s.ParentID], i)
	}
	// Three colors detect cycles in linear time, even for deeply nested captures.
	colors := make(map[uint64]uint8, len(spans))
	for _, s := range spans {
		path := []uint64{}
		id := s.ID
		for id != 0 && colors[id] == 0 {
			i, ok := index[id]
			if !ok {
				break
			}
			colors[id] = 1
			path = append(path, id)
			id = spans[i].ParentID
		}
		if id != 0 && colors[id] == 1 {
			return r, fmt.Errorf("cyclic timeline ancestry at span %d", id)
		}
		for _, id := range path {
			colors[id] = 2
		}
	}
	for _, s := range spans {
		if s.ParentID != 0 {
			p, ok := index[s.ParentID]
			if !ok {
				r.Complete = false
				r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("span %d has missing parent %d", s.ID, s.ParentID))
			} else {
				parent := spans[p]
				if s.Start < parent.Start || parent.End != 0 && (s.Start > parent.End || s.End > parent.End) {
					return r, fmt.Errorf("span %d falls outside parent %d", s.ID, s.ParentID)
				}
			}
		}
		if s.End == 0 {
			r.Complete = false
			r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("span %d is incomplete", s.ID))
		}
	}
	if r.Dropped != 0 {
		r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("%d boundary spans dropped; exclusive durations unavailable", r.Dropped))
	}
	for _, s := range spans {
		row := TimelineRow{Span: s}
		if s.End != 0 {
			inclusive := s.End - s.Start
			row.InclusiveNS = &inclusive
			if r.Complete {
				indexes := children[s.ID]
				sort.Slice(indexes, func(i, j int) bool { return spans[indexes[i]].Start < spans[indexes[j]].Start })
				var covered, start, end uint64
				for n, i := range indexes {
					child := spans[i]
					if n == 0 {
						start, end = child.Start, child.End
						continue
					}
					if child.Start > end {
						covered += end - start
						start, end = child.Start, child.End
					} else if child.End > end {
						end = child.End
					}
				}
				if len(indexes) > 0 {
					covered += end - start
				}
				exclusive := inclusive - covered
				row.ExclusiveNS = &exclusive
			}
		}
		r.Rows = append(r.Rows, row)
	}
	sort.Slice(r.Rows, func(i, j int) bool {
		if r.Rows[i].Start == r.Rows[j].Start {
			return r.Rows[i].ID < r.Rows[j].ID
		}
		return r.Rows[i].Start < r.Rows[j].Start
	})
	return r, nil
}
