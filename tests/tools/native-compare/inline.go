//go:build wago_nativecompare

package main

import "fmt"

const maxInlineFrames = 128

// InlineFrame preserves a caller location from the existing profile contract.
// Parent is one-based; parents precede children. Zero ends the ancestry.
type InlineFrame struct {
	Function   uint32 `json:"function"`
	WasmOffset uint32 `json:"wasm_offset"`
	Parent     uint32 `json:"parent,omitempty"`
}

// Resolve roots once in table order, without walking each region's ancestry.
// The fixed table stays on the stack; no cache or per-region slices are retained.
func inlineRoots(frames []InlineFrame, roots *[maxInlineFrames]uint32) error {
	if len(frames) > maxInlineFrames {
		return fmt.Errorf("inline frame budget")
	}
	for i, frame := range frames {
		if uint64(frame.Parent) > uint64(i) {
			return fmt.Errorf("invalid inline frame parent")
		}
		if roots != nil {
			roots[i] = frame.Function
			if frame.Parent != 0 {
				roots[i] = roots[frame.Parent-1]
			}
		}
	}
	return nil
}

func inlineOwners(s Snapshot, roots *[maxInlineFrames]uint32) error {
	if err := inlineRoots(s.InlineFrames, roots); err != nil {
		return err
	}
	for _, r := range s.Regions {
		if uint64(r.InlineParent) > uint64(len(s.InlineFrames)) {
			return fmt.Errorf("dangling inline source parent")
		}
	}
	return nil
}

// The logical callee stays in Region.Function. Only ownership uses the root.
func sourceOwnerFunction(r Region, roots *[maxInlineFrames]uint32) uint32 {
	if r.InlineParent != 0 {
		return roots[r.InlineParent-1]
	}
	return r.Function
}

func sameInlineFrames(a, b []InlineFrame) bool {
	if len(a) != len(b) {
		return false
	}
	for i, frame := range a {
		if frame != b[i] {
			return false
		}
	}
	return true
}
