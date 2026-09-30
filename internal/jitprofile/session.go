// Package jitprofile records opt-in JIT metadata. It never calls user code,
// performs I/O, or keeps pointers into executable mappings.
package jitprofile

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Options bounds retained diagnostic data, independently of executable memory.
type Options struct {
	MaxBytes        uint64
	MaxEvents       int
	IncludeCode     bool
	SourceMaps      bool
	UnwindMaps      bool
	TraceBoundaries bool
	TraceLifecycle  bool
	MaxSpans        int
}

// Region describes an exact half-open range in a finalized image. Function is
// the full Wasm index, or -1 for a shared/module region. Function regions can
// contain cold code or literals; they are not instruction/source maps.
type Region struct {
	Offset   uint64 `json:"offset"`
	Size     uint64 `json:"size"`
	Kind     string `json:"kind"`
	Function int    `json:"function"`
	Name     string `json:"name,omitempty"`
}

// Function holds static compiler counters, never dynamic execution counts.
type Function struct {
	Index        int            `json:"index"`
	Name         string         `json:"name"`
	NativeBytes  int            `json:"native_bytes"`
	FrameBytes   int            `json:"frame_bytes"`
	Spills       int            `json:"emitted_spills"`
	Reloads      int            `json:"emitted_reloads"`
	BoundsChecks int            `json:"emitted_bounds_checks"`
	Calls        map[string]int `json:"emitted_calls,omitempty"`
	Decisions    map[string]int `json:"compiler_decisions,omitempty"`
	Fallback     string         `json:"finalizer_fallback,omitempty"`
}

// Image is an immutable-by-copy description of one executable mapping generation.
// Size covers finalized code bytes, excluding unused page-rounded capacity.
type Image struct {
	CodeSites    []CodeSite `json:"compiler_sites,omitempty"`
	SiteCoverage string     `json:"compiler_site_coverage,omitempty"`
	// Preexisting means load time is first observation, not original mapping creation.
	Preexisting    bool          `json:"preexisting,omitempty"`
	InlineFrames   []InlineFrame `json:"inline_frames,omitempty"`
	Sources        []SourceRange `json:"sources,omitempty"`
	SourceCoverage string        `json:"source_coverage,omitempty"`
	Unwind         []UnwindRange `json:"unwind,omitempty"`
	UnwindCoverage string        `json:"unwind_coverage,omitempty"`
	ID             uint64        `json:"id"`
	ModuleID       string        `json:"module_id"`
	ArtifactID     string        `json:"artifact_id"`
	Base           uint64        `json:"base"`
	Size           uint64        `json:"size"`
	Target         string        `json:"target"`
	Regions        []Region      `json:"regions"`
	Functions      []Function    `json:"functions,omitempty"`
	Code           []byte        `json:"code,omitempty"`
}

type Event struct {
	ThreadID  uint32 `json:"thread_id"`
	Sequence  uint64 `json:"sequence"`
	Timestamp uint64 `json:"timestamp_ns"`
	Kind      string `json:"kind"`
	ImageID   uint64 `json:"image_id"`
	Image     *Image `json:"image,omitempty"`
}

type Status struct {
	Clock         string `json:"clock"`
	DroppedSpans  uint64 `json:"dropped_spans"`
	Dropped       uint64 `json:"dropped_records"`
	RetainedBytes uint64 `json:"retained_bytes"`
	Closed        bool   `json:"closed"`
}

// Session is a bounded ordered journal. Snapshot and Read share the publication
// lock: Read(snapshotCursor) cannot miss a concurrent publication. Close releases
// diagnostic storage, never code mappings. Runtime publication becomes a no-op.
type Session struct {
	mu           sync.Mutex
	opts         Options
	events       []Event
	spans        []Span
	droppedSpans uint64
	active       map[uint64]*Image
	seq          uint64
	bytes        uint64
	dropped      uint64
	closed       bool
}

var nextImage atomic.Uint64

func New(opts Options) *Session {
	if opts.TraceLifecycle {
		opts.TraceBoundaries = true
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 64 << 20
	}
	if opts.MaxEvents == 0 {
		opts.MaxEvents = 65536
	}
	if opts.MaxEvents < 0 {
		opts.MaxEvents = 1
	}
	if opts.MaxSpans <= 0 {
		opts.MaxSpans = 65536
	}
	return &Session{opts: opts, active: make(map[uint64]*Image)}
}

func (s *Session) IncludeCode() bool { return s != nil && s.opts.IncludeCode }

func ValidateRegions(regions []Region, size uint64) error {
	var end uint64
	for i, r := range regions {
		if r.Size == 0 || r.Offset != end || r.Offset > size || r.Size > size-r.Offset {
			return fmt.Errorf("invalid/non-exhaustive code region %d", i)
		}
		end = r.Offset + r.Size
	}
	if end != size {
		return fmt.Errorf("code regions cover %d of %d bytes", end, size)
	}
	return nil
}

func imageBytes(im *Image) uint64 {
	n := uint64(256 + 48*len(im.Unwind) + len(im.UnwindCoverage) + 32*len(im.Sources) + 12*len(im.InlineFrames) + len(im.SourceCoverage) + len(im.Code) + len(im.ModuleID) + len(im.ArtifactID) + len(im.Target))
	n += uint64(len(im.SiteCoverage))
	for _, site := range im.CodeSites {
		n += uint64(32 + len(site.Kind))
	}
	for _, r := range im.Regions {
		n += uint64(64 + len(r.Name) + len(r.Kind))
	}
	for _, f := range im.Functions {
		n += uint64(128 + len(f.Name) + len(f.Fallback))
		for k := range f.Calls {
			n += uint64(64 + len(k))
		}
		for k := range f.Decisions {
			n += uint64(64 + len(k))
		}
	}
	return n
}

func cloneMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func cloneImage(im *Image) *Image {
	out := *im
	out.Code = append([]byte(nil), im.Code...)
	out.Unwind = append([]UnwindRange(nil), im.Unwind...)
	out.CodeSites = append([]CodeSite(nil), im.CodeSites...)
	out.Sources = append([]SourceRange(nil), im.Sources...)
	out.InlineFrames = append([]InlineFrame(nil), im.InlineFrames...)
	out.Regions = append([]Region(nil), im.Regions...)
	out.Functions = append([]Function(nil), im.Functions...)
	for i := range out.Functions {
		out.Functions[i].Calls = cloneMap(im.Functions[i].Calls)
		out.Functions[i].Decisions = cloneMap(im.Functions[i].Decisions)
	}
	return &out
}

// Register copies all retained bytes before execution can start. A zero return
// means capture loss; Status and Read report it. Execution is unaffected.
func (s *Session) Register(im Image, code []byte) uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	im.Code = nil
	if !s.opts.SourceMaps {
		im.CodeSites = nil
		im.SiteCoverage = ""
	}
	if s.opts.IncludeCode {
		if uint64(len(code)) != im.Size {
			s.dropped++
			return 0
		}
		im.Code = code
	}
	if !s.opts.UnwindMaps {
		im.Unwind = nil
		im.UnwindCoverage = ""
	}
	n := imageBytes(&im)
	if len(s.events) >= s.opts.MaxEvents || n > s.opts.MaxBytes-s.bytes || ValidateRegions(im.Regions, im.Size) != nil || ValidateSources(im.Sources, im.Size) != nil || ValidateInlineSources(im.Sources, im.InlineFrames) != nil || ValidateUnwind(im.Unwind, im.Size) != nil || ValidateCodeSites(im.CodeSites, im.Size) != nil || ValidateCodeSiteRegions(im.CodeSites, im.Regions) != nil {
		s.dropped++
		return 0
	}
	im.ID = nextImage.Add(1)
	stored := cloneImage(&im)
	s.seq++
	s.bytes += n
	s.events = append(s.events, Event{Sequence: s.seq, Timestamp: Now(), ThreadID: threadID(), Kind: "load", ImageID: im.ID, Image: stored})
	s.active[im.ID] = stored
	return im.ID
}

func (s *Session) Retire(id uint64) {
	if s == nil || id == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if _, ok := s.active[id]; !ok {
		return
	}
	delete(s.active, id)
	if len(s.events) >= s.opts.MaxEvents || 64 > s.opts.MaxBytes-s.bytes {
		s.dropped++
		return
	}
	s.seq++
	s.bytes += 64
	s.events = append(s.events, Event{Sequence: s.seq, Timestamp: Now(), ThreadID: threadID(), Kind: "retire", ImageID: id})
}

func (s *Session) statusLocked() Status {
	return Status{Clock: Clock, Dropped: s.dropped, DroppedSpans: s.droppedSpans, RetainedBytes: s.bytes, Closed: s.closed}
}
func (s *Session) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.statusLocked() }
func (s *Session) Snapshot() ([]Image, uint64, Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Image, 0, len(s.active))
	for _, im := range s.active {
		out = append(out, *cloneImage(im))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, s.seq, s.statusLocked()
}
func (s *Session) Read(after uint64) ([]Event, Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := sort.Search(len(s.events), func(i int) bool { return s.events[i].Sequence > after })
	out := append([]Event(nil), s.events[start:]...)
	for i := range out {
		if out[i].Image != nil {
			out[i].Image = cloneImage(out[i].Image)
		}
	}
	return out, s.statusLocked()
}
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.events = nil
	s.spans = nil
	s.active = nil
	s.bytes = 0
}
