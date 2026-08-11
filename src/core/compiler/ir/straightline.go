package ir

// StraightLinePlan is the local-SSA identity map needed by a backend that
// schedules one straight-line function as a value DAG.
type StraightLinePlan struct {
	Aliases       []ValueID
	InitialLocals []ValueID
}

// BuildStraightLinePlan renames explicit local.get/set/tee operations into SSA
// identities. It does not reorder effects; the backend separately admits only
// effect shapes it can preserve exactly.
func BuildStraightLinePlan(f *Func) *StraightLinePlan {
	if f == nil || len(f.Blocks) == 0 {
		return nil
	}
	r := localSSARenamer{
		f:       f,
		aliases: make([]ValueID, len(f.Values)),
		initial: make([]ValueID, straightLineLocalCount(f)),
	}
	for i := range r.aliases {
		r.aliases[i] = ValueID(i)
	}
	for i := range r.initial {
		r.initial[i] = InvalidValue
	}
	if !r.rename() {
		return nil
	}
	return &StraightLinePlan{
		Aliases:       r.aliases,
		InitialLocals: r.initial,
	}
}

func (p *StraightLinePlan) Resolve(v ValueID) ValueID {
	for v != InvalidValue && int(v) < len(p.Aliases) && p.Aliases[v] != v {
		v = p.Aliases[v]
	}
	return v
}

type localSSARenamer struct {
	f       *Func
	aliases []ValueID
	initial []ValueID
}

func (r *localSSARenamer) rename() bool {
	current := make([]ValueID, len(r.initial))
	copy(current, r.initial)
	for id := range r.f.Insts {
		in := &r.f.Insts[id]
		switch in.Op {
		case OpLocalGet:
			x := int(uint32(in.Aux))
			if x >= len(current) || in.Results.Len != 1 {
				return false
			}
			out := r.f.ValueIDs[in.Results.Start]
			if current[x] == InvalidValue {
				current[x] = out
				r.initial[x] = out
			} else {
				r.aliases[out] = r.resolve(current[x])
			}
		case OpLocalSet, OpLocalTee:
			x := int(uint32(in.Aux))
			if x >= len(current) || in.Args.Len != 1 {
				return false
			}
			v := r.resolve(r.f.ValueIDs[in.Args.Start])
			current[x] = v
			if in.Op == OpLocalTee {
				if in.Results.Len != 1 {
					return false
				}
				r.aliases[r.f.ValueIDs[in.Results.Start]] = v
			}
		}
	}
	return true
}

func (r *localSSARenamer) resolve(v ValueID) ValueID {
	for v != InvalidValue && r.aliases[v] != v {
		v = r.aliases[v]
	}
	return v
}

func straightLineLocalCount(f *Func) int {
	n := len(f.Locals)
	for _, run := range f.LocalRuns {
		n += int(run.Count)
	}
	return n
}
