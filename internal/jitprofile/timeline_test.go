package jitprofile

import (
	"sync"
	"testing"
)

func TestTimelineReservationAndCompletion(t *testing.T) {
	s := New(Options{TraceBoundaries: true, MaxSpans: 2})
	a := s.BeginSpan(Span{Kind: "guest", InstanceID: 1, InvocationID: 2})
	b := s.BeginSpan(Span{Kind: "host", ParentID: a.ID(), InstanceID: 3, InvocationID: 2})
	if a.ID() == 0 || b.ID() == 0 {
		t.Fatal("missing span identity")
	}
	if s.BeginSpan(Span{}).ID() != 0 {
		t.Fatal("span limit ignored")
	}
	b.Finish("return")
	a.Finish("panic")
	a.Finish("return")
	spans := s.Spans()
	if len(spans) != 2 || spans[0].Outcome != "panic" || spans[1].Outcome != "return" || spans[1].End == 0 {
		t.Fatal(spans)
	}
	if s.Status().DroppedSpans != 1 {
		t.Fatal(s.Status())
	}
	spans[0].Outcome = "changed"
	if s.Spans()[0].Outcome != "panic" {
		t.Fatal("snapshot aliases journal")
	}
	s.Close()
	b.Finish("return")
	if len(s.Spans()) != 0 || s.Status().RetainedBytes != 0 {
		t.Fatal("close retained timeline")
	}
}

func TestTimelineSharedMemoryLimit(t *testing.T) {
	s := New(Options{TraceBoundaries: true, MaxBytes: 193})
	a := s.BeginSpan(Span{Kind: "x"})
	if a.ID() == 0 {
		t.Fatal("reservation rejected")
	}
	if s.BeginSpan(Span{}).ID() != 0 {
		t.Fatal("byte budget ignored")
	}
	a.Finish("this must not retain arbitrary diagnostic strings")
	if s.Spans()[0].Outcome != "error" {
		t.Fatal(s.Spans())
	}
}

func TestTimelineConcurrentFinishAndClose(t *testing.T) {
	s := New(Options{TraceBoundaries: true})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a := s.BeginSpan(Span{Kind: "guest"}); a.Finish("return"); s.Spans() }()
	}
	s.Close()
	wg.Wait()
	if !s.Status().Closed {
		t.Fatal("session open")
	}
}

func TestTimelineDisabled(t *testing.T) {
	s := New(Options{})
	if s.BeginSpan(Span{}).ID() != 0 || len(s.Spans()) != 0 {
		t.Fatal("tracing enabled implicitly")
	}
}
