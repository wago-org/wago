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
	FailSoft     uint16
}

func (s ResidencyShadowSummary) Active() bool {
	return s.Candidates|s.Versions|s.Segments|s.Profitable|s.Reads|s.Defines|
		s.LoadsAvoided|s.SyncDebt|s.MaxLive|s.PressureDebt|s.FailSoft != 0
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
}

// PlanResidencyShadow forms local-version segments at definitions and hard
// structured boundaries, then estimates avoided loads against synchronization
// and transient-pressure debt. Fixed arrays and hard caps keep the plan
// deterministic and allocation-free.
func PlanResidencyShadow(events []LocalEvent, nLocals, regBudget int, overflow bool) ResidencyShadowSummary {
	var out ResidencyShadowSummary
	if overflow || nLocals < 0 || nLocals > ResidencyShadowMaxLocals {
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
	live := 0
	closeOne := func(local uint16, sync uint16) bool {
		s := &state[local]
		if !s.active {
			return true
		}
		if out.Segments == ResidencyShadowMaxSegments {
			out.FailSoft = 1
			return false
		}
		out.Segments++
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
		*s = shadowLocal{}
		live--
		return true
	}
	closeAll := func(sync uint16) bool {
		for _, candidate := range ranked[:rankedN] {
			if !closeOne(candidate.local, sync) {
				return false
			}
		}
		return true
	}
	open := func(local uint16) *shadowLocal {
		s := &state[local]
		if !s.active {
			s.active = true
			out.Versions = saturatingInc16(out.Versions)
			live++
			if live > int(out.MaxLive) {
				out.MaxLive = uint16(live)
			}
		}
		return s
	}

	for _, event := range events {
		if event.Local != NoLocal && int(event.Local) < nLocals && selected[event.Local] {
			switch event.Kind {
			case LocalEventRead:
				s := open(event.Local)
				s.reads = saturatingInc16(s.reads)
				out.Reads = saturatingInc16(out.Reads)
			case LocalEventDefine:
				if !closeOne(event.Local, 0) {
					return out
				}
				s := open(event.Local)
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
			if !closeAll(2) {
				return out
			}
		case LocalEventBlock, LocalEventLoop, LocalEventIf, LocalEventElse,
			LocalEventEnd, LocalEventBranch, LocalEventInvalidate:
			if !closeAll(0) {
				return out
			}
		}
		if regBudget > 0 && live > regBudget {
			excess := uint16(live - regBudget)
			out.PressureDebt = saturatingAdd16(out.PressureDebt, excess)
		}
	}
	closeAll(0)
	return out
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
