package jitprofile

import (
	"sync"
	"testing"
)

func testImage() Image {
	return Image{ModuleID: "module", ArtifactID: "artifact", Base: 4096, Size: 2, Regions: []Region{{Offset: 0, Size: 2, Function: 3, Kind: "guest-body"}}}
}
func TestJournalLifetimeAndCopies(t *testing.T) {
	s := New(Options{IncludeCode: true})
	code := []byte{1, 2}
	id := s.Register(testImage(), code)
	code[0] = 9
	current, cursor, status := s.Snapshot()
	if len(current) != 1 || status.Dropped != 0 {
		t.Fatal(current, status)
	}
	current[0].Code[0] = 8
	s.Retire(id)
	later, _ := s.Read(cursor)
	if len(later) != 1 || later[0].Kind != "retire" {
		t.Fatal(later)
	}
	next := s.Register(testImage(), []byte{3, 4})
	if next == id {
		t.Fatal("reused generation")
	}
	events, _ := s.Read(0)
	if events[0].Image.Code[0] != 1 {
		t.Fatal("mutable code alias")
	}
}
func TestSnapshotPublicationOrdering(t *testing.T) {
	s := New(Options{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			s.Register(testImage(), nil)
		}
	}()
	images, cursor, _ := s.Snapshot()
	wg.Wait()
	events, status := s.Read(cursor)
	if len(images)+len(events) != 1000 || status.Dropped != 0 {
		t.Fatal(len(images), len(events), status)
	}
}
func TestBoundedJournalReportsLoss(t *testing.T) {
	s := New(Options{MaxEvents: 1})
	id := s.Register(testImage(), nil)
	s.Retire(id)
	_, _, status := s.Snapshot()
	if status.Dropped != 1 {
		t.Fatal(status)
	}
	s.Close()
	if s.Register(testImage(), nil) != 0 {
		t.Fatal("published after close")
	}
}
