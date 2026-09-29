package jitprofile

import (
	"fmt"
	"sort"
)

// SourceRange is a sparse mapping of final native bytes to compiler-recorded
// Wasm provenance. Function includes imports; WasmOffset starts at the function's
// local declarations. An absent range is unknown, never attributed by proximity.
// InlineFrame is a caller location. Parent is a one-based index into the same
// image table; zero ends the ancestry. Parents precede their children.
type InlineFrame struct {
	Function   uint32 `json:"function"`
	WasmOffset uint32 `json:"wasm_offset"`
	Parent     uint32 `json:"parent,omitempty"`
}

type SourceRange struct {
	InlineParent uint32 `json:"inline_parent,omitempty"`
	Offset       uint64 `json:"offset"`
	Size         uint64 `json:"size"`
	Function     uint32 `json:"function"`
	WasmOffset   uint32 `json:"wasm_offset"`
}

func (s *Session) IncludeSources() bool { return s != nil && s.opts.SourceMaps }

func ValidateSources(ranges []SourceRange, size uint64) error {
	var end uint64
	for i, r := range ranges {
		if r.Size == 0 || r.Offset < end || r.Offset > size || r.Size > size-r.Offset {
			return fmt.Errorf("invalid native source range %d", i)
		}
		end = r.Offset + r.Size
	}
	return nil
}

// LookupSource resolves an image-relative PC without filling source-map gaps.
func LookupSource(ranges []SourceRange, offset uint64) (SourceRange, bool) {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Offset > offset }) - 1
	if i < 0 {
		return SourceRange{}, false
	}
	r := ranges[i]
	return r, offset-r.Offset < r.Size
}

// ValidateInlineSources rejects dangling references and cycles in linear time.
func ValidateInlineSources(ranges []SourceRange, frames []InlineFrame) error {
	for i, frame := range frames {
		if uint64(frame.Parent) > uint64(i) {
			return fmt.Errorf("invalid inline parent at frame %d", i+1)
		}
	}
	for i, r := range ranges {
		if uint64(r.InlineParent) > uint64(len(frames)) {
			return fmt.Errorf("invalid inline parent at source range %d", i)
		}
	}
	return nil
}

// InlineCallers returns caller-to-root ancestry from a validated image table.
func InlineCallers(frames []InlineFrame, parent uint32) []InlineFrame {
	var out []InlineFrame
	for parent != 0 {
		if uint64(parent) > uint64(len(frames)) {
			return nil
		}
		frame := frames[parent-1]
		if frame.Parent >= parent {
			return nil
		}
		out = append(out, frame)
		parent = frame.Parent
	}
	return out
}
