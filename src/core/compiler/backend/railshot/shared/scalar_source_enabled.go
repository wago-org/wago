//go:build wago_profile

package shared

import "fmt"

const scalarSourcesEnabled = true

type scalarSourceState struct{ sources *ScalarSourceRecorder }

// CompileScalarWithSources records sparse opcode/deferred-definition origins
// without changing lowering or the 19-method ScalarTarget contract. The caller
// owns recorder backing, separately from worker scratch. Ordinary CompileScalar
// never creates or retains this optional sidecar, including in profile builds.
// Native offsets retain target.Position coordinates, including any existing
// prefix; only instructions emitted by this call receive source ranges.
func (s *ScalarState) CompileScalarWithSources(code []byte, summary ScalarSummary, localWide []bool, nParams int, target ScalarTarget, recorder *ScalarSourceRecorder, function, localDeclBytes uint32) (slots int, err error) {
	if recorder == nil {
		return s.CompileScalar(code, summary, localWide, nParams, target)
	}
	if s.sources != nil {
		return 0, fmt.Errorf("shared scalar: source recorder already active")
	}
	ranges := recorder.emission.ranges[:0]
	*recorder = ScalarSourceRecorder{nodes: recorder.nodes[:0], emission: SourceEmission{cursor: target.Position(), ranges: ranges}, function: function, localDecls: localDeclBytes}
	if recorder.emission.cursor < 0 {
		return 0, fmt.Errorf("shared scalar: invalid source emission position")
	}
	if uint64(localDeclBytes)+uint64(len(code)) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("shared scalar: source offset overflow")
	}
	s.sources = recorder
	defer func() {
		recorder.switchOrigin(target.Position(), 0)
		if err == nil {
			err = recorder.err
		}
		if err != nil {
			recorder.emission.ranges = recorder.emission.ranges[:0]
		}
		s.sources = nil
		s.target, s.regs = nil, nil
	}()
	return s.compileScalar(code, summary, localWide, nParams, target)
}

func (s *ScalarState) sourceAdd(id scalarID, kind ScalarLocation) {
	r := s.sources
	if r == nil || r.err != nil {
		return
	}
	if id > scalarMaxNodes {
		r.err = fmt.Errorf("shared scalar: source node bound")
		return
	}
	if int(id) >= len(r.nodes) {
		if int(id) >= cap(r.nodes) {
			capacity := min(scalarMaxNodes+1, max(int(id)+1, max(64, 2*cap(r.nodes))))
			nodes := make([]uint32, len(r.nodes), capacity)
			copy(nodes, r.nodes)
			r.nodes = nodes
		}
		r.nodes = r.nodes[:int(id)+1]
	}
	r.nodes[id] = 0
	if kind == ScalarConstant || kind == scalarDeferred {
		r.nodes[id] = r.instruction
	}
}

func (s *ScalarState) sourceInstruction(pc int) {
	if r := s.sources; r != nil {
		r.instruction = uint32(pc) + 1
		r.switchOrigin(s.target.Position(), r.instruction)
	}
}

func (s *ScalarState) sourceEnterNode(id scalarID) uint32 {
	r := s.sources
	if r == nil {
		return 0
	}
	previous := r.active
	origin := uint32(0)
	if int(id) < len(r.nodes) {
		origin = r.nodes[id]
	}
	r.switchOrigin(s.target.Position(), origin)
	return previous
}

func (s *ScalarState) sourceRestore(origin uint32) {
	if r := s.sources; r != nil {
		r.switchOrigin(s.target.Position(), origin)
	}
}

func (r *ScalarSourceRecorder) switchOrigin(at int, origin uint32) {
	if r.err != nil {
		return
	}
	if at < r.emission.cursor {
		r.err = fmt.Errorf("shared scalar: source emission moved backwards")
		return
	}
	if at > r.emission.cursor && r.emission.origin.Valid {
		previous := r.emission.origin
		ranges := r.emission.ranges
		coalesces := len(ranges) > 0 && ranges[len(ranges)-1].Offset+ranges[len(ranges)-1].Size == uint64(r.emission.cursor) && ranges[len(ranges)-1].Function == previous.Function && ranges[len(ranges)-1].WasmOffset == previous.PC && ranges[len(ranges)-1].InlineParent == previous.InlineParent
		if !coalesces && len(ranges) == MaxScalarSourceRanges {
			r.err = fmt.Errorf("shared scalar: source range bound")
			return
		}
		if !coalesces && len(ranges) == cap(ranges) {
			capacity := min(MaxScalarSourceRanges, max(16, 2*cap(ranges)))
			grown := make([]NativeSourceRange, len(ranges), capacity)
			copy(grown, ranges)
			r.emission.ranges = grown
		}
	}
	next := EmissionOrigin{}
	if origin != 0 {
		next = EmissionOrigin{Function: r.function, PC: r.localDecls + origin - 1, Valid: true}
	}
	r.emission.Switch(at, next)
	r.active = origin
}
