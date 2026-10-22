package shared

import "unsafe"

// MaxScalarSourceRanges bounds one optional function source directory. A
// request exceeding it fails; a partial directory is never reported as complete.
const MaxScalarSourceRanges = 65536

// ScalarSourceRecorder owns optional provenance scratch independently of the
// ordinary scalar worker. It requires wago_profile, is not concurrent, and must
// not be released or reused during compilation. Node origins use stable arena
// IDs; zero means unknown, and other values encode raw-body opcode offset + 1.
// Definition origins survive deferred realization and aliases. Synthetic
// parameter/zero/join values remain unknown rather than borrowing a nearby PC.
type ScalarSourceRecorder struct {
	nodes       []uint32
	emission    SourceEmission
	instruction uint32
	active      uint32
	function    uint32
	localDecls  uint32
	err         error
}

// Ranges returns borrowed ranges in the target.Position coordinate system until
// the recorder's next compile or Release. An existing target prefix is not
// subtracted: its bytes remain unmapped. Wasm offsets include local declarations
// and Function is the supplied full module index. Callers must relocate from
// these target coordinates through final framing, and copy before publishing.
// Failed compilation leaves no ranges.
func (r *ScalarSourceRecorder) Ranges() []NativeSourceRange {
	if r == nil {
		return nil
	}
	return r.emission.Ranges()
}

// Memory reports retained sidecar/directory backing bytes, separate from
// ScalarState.Memory. No recorder or sidecar is allocated by CompileScalar.
func (r *ScalarSourceRecorder) Memory() uint64 {
	if r == nil {
		return 0
	}
	return uint64(cap(r.nodes))*4 + uint64(cap(r.emission.ranges))*uint64(unsafe.Sizeof(NativeSourceRange{}))
}

// Release drops all optional source backing after its final consumer finishes.
func (r *ScalarSourceRecorder) Release() { *r = ScalarSourceRecorder{} }
