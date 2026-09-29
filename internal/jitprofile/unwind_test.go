package jitprofile

import "testing"

func TestUnwindJournalCopiesBudgetsAndOptIn(t *testing.T) {
	im := testImage()
	im.Unwind = []UnwindRange{{Size: 2, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8}}
	im.UnwindCoverage = "fixture"
	for _, enabled := range []bool{false, true} {
		s := New(Options{UnwindMaps: enabled})
		id := s.Register(im, nil)
		if id == 0 {
			t.Fatal(s.Status())
		}
		im.Unwind[0].CFAOffset = 16
		images, _, status := s.Snapshot()
		if enabled {
			if len(images[0].Unwind) != 1 || images[0].Unwind[0].CFAOffset != 8 {
				t.Fatal(images)
			}
			images[0].Unwind[0].CFAOffset = 24
		} else if len(images[0].Unwind) != 0 || images[0].UnwindCoverage != "" {
			t.Fatal("retained unrequested rules")
		}
		s.Retire(id)
		events, _ := s.Read(0)
		if enabled && events[0].Image.Unwind[0].CFAOffset != 8 {
			t.Fatal("mutable unwind history")
		}
		im.Unwind[0].CFAOffset = 8
		budget := New(Options{UnwindMaps: enabled, MaxBytes: status.RetainedBytes - 1})
		if budget.Register(im, nil) != 0 || budget.Status().Dropped != 1 {
			t.Fatal("unwind escaped byte budget")
		}
	}
}

func TestUnwindDirectoryValidationAndBoundaries(t *testing.T) {
	rules := []UnwindRange{{Offset: 2, Size: 2, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8}, {Offset: 6, Size: 3, CFARegister: 7, CFAOffset: 40, ReturnOffset: -8}}
	if err := ValidateUnwind(rules, 10); err != nil {
		t.Fatal(err)
	}
	for pc := uint64(0); pc <= 10; pc++ {
		r, ok := LookupUnwind(rules, pc)
		want := pc >= 2 && pc < 4 || pc >= 6 && pc < 9
		if ok != want || ok && (r.CFAOffset != 8 && r.CFAOffset != 40) {
			t.Fatal(pc, r, ok)
		}
	}
	for _, change := range []func(*UnwindRange){
		func(r *UnwindRange) { r.Size = 0 },
		func(r *UnwindRange) { r.Offset = ^uint64(0) },
		func(r *UnwindRange) { r.Size = 9 },
		func(r *UnwindRange) { r.CFARegister = 256 },
		func(r *UnwindRange) { r.CFAOffset = -1 },
		func(r *UnwindRange) { r.ReturnOffset = 0 },
		func(r *UnwindRange) { r.ReturnOffset = -9 },
	} {
		r := rules[0]
		change(&r)
		if ValidateUnwind([]UnwindRange{r}, 10) == nil {
			t.Fatal("accepted malformed unwind", r)
		}
		im := testImage()
		im.Unwind = []UnwindRange{r}
		s := New(Options{UnwindMaps: true})
		if s.Register(im, nil) != 0 || s.Status().Dropped != 1 {
			t.Fatal("journal accepted invalid unwind")
		}
	}
	if ValidateUnwind([]UnwindRange{rules[1], rules[0]}, 10) == nil {
		t.Fatal("accepted unordered rules")
	}
}
