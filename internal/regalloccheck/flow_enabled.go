//go:build wago_regalloccheck

package regalloccheck

import "fmt"

// ValueID names an independent semantic definition, not an allocator owner.
// IDs are one-based indices into Graph.Widths and remain stable during analysis.
type ValueID uint32

type OpKind uint8

const (
	Machine OpKind = iota
	Define
	Use
	Unsupported
)

// Operation separates emitted effects from the semantic contract. Define is
// trusted only after all of its input Uses; moves and reloads must use Machine.
type Operation struct {
	Kind     OpKind
	Effect   Effect
	Location Location
	Value    ValueID
	Where    string
}

type Binding struct {
	Location Location
	Value    ValueID
}

// Parameter assigns a successor's semantic name after the physical edge moves.
// Multiple parameters can receive the same source; assignments are simultaneous.
type Parameter struct {
	From, To ValueID
	Location Location // actual outgoing carrier, after the edge's physical moves
}
type Edge struct {
	To         int
	Parameters []Parameter
}
type Block struct {
	Operations []Operation
	Edges      []Edge
}

// Graph is a checked-build analysis input, not production compiler IR. Inputs
// are its only entry assumptions. Clients must also bound graph construction.
type Graph struct {
	Widths []uint8
	Blocks []Block
	Entry  int
	Inputs []Binding
}

type Verdict uint8

const (
	Inconclusive Verdict = iota
	Verified
	Rejected
)

// Result never treats an unsupported operation or exhausted budget as success.
// Rejected means a use contract was not proved, including entirely unknown bytes.
type FailureReason uint8

const (
	NoFailure FailureReason = iota
	UnknownInput
	ProvenanceMismatch
	UnsupportedOperation
	InvalidGraph
	ResourceLimit
)

type Result struct {
	Reason    FailureReason
	Verdict   Verdict
	Block     int
	Operation int
	Message   string
	Work      int // metered analysis units, not elapsed time; tied limits may stop at different units
}

// Limits bound retained storage credits and total work, including validation and the
// final use pass. Deleted map entries keep storage credits until image release. Zero selects the default; negative limits are invalid.
type Limits struct {
	Blocks, Values, Operations, Facts, Work int
}

func DefaultLimits() Limits {
	return Limits{Blocks: 4096, Values: 65536, Operations: 262144, Facts: 1048576, Work: 33554432}
}

type flowLimit struct{}
type flowBudget struct {
	limits      Limits
	work, facts int
}

func (b *flowBudget) charge(n int) {
	if n > b.limits.Work-b.work {
		panic(flowLimit{})
	}
	b.work += n
}

func (b *flowBudget) addFact() {
	b.charge(1)
	if b.facts == b.limits.Facts {
		panic(flowLimit{})
	}
	b.facts++
}

type symbol struct {
	id   ValueID
	part uint8
}

// Most bytes have one identity. Allocate a set only when semantic aliases meet.
type flowCell struct {
	one         symbol
	more        map[symbol]struct{}
	moreHistory int // retained alias-map capacity survives deletions
}

func (c flowCell) empty() bool { return c.one.id == 0 && len(c.more) == 0 }
func (c flowCell) each(b *flowBudget, fn func(symbol)) {
	if c.one.id != 0 {
		fn(c.one)
	}
	if len(c.more) == 0 {
		return
	}
	b.charge(c.moreHistory)
	for v := range c.more {
		fn(v)
	}
}

type flowIndex struct {
	locations map[Location]uint16
	history   int // retained per-value map capacity survives deletions
}

// The reverse index makes semantic redefinition proportional to that value's
// aliases, rather than scanning the entire frame at every definition.
type flowImage struct {
	cells      map[Location]flowCell
	registers  map[Location]struct{}
	ids        map[ValueID]flowIndex
	count      int
	retained   int // storage credits are reclaimed only when the image is released
	regHistory int // register-map scans must not charge unrelated frame history
	b          *flowBudget
}

func newImage(b *flowBudget) *flowImage {
	return &flowImage{cells: make(map[Location]flowCell), registers: make(map[Location]struct{}), ids: make(map[ValueID]flowIndex), b: b}
}

func (s *flowImage) has(loc Location, v symbol) bool {
	s.b.charge(1)
	c := s.cells[loc]
	if c.one == v {
		return true
	}
	_, ok := c.more[v]
	return ok
}

func (s *flowImage) add(loc Location, v symbol) {
	if s.has(loc, v) {
		return
	}
	s.b.addFact()
	c := s.cells[loc]
	if c.one.id == 0 {
		c.one = v
	} else {
		if c.more == nil {
			c.more = make(map[symbol]struct{})
		}
		c.more[v] = struct{}{}
		c.moreHistory++
	}
	s.cells[loc] = c
	if loc.Bank != Frame {
		s.registers[loc] = struct{}{}
		s.regHistory++
	}
	index := s.ids[v.id]
	if index.locations == nil {
		index.locations = make(map[Location]uint16)
	}
	index.locations[loc] |= 1 << v.part
	index.history++
	s.ids[v.id] = index
	s.count++
	s.retained++
}

func (s *flowImage) remove(loc Location, v symbol) {
	s.b.charge(1)
	c := s.cells[loc]
	if c.one == v {
		c.one = symbol{}
	} else {
		delete(c.more, v)
	}
	if c.empty() {
		delete(s.cells, loc)
		delete(s.registers, loc)
	} else {
		s.cells[loc] = c
	}
	index := s.ids[v.id]
	index.locations[loc] &^= 1 << v.part
	if index.locations[loc] == 0 {
		delete(index.locations, loc)
	}
	if len(index.locations) == 0 {
		delete(s.ids, v.id)
	}
	s.count--
	// Go maps retain buckets after deletion. Keeping the storage credit until
	// release bounds historical capacity, not merely currently visible facts.
}

func (s *flowImage) clear(loc Location, size int) {
	for i := 0; i < size; i++ {
		s.b.charge(1)
		at := loc.next(i)
		s.cells[at].each(s.b, func(v symbol) { s.remove(at, v) })
	}
}

func (s *flowImage) forget(id ValueID) {
	index := s.ids[id]
	if len(index.locations) == 0 {
		return
	}
	s.b.charge(index.history)
	for loc, parts := range index.locations {
		for part := uint8(0); part < 16; part++ {
			s.b.charge(1)
			if parts&(1<<part) != 0 {
				s.remove(loc, symbol{id, part})
			}
		}
	}
}

func (s *flowImage) release() {
	s.b.facts -= s.retained
	s.cells, s.ids, s.registers, s.count, s.retained = nil, nil, nil, 0, 0
	s.regHistory = 0
}

func (s *flowImage) clone() *flowImage {
	out := newImage(s.b)
	if len(s.cells) == 0 {
		return out
	}
	s.b.charge(s.retained)
	for loc, values := range s.cells {
		values.each(s.b, func(v symbol) { out.add(loc, v) })
	}
	return out
}

func (s *flowImage) meet(other *flowImage) bool {
	changed := false
	if len(s.cells) == 0 {
		return false
	}
	s.b.charge(s.retained)
	for loc, values := range s.cells {
		values.each(s.b, func(v symbol) {
			if !other.has(loc, v) {
				s.remove(loc, v)
				changed = true
			}
		})
	}
	return changed
}

type flowFact struct {
	loc Location
	v   symbol
}

func (s *flowImage) snapshot(src, dst Location, size int, facts []flowFact) []flowFact {
	for i := 0; i < size; i++ {
		s.b.charge(1)
		s.cells[src.next(i)].each(s.b, func(v symbol) {
			// Snapshots count against the same live-memory budget as states.
			s.b.addFact()
			facts = append(facts, flowFact{dst.next(i), v})
		})
	}
	return facts
}

func (s *flowImage) restore(facts []flowFact) {
	for _, f := range facts {
		s.add(f.loc, f.v)
	}
	s.b.facts -= len(facts)
}

func (s *flowImage) effect(e Effect) {
	switch e.Kind {
	case Copy, Swap:
		var inline [32]flowFact
		facts := s.snapshot(e.Src, e.Dst, e.Size, inline[:0])
		if e.Kind == Swap {
			facts = s.snapshot(e.Dst, e.Src, e.Size, facts)
			s.clear(e.Src, e.Size)
			if e.Src.Bank == GP && e.Size == 4 {
				s.clear(e.Src.next(4), 4)
			}
		}
		s.clear(e.Dst, e.Size)
		if e.Dst.Bank == GP && e.Size == 4 {
			s.clear(e.Dst.next(4), 4)
		}
		s.restore(facts)
	case Kill:
		s.clear(e.Dst, e.Size)
	case Call:
		if len(s.registers) == 0 {
			return
		}
		s.b.charge(s.regHistory)
		for loc := range s.registers {
			s.cells[loc].each(s.b, func(v symbol) { s.remove(loc, v) })
		}
	case Read:
		return
	}
	if e.ClearTo > e.Size {
		s.clear(e.Dst.next(e.Size), e.ClearTo-e.Size)
	}
}

func (s *flowImage) define(loc Location, id ValueID, width int) {
	// Repeated loop definitions must not leave the previous iteration's aliases.
	s.forget(id)
	s.clear(loc, width)
	for i := 0; i < width; i++ {
		s.add(loc.next(i), symbol{id, uint8(i)})
	}
}

func (s *flowImage) parameters(params []Parameter) {
	var facts []flowFact
	for _, p := range params {
		s.b.charge(1)
		index := s.ids[p.From]
		if len(index.locations) == 0 {
			continue
		}
		s.b.charge(index.history)
		for loc, parts := range index.locations {
			for part := uint8(0); part < 16; part++ {
				s.b.charge(1)
				if parts&(1<<part) != 0 {
					s.b.addFact()
					facts = append(facts, flowFact{loc, symbol{p.To, part}})
				}
			}
		}
	}
	for _, p := range params {
		s.b.charge(1)
		s.forget(p.To)
	}
	s.restore(facts)
}

func resolveLimits(l Limits) (Limits, bool) {
	d := DefaultLimits()
	for _, pair := range [][2]*int{{&l.Blocks, &d.Blocks}, {&l.Values, &d.Values}, {&l.Operations, &d.Operations}, {&l.Facts, &d.Facts}, {&l.Work, &d.Work}} {
		if *pair[0] < 0 {
			return l, false
		}
		if *pair[0] == 0 {
			*pair[0] = *pair[1]
		}
	}
	return l, true
}

func flowLocation(loc Location, size int, unknown bool) bool {
	if size < 1 || size > 16 {
		return false
	}
	switch loc.Bank {
	case GP:
		return loc.Index >= 0 && loc.Index < 32 && int(loc.Byte)+size <= 8
	case FP:
		return loc.Index >= 0 && loc.Index < 32 && int(loc.Byte)+size <= 16
	case Frame:
		return loc.Byte == 0 && int64(loc.Index)+int64(size)-1 <= 2147483647
	case Unknown:
		return unknown
	}
	return false
}

func partialOverlap(a, b Location, size int) bool {
	if a.Bank != b.Bank || a == b {
		return false
	}
	x, y := int64(a.Byte), int64(b.Byte)
	if a.Bank == Frame {
		x, y = int64(a.Index), int64(b.Index)
	} else if a.Index != b.Index {
		return false
	}
	return x < y+int64(size) && y < x+int64(size)
}

func (g *Graph) width(id ValueID) int {
	if id == 0 || uint64(id) > uint64(len(g.Widths)) {
		return 0
	}
	return int(g.Widths[id-1])
}

func (g *Graph) validate(b *flowBudget) string {
	if len(g.Blocks) == 0 || len(g.Blocks) > b.limits.Blocks || g.Entry < 0 || g.Entry >= len(g.Blocks) {
		return "invalid block count or entry"
	}
	if len(g.Widths) > b.limits.Values {
		return "semantic value limit"
	}
	for _, width := range g.Widths {
		b.charge(1)
		if width == 0 || width > 16 {
			return "invalid semantic width"
		}
	}
	remaining := b.limits.Operations
	consume := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		b.charge(n)
		return true
	}
	if !consume(len(g.Inputs)) {
		return "operation limit"
	}
	for _, input := range g.Inputs {
		if !flowLocation(input.Location, g.width(input.Value), false) {
			return "invalid input binding"
		}
	}
	for _, block := range g.Blocks {
		b.charge(1)
		if !consume(len(block.Operations)) || !consume(len(block.Edges)) {
			return "operation limit"
		}
		for _, op := range block.Operations {
			switch op.Kind {
			case Define, Use:
				if !flowLocation(op.Location, g.width(op.Value), false) {
					return "invalid semantic operation"
				}
			case Machine:
				e := op.Effect
				if e.Kind == Call {
					if e.Size != 0 || e.ClearTo != 0 {
						return "invalid call effect"
					}
					continue
				}
				if e.Size < 1 || e.Size > 16 || e.Kind > Read || (e.Kind == Read && e.ClearTo != 0) || e.ClearTo < 0 || (e.ClearTo != 0 && e.ClearTo < e.Size) {
					return "invalid machine effect"
				}
				if e.Kind != Read && !flowLocation(e.Dst, max(e.Size, e.ClearTo), false) {
					return "invalid machine destination"
				}
				if (e.Kind == Copy || e.Kind == Swap || e.Kind == Read) && !flowLocation(e.Src, e.Size, e.Kind != Swap) {
					return "invalid machine source"
				}
				if (e.Kind == Copy || e.Kind == Swap) && e.Size == 4 && ((e.Dst.Bank == GP && e.Dst.Byte != 0) || (e.Kind == Swap && e.Src.Bank == GP && e.Src.Byte != 0)) {
					return "invalid GP32 register slice"
				}
				if e.Kind == Swap && (e.ClearTo != 0 || partialOverlap(e.Dst, e.Src, e.Size)) {
					return "invalid swap clear width or overlap"
				}
			case Unsupported:
			default:
				return "invalid operation kind"
			}
		}
		for _, edge := range block.Edges {
			if edge.To < 0 || edge.To >= len(g.Blocks) {
				return "invalid edge"
			}
			if !consume(len(edge.Parameters)) {
				return "operation limit"
			}
			seen := make(map[ValueID]bool)
			for _, p := range edge.Parameters {
				if g.width(p.From) == 0 || g.width(p.From) != g.width(p.To) || seen[p.To] || !flowLocation(p.Location, g.width(p.From), false) {
					return "invalid or duplicate edge parameter"
				}
				seen[p.To] = true
			}
		}
	}
	return ""
}

func (g *Graph) run(s *flowImage, block int, check bool) *Result {
	for i, op := range g.Blocks[block].Operations {
		s.b.charge(1)
		switch op.Kind {
		case Machine:
			s.effect(op.Effect)
		case Define:
			s.define(op.Location, op.Value, g.width(op.Value))
		case Use:
			if check {
				if failure := g.check(s, op.Location, op.Value, op.Where, block, i); failure != nil {
					return failure
				}
			}
		case Unsupported:
			return &Result{Verdict: Inconclusive, Reason: UnsupportedOperation, Block: block, Operation: i, Message: "unsupported: " + op.Where}
		}
	}
	return nil
}

func (g *Graph) check(s *flowImage, loc Location, value ValueID, where string, block, op int) *Result {
	for part := 0; part < g.width(value); part++ {
		at := loc.next(part)
		if !s.has(at, symbol{value, uint8(part)}) {
			reason, why := ProvenanceMismatch, "unproven provenance"
			if s.cells[at].empty() {
				reason, why = UnknownInput, "unknown value"
			}
			return &Result{Verdict: Rejected, Reason: reason, Block: block, Operation: op, Message: fmt.Sprintf("%s: %s at %v byte %d", where, why, loc, part)}
		}
	}
	return nil
}

// Verify computes the descending must-fact fixed point before checking uses.
// Unvisited blocks are distinct from reachable blocks with no known facts.
// The entry boundary remains an incoming edge even when entry has a backedge.
func (g *Graph) Verify(limits Limits) (result Result) {
	result.Block, result.Operation = -1, -1
	resolved, ok := resolveLimits(limits)
	if !ok {
		result.Verdict, result.Reason, result.Message = Inconclusive, InvalidGraph, "invalid analysis limits"
		return
	}
	b := &flowBudget{limits: resolved}
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(flowLimit); ok {
				result = Result{Verdict: Inconclusive, Reason: ResourceLimit, Block: -1, Operation: -1, Message: "analysis resource limit"}
			} else {
				panic(failure)
			}
		}
		result.Work = b.work
	}()
	if why := g.validate(b); why != "" {
		result.Verdict, result.Reason, result.Message = Inconclusive, InvalidGraph, why
		if why == "semantic value limit" || why == "operation limit" || len(g.Blocks) > resolved.Blocks {
			result.Reason = ResourceLimit
		}
		return
	}
	in := make([]*flowImage, len(g.Blocks))
	in[g.Entry] = newImage(b)
	for _, input := range g.Inputs {
		for part := 0; part < g.width(input.Value); part++ {
			in[g.Entry].add(input.Location.next(part), symbol{input.Value, uint8(part)})
		}
	}
	// A bounded ring holds each block at most once, including self-edges.
	queue := make([]int, len(g.Blocks))
	queued := make([]bool, len(g.Blocks))
	head, tail, count := 0, 0, 0
	enqueue := func(block int) {
		if !queued[block] {
			queue[tail] = block
			tail = (tail + 1) % len(queue)
			count++
			queued[block] = true
		}
	}
	enqueue(g.Entry)
	for count != 0 {
		b.charge(1)
		block := queue[head]
		head = (head + 1) % len(queue)
		count--
		queued[block] = false
		out := in[block].clone()
		if failure := g.run(out, block, false); failure != nil {
			return *failure
		}
		for _, edge := range g.Blocks[block].Edges {
			b.charge(1)
			next := out
			if len(edge.Parameters) != 0 {
				next = out.clone()
				next.parameters(edge.Parameters)
			}
			if in[edge.To] == nil {
				in[edge.To] = next.clone()
				enqueue(edge.To)
			} else if in[edge.To].meet(next) {
				enqueue(edge.To)
			}
			if next != out {
				next.release()
			}
		}
		out.release()
	}
	for block, entry := range in {
		b.charge(1)
		if entry == nil {
			continue
		}
		out := entry.clone()
		if failure := g.run(out, block, true); failure != nil {
			return *failure
		}
		for edgeIndex, edge := range g.Blocks[block].Edges {
			b.charge(1)
			for parameterIndex, p := range edge.Parameters {
				b.charge(1)
				where := fmt.Sprintf("edge %d parameter %d", edgeIndex, parameterIndex)
				if failure := g.check(out, p.Location, p.From, where, block, len(g.Blocks[block].Operations)); failure != nil {
					return *failure
				}
			}
		}
		out.release()
	}
	result.Verdict = Verified
	return
}
