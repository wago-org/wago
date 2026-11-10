//go:build wago_regalloccheck

package regalloccheck

import "testing"

func journalTestSpan(t *testing.T, j *EmissionJournal, start, end int) {
	t.Helper()
	if !j.BeginEmission(start) {
		t.Fatal(j.Result())
	}
	j.ObserveEffect(Effect{Kind: Copy, Dst: Register(GP, 1), Src: Slot(8), Size: 4})
	j.ObserveGPWrites(2)
	if !j.EndEmission(end) {
		t.Fatal(j.Result())
	}
}

func TestJournalOrderMappingAndValueAccess(t *testing.T) {
	j := NewEmissionJournal(JournalLimits{})
	defer j.Close()
	journalTestSpan(t, j, 0, 4)
	journalTestSpan(t, j, 4, 12)
	if _, ok := j.Event(0); ok {
		t.Fatal("open journal exposed snapshot")
	}
	calls := map[int]int{}
	r := j.Finalize(12, 9, func(old int) (int, bool) {
		calls[old]++
		return map[int]int{0: 0, 4: 3, 12: 9}[old], true
	})
	if r.State != JournalReady || r.Events != 4 || r.Spans != 2 {
		t.Fatal(r)
	}
	for _, boundary := range []int{0, 4, 12} {
		if calls[boundary] != 1 {
			t.Fatalf("boundary %d mapped %d times", boundary, calls[boundary])
		}
	}
	for i, want := range []JournalEvent{
		{Kind: JournalEffect, Effect: Effect{Kind: Copy, Dst: Register(GP, 1), Src: Slot(8), Size: 4}, Start: 0, End: 3},
		{Kind: JournalGPWrites, GPWrites: 2, Start: 0, End: 3},
		{Kind: JournalEffect, Effect: Effect{Kind: Copy, Dst: Register(GP, 1), Src: Slot(8), Size: 4}, Start: 3, End: 9},
		{Kind: JournalGPWrites, GPWrites: 2, Start: 3, End: 9},
	} {
		got, ok := j.Event(i)
		if !ok || got != want {
			t.Fatalf("event %d = %+v/%v, want %+v", i, got, ok, want)
		}
		got.Effect.Size = 99
		again, _ := j.Event(i)
		if again != want {
			t.Fatal("returned value mutated journal")
		}
	}
	for _, i := range []int{-1, 4} {
		if _, ok := j.Event(i); ok {
			t.Fatal("out of bounds event")
		}
	}
	j.Close()
	if j.events != nil || j.spans != nil || j.transactions != nil || j.Result().State != JournalClosed {
		t.Fatal("close retained pools", j.Result())
	}
	if _, ok := j.Event(0); ok {
		t.Fatal("closed snapshot exposed")
	}
}

func TestJournalNestedRollbackAndGap(t *testing.T) {
	j := NewEmissionJournal(JournalLimits{})
	defer j.Close()
	outer := j.Checkpoint(0)
	inner := j.Checkpoint(0)
	if outer == inner || !j.Commit(inner) {
		t.Fatal("nested empty tokens aliased", j.Result())
	}
	journalTestSpan(t, j, 0, 4)
	discard := j.Checkpoint(4)
	if !j.BeginEmission(4) {
		t.Fatal(j.Result())
	}
	j.ObserveGap()
	j.ObserveEffect(Effect{Kind: Call})
	if !j.EndEmission(8) || !j.Rollback(discard, 4) {
		t.Fatal(j.Result())
	}
	if !j.BeginEmission(4) {
		t.Fatal(j.Result())
	}
	j.ObserveGap()
	if !j.AbortEmission(4) {
		t.Fatal(j.Result())
	}
	if !j.Commit(outer) {
		t.Fatal(j.Result())
	}
	r := j.Finalize(4, 4, nil)
	if r.State != JournalReady || r.Events != 2 || r.Spans != 1 || r.EventHistory != 5 || r.SpanHistory != 3 {
		t.Fatal(r)
	}
}

func TestJournalRetainedGapIsIncomplete(t *testing.T) {
	j := NewEmissionJournal(JournalLimits{})
	defer j.Close()
	j.BeginEmission(0)
	j.ObserveGap()
	j.EndEmission(4)
	r := j.Finalize(4, 4, nil)
	if r.State != JournalIncomplete || r.Reason != UnsupportedOperation {
		t.Fatal(r)
	}
	e, ok := j.Event(0)
	if !ok || e.Kind != JournalGap {
		t.Fatal("incomplete marker unavailable")
	}
}

func TestJournalMisuseIsSticky(t *testing.T) {
	for _, tc := range []struct {
		name   string
		misuse func(*EmissionJournal)
	}{
		{"foreign token", func(j *EmissionJournal) {
			k := NewEmissionJournal(JournalLimits{})
			defer k.Close()
			j.Checkpoint(0)
			j.Commit(k.Checkpoint(0))
		}},
		{"non LIFO", func(j *EmissionJournal) { m := j.Checkpoint(0); j.Checkpoint(0); j.Rollback(m, 0) }},
		{"stale token", func(j *EmissionJournal) { m := j.Checkpoint(0); j.Commit(m); j.Checkpoint(0); j.Commit(m) }},
		{"wrong rollback cursor", func(j *EmissionJournal) { m := j.Checkpoint(0); journalTestSpan(t, j, 0, 4); j.Rollback(m, 4) }},
		{"wrong checkpoint cursor", func(j *EmissionJournal) { j.Checkpoint(1) }},
		{"wrong begin cursor", func(j *EmissionJournal) { j.BeginEmission(1) }},
		{"nested span", func(j *EmissionJournal) { j.BeginEmission(0); j.BeginEmission(0) }},
		{"zero span", func(j *EmissionJournal) { j.BeginEmission(0); j.ObserveGPWrites(1); j.EndEmission(0) }},
		{"outside span", func(j *EmissionJournal) { j.ObserveGPWrites(1) }},
		{"empty mask", func(j *EmissionJournal) { j.BeginEmission(0); j.ObserveGPWrites(0) }},
		{"invalid effect", func(j *EmissionJournal) { j.BeginEmission(0); j.ObserveEffect(Effect{Kind: Copy, Size: 99}) }},
		{"bad abort", func(j *EmissionJournal) { j.BeginEmission(0); j.AbortEmission(4) }},
		{"active finalize", func(j *EmissionJournal) { j.BeginEmission(0); j.Finalize(0, 0, nil) }},
		{"transaction finalize", func(j *EmissionJournal) { j.Checkpoint(0); j.Finalize(0, 0, nil) }},
		{"final length", func(j *EmissionJournal) { j.Finalize(4, 4, nil) }},
		{"sealed mutation", func(j *EmissionJournal) { j.Finalize(0, 0, nil); j.BeginEmission(0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := NewEmissionJournal(JournalLimits{})
			defer j.Close()
			tc.misuse(j)
			r := j.Finalize(0, 0, nil)
			if r.State != JournalInvalid || r.Reason != InvalidGraph {
				t.Fatal(r)
			}
			if j.BeginEmission(0) {
				t.Fatal("failure erased")
			}
			if _, ok := j.Event(0); ok {
				t.Fatal("invalid snapshot exposed")
			}
		})
	}
}

func TestJournalMappingFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mapper func(int) (int, bool)
	}{
		{"collapse", func(int) (int, bool) { return 0, true }},
		{"negative", func(int) (int, bool) { return -1, true }},
		{"out of bounds", func(int) (int, bool) { return 9, true }},
		{"nonmonotone", func(old int) (int, bool) { return 8 - old, true }},
		{"deleted boundary", func(old int) (int, bool) { return old, old != 4 }},
		{"wrong terminal", func(old int) (int, bool) { return old / 2, true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := NewEmissionJournal(JournalLimits{})
			defer j.Close()
			journalTestSpan(t, j, 0, 4)
			journalTestSpan(t, j, 4, 8)
			r := j.Finalize(8, 8, tc.mapper)
			if r.State != JournalInvalid {
				t.Fatal(r)
			}
			if _, ok := j.Event(0); ok {
				t.Fatal("partial mapping exposed")
			}
			if j.Finalize(8, 8, nil).State != JournalInvalid {
				t.Fatal("retry erased failure")
			}
		})
	}
	for _, action := range []string{"panic", "begin", "close"} {
		t.Run(action, func(t *testing.T) {
			j := NewEmissionJournal(JournalLimits{})
			defer j.Close()
			journalTestSpan(t, j, 0, 4)
			func() {
				defer func() {
					if action == "panic" && recover() != "mapping panic" {
						t.Fatal("mapper panic changed")
					}
				}()
				j.Finalize(4, 4, func(old int) (int, bool) {
					switch action {
					case "panic":
						panic("mapping panic")
					case "begin":
						j.BeginEmission(0)
					case "close":
						j.Close()
					}
					return old, true
				})
			}()
			if j.Result().State != JournalInvalid {
				t.Fatal(j.Result())
			}
			if _, ok := j.Event(0); ok {
				t.Fatal("partial mapping exposed")
			}
		})
	}
}

func TestJournalHistoryAndWorkLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits JournalLimits
		fill   func(*EmissionJournal)
	}{
		{"events after rollback", JournalLimits{Events: 2}, func(j *EmissionJournal) {
			m := j.Checkpoint(0)
			journalTestSpan(t, j, 0, 4)
			j.Rollback(m, 0)
			j.BeginEmission(0)
			j.ObserveGPWrites(1)
		}},
		{"spans after abort", JournalLimits{Spans: 1}, func(j *EmissionJournal) { j.BeginEmission(0); j.AbortEmission(0); j.BeginEmission(0) }},
		{"depth", JournalLimits{Transactions: 1}, func(j *EmissionJournal) { j.Checkpoint(0); j.Checkpoint(0) }},
		{"work", JournalLimits{Work: 1}, func(j *EmissionJournal) { j.BeginEmission(0); j.ObserveGPWrites(1) }},
		{"finalize work", JournalLimits{Work: 6}, func(j *EmissionJournal) { journalTestSpan(t, j, 0, 4); j.Finalize(4, 4, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := NewEmissionJournal(tc.limits)
			defer j.Close()
			tc.fill(j)
			r := j.Result()
			if r.State != JournalIncomplete || r.Reason != ResourceLimit {
				t.Fatal(r)
			}
			if j.Rollback(JournalMark{}, 0) || j.Finalize(0, 0, nil).Reason != ResourceLimit {
				t.Fatal("resource failure erased")
			}
		})
	}
	j := NewEmissionJournal(JournalLimits{Events: -1})
	if j.Result().State != JournalInvalid {
		t.Fatal("negative limit admitted")
	}
	j.Close()
}
