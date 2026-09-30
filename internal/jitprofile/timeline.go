package jitprofile

import "sync/atomic"

// Span describes elapsed boundary time, never sampled CPU time. ParentID names
// the immediately enclosing activation or callback. InstanceID is a logical
// runtime identity; it must not be a native address or an executable image ID.
type Span struct {
	ID           uint64 `json:"id"`
	ParentID     uint64 `json:"parent_id,omitempty"`
	InvocationID uint64 `json:"invocation_id"`
	InstanceID   uint64 `json:"instance_id"`
	ModuleID     string `json:"module_id,omitempty"`
	Kind         string `json:"kind"`
	Function     int    `json:"function"`
	Start        uint64 `json:"start_ns"`
	End          uint64 `json:"end_ns,omitempty"`
	Outcome      string `json:"outcome"`
}

// SpanToken owns a reserved journal slot, not execution or mapping ownership.
// Copies may be finished more than once; only the first completion is recorded.
type SpanToken struct {
	session *Session
	index   int
	id      uint64
}

var nextSpan atomic.Uint64

func (s *Session) TraceBoundaries() bool { return s != nil && s.opts.TraceBoundaries }

// BeginSpan reserves storage before work begins, ensuring that capacity loss
// cannot turn an accepted start into a silently missing completion. Supplied
// timestamps and IDs are ignored. Strings are diagnostic identities only;
// callers must not include argument values or guest memory.
func (s *Session) BeginSpan(span Span) SpanToken {
	if !s.TraceBoundaries() {
		return SpanToken{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return SpanToken{}
	}
	size := uint64(192 + len(span.ModuleID) + len(span.Kind))
	if len(s.spans) >= s.opts.MaxSpans || size > s.opts.MaxBytes || s.bytes > s.opts.MaxBytes-size {
		s.droppedSpans++
		return SpanToken{}
	}
	span.ID = nextSpan.Add(1)
	span.Start = Now()
	span.End = 0
	span.Outcome = "incomplete"
	s.bytes += size
	index := len(s.spans)
	s.spans = append(s.spans, span)
	return SpanToken{session: s, index: index, id: span.ID}
}

func (t SpanToken) ID() uint64 { return t.id }

// Finish records a fixed outcome vocabulary. Unknown outcome strings become
// "error" so an error message cannot accidentally retain guest data or exceed
// the memory reserved at BeginSpan. End timestamps share the code-image clock.
func (t SpanToken) Finish(outcome string) {
	if t.session == nil {
		return
	}
	switch outcome {
	case "return", "trap", "host-exit", "cancelled", "panic", "error":
	default:
		outcome = "error"
	}
	s := t.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || t.index >= len(s.spans) {
		return
	}
	span := &s.spans[t.index]
	if span.ID != t.id || span.End != 0 {
		return
	}
	span.End = Now()
	span.Outcome = outcome
}

// Spans returns a defensive snapshot, including still-open activations. Callers
// must inspect Status.DroppedSpans before presenting the timeline as complete.
func (s *Session) Spans() []Span {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Span(nil), s.spans...)
}

// TraceLifecycle reports whether instance lifecycle operations are observed.
func (s *Session) TraceLifecycle() bool { return s != nil && s.opts.TraceLifecycle }
