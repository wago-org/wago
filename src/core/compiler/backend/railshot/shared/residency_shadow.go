package shared

const (
	ResidencyShadowMaxLocals     = 256
	ResidencyShadowMaxCandidates = 48
	ResidencyShadowMaxSegments   = 768
)

// ResidencyShadowSummary is a pointer-free description of a bounded planning
// run. It is telemetry only: no field is consumed by code generation.
type ResidencyShadowSummary struct {
	Candidates   uint16
	Versions     uint16
	Segments     uint16
	Profitable   uint16
	Reads        uint16
	Defines      uint16
	LoadsAvoided uint16
	SyncDebt     uint16
	MaxLive      uint16
	PressureDebt uint16
	Admissions   uint16
	Evictions    uint16
	Reloads      uint16
	Writebacks   uint16
	FailSoft     uint16
}

func (s ResidencyShadowSummary) Active() bool {
	return s.Candidates|s.Versions|s.Segments|s.Profitable|s.Reads|s.Defines|
		s.LoadsAvoided|s.SyncDebt|s.MaxLive|s.PressureDebt|s.Admissions|
		s.Evictions|s.Reloads|s.Writebacks|s.FailSoft != 0
}

// ResidencyShadowEntry associates a summary with the already-retained local
// score offset, avoiding growth of the fixed per-function hint header.
type ResidencyShadowEntry struct {
	LocalStart uint32
	Summary    ResidencyShadowSummary
}

type shadowCandidate struct {
	local uint16
	score uint32
}

type shadowLocal struct {
	reads    uint16
	fixed    uint16
	pressure uint16
	dirty    bool
	active   bool
	segment  int16
}

type shadowSegment struct {
	local   uint16
	start   uint16
	end     uint16
	benefit int16
	dirty   bool
}

// PlanResidencyShadow forms local-version segments at definitions and hard
// structured boundaries, then estimates avoided loads against synchronization
// and transient-pressure debt. Fixed arrays and hard caps keep the plan
// deterministic and allocation-free.
func PlanResidencyShadow(events []LocalEvent, nLocals, regBudget int, overflow bool) ResidencyShadowSummary {
	return planResidencyShadow(events, nLocals, regBudget, overflow, false)
}

// PlanResidencyTransitionShadow adds the more expensive version-transition
// simulation used by opt-in explain output. Keeping it out of ordinary hint
// construction makes the diagnostic model free in production compilation.
func PlanResidencyTransitionShadow(events []LocalEvent, nLocals, regBudget int, overflow bool) ResidencyShadowSummary {
	return planResidencyShadow(events, nLocals, regBudget, overflow, true)
}

func planResidencyShadow(events []LocalEvent, nLocals, regBudget int, overflow, transitions bool) ResidencyShadowSummary {
	var out ResidencyShadowSummary
	if overflow || len(events) > LocalEventLimit || nLocals < 0 || nLocals > ResidencyShadowMaxLocals {
		out.FailSoft = 1
		return out
	}

	var reads, defines [ResidencyShadowMaxLocals]uint16
	for _, event := range events {
		if event.Local == NoLocal || int(event.Local) >= nLocals {
			continue
		}
		switch event.Kind {
		case LocalEventRead:
			reads[event.Local] = saturatingInc16(reads[event.Local])
		case LocalEventDefine:
			defines[event.Local] = saturatingInc16(defines[event.Local])
		}
	}

	var ranked [ResidencyShadowMaxCandidates]shadowCandidate
	rankedN := 0
	for local := 0; local < nLocals; local++ {
		if reads[local] < 2 {
			continue
		}
		candidate := shadowCandidate{local: uint16(local), score: uint32(reads[local])*2 + uint32(defines[local])}
		at := rankedN
		if at == len(ranked) {
			at--
			if !betterShadowCandidate(candidate, ranked[at]) {
				continue
			}
		} else {
			rankedN++
		}
		for at > 0 && betterShadowCandidate(candidate, ranked[at-1]) {
			if at < len(ranked) {
				ranked[at] = ranked[at-1]
			}
			at--
		}
		ranked[at] = candidate
	}
	out.Candidates = uint16(rankedN)
	if rankedN == 0 {
		return out
	}

	var selected [ResidencyShadowMaxLocals]bool
	for _, candidate := range ranked[:rankedN] {
		selected[candidate.local] = true
	}
	var state [ResidencyShadowMaxLocals]shadowLocal
	var segments [ResidencyShadowMaxSegments]shadowSegment
	live := 0
	closeOne := func(local uint16, sync uint16, end int) bool {
		s := &state[local]
		if !s.active {
			return true
		}
		avoided := uint16(0)
		if s.reads > 1 {
			avoided = s.reads - 1
		}
		if s.dirty && s.reads != 0 {
			avoided = saturatingInc16(avoided)
		}
		debt := saturatingAdd16(sync, s.fixed)
		debt = saturatingAdd16(debt, (s.pressure+1)/2)
		out.LoadsAvoided = saturatingAdd16(out.LoadsAvoided, avoided)
		out.SyncDebt = saturatingAdd16(out.SyncDebt, sync)
		out.PressureDebt = saturatingAdd16(out.PressureDebt, debt-sync)
		if avoided > debt {
			out.Profitable++
		}
		benefit := int(avoided) - int(debt)
		if benefit < -32768 {
			benefit = -32768
		} else if benefit > 32767 {
			benefit = 32767
		}
		segment := &segments[s.segment]
		segment.end = uint16(end)
		segment.benefit = int16(benefit)
		segment.dirty = s.dirty
		*s = shadowLocal{}
		live--
		return true
	}
	closeAll := func(sync uint16, end int) bool {
		for _, candidate := range ranked[:rankedN] {
			if !closeOne(candidate.local, sync, end) {
				return false
			}
		}
		return true
	}
	open := func(local uint16, at int) *shadowLocal {
		s := &state[local]
		if !s.active {
			if out.Segments == ResidencyShadowMaxSegments {
				out.FailSoft = 1
				return nil
			}
			s.active = true
			s.segment = int16(out.Segments)
			segments[out.Segments] = shadowSegment{local: local, start: uint16(at)}
			out.Segments++
			out.Versions = saturatingInc16(out.Versions)
			live++
			if live > int(out.MaxLive) {
				out.MaxLive = uint16(live)
			}
		}
		return s
	}

	for at, event := range events {
		if event.Local != NoLocal && int(event.Local) < nLocals && selected[event.Local] {
			switch event.Kind {
			case LocalEventRead:
				s := open(event.Local, at)
				if s == nil {
					return out
				}
				s.reads = saturatingInc16(s.reads)
				out.Reads = saturatingInc16(out.Reads)
			case LocalEventDefine:
				if !closeOne(event.Local, 0, at) {
					return out
				}
				s := open(event.Local, at)
				if s == nil {
					return out
				}
				s.dirty = true
				out.Defines = saturatingInc16(out.Defines)
			}
		}
		switch event.Kind {
		case LocalEventFixedReg:
			for _, candidate := range ranked[:rankedN] {
				if state[candidate.local].active {
					state[candidate.local].fixed = saturatingInc16(state[candidate.local].fixed)
				}
			}
		case LocalEventPressure:
			for _, candidate := range ranked[:rankedN] {
				if state[candidate.local].active {
					state[candidate.local].pressure = saturatingInc16(state[candidate.local].pressure)
				}
			}
		case LocalEventCall, LocalEventCollection:
			if !closeAll(2, at) {
				return out
			}
		case LocalEventBlock, LocalEventLoop, LocalEventIf, LocalEventElse,
			LocalEventEnd, LocalEventBranch, LocalEventInvalidate:
			if !closeAll(0, at) {
				return out
			}
		}
		if regBudget > 0 && live > regBudget {
			excess := uint16(live - regBudget)
			out.PressureDebt = saturatingAdd16(out.PressureDebt, excess)
		}
	}
	if !closeAll(0, len(events)) {
		return out
	}
	if transitions {
		planShadowTransitions(events, segments[:out.Segments], ranked[:rankedN], regBudget, &out)
	}
	return out
}

// planShadowTransitions simulates a bounded, hysteretic lease policy over the
// already-scored version segments. It is deliberately telemetry-only: the
// compiler does not consume these decisions. The counters reveal whether a
// future active policy would trade pressure misses for excessive reload/store
// churn before that policy is allowed to affect machine code.
func planShadowTransitions(events []LocalEvent, segments []shadowSegment, candidates []shadowCandidate, regBudget int, out *ResidencyShadowSummary) {
	if regBudget <= 0 || len(segments) == 0 {
		return
	}
	if regBudget > len(candidates) {
		regBudget = len(candidates)
	}
	var current [ResidencyShadowMaxLocals]int16
	var resident, dirty [ResidencyShadowMaxLocals]bool
	var remaining [ResidencyShadowMaxSegments]int16
	for i := range current {
		current[i] = -1
	}
	active, next := 0, 0
	for at := 0; at <= len(events); at++ {
		for next < len(segments) && int(segments[next].start) == at {
			seg := &segments[next]
			local := seg.local
			wasResident := resident[local]
			current[local] = int16(next)
			remaining[next] = seg.benefit
			if wasResident && seg.benefit <= 0 {
				resident[local] = false
				dirty[local] = false // a same-event definition supersedes the old value.
				active--
				wasResident = false
			}
			if wasResident {
				dirty[local] = dirty[local] || seg.dirty
			} else if seg.benefit > 0 {
				victim := uint16(NoLocal)
				victimCost := int(^uint(0) >> 1)
				if active >= regBudget {
					for _, candidate := range candidates {
						x := candidate.local
						if !resident[x] || current[x] < 0 {
							continue
						}
						cost := int(remaining[current[x]])
						if dirty[x] {
							cost++
						}
						if cost < victimCost || cost == victimCost && x < victim {
							victim, victimCost = x, cost
						}
					}
				}
				// One unit of hysteresis pays for changing physical ownership.
				if active < regBudget || victim != NoLocal && int(seg.benefit) > victimCost+1 {
					if victim != NoLocal {
						resident[victim] = false
						active--
						out.Evictions = saturatingInc16(out.Evictions)
						if dirty[victim] {
							out.Writebacks = saturatingInc16(out.Writebacks)
						}
					}
					resident[local] = true
					dirty[local] = seg.dirty
					active++
					out.Admissions = saturatingInc16(out.Admissions)
					if at < len(events) && events[at].Kind == LocalEventRead {
						out.Reloads = saturatingInc16(out.Reloads)
					}
				}
			}
			next++
		}

		for _, candidate := range candidates {
			local := candidate.local
			idx := current[local]
			if resident[local] && idx >= 0 && int(segments[idx].end) <= at {
				resident[local] = false
				dirty[local] = false
				active--
			}
		}
		if at == len(events) {
			break
		}
		event := events[at]
		if event.Local == NoLocal || int(event.Local) >= len(current) || current[event.Local] < 0 || !resident[event.Local] {
			continue
		}
		idx := current[event.Local]
		switch event.Kind {
		case LocalEventRead:
			if remaining[idx] > 0 {
				remaining[idx]--
			}
		case LocalEventDefine:
			dirty[event.Local] = true
		}
	}
}

func FindResidencyShadow(entries []ResidencyShadowEntry, key uint32) ResidencyShadowSummary {
	lo, hi := 0, len(entries)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if entries[mid].LocalStart < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(entries) && entries[lo].LocalStart == key {
		return entries[lo].Summary
	}
	return ResidencyShadowSummary{}
}

func betterShadowCandidate(a, b shadowCandidate) bool {
	return a.score > b.score || a.score == b.score && a.local < b.local
}

func saturatingInc16(v uint16) uint16 {
	if v != ^uint16(0) {
		v++
	}
	return v
}

func saturatingAdd16(a, b uint16) uint16 {
	if ^uint16(0)-a < b {
		return ^uint16(0)
	}
	return a + b
}
