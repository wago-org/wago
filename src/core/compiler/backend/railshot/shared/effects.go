package shared

// FuncEffects is the compact semantic-effect vocabulary shared by Railshot
// targets. Add bits only when a lowering consumer can exploit them; unknown
// calls use AllFuncEffects and therefore remain conservative.
type FuncEffects uint8

const (
	EffectGrowsMemory FuncEffects = 1 << iota
	EffectWritesGlobals
)

const AllFuncEffects = EffectGrowsMemory | EffectWritesGlobals

// FuncEffectCollector records direct effects and a bounded CSR direct-call
// graph during a target's existing module scan. Once either cap is exceeded it
// conservatively classifies every call-making function while retaining exact
// leaf effects.
type FuncEffectCollector struct {
	imported int
	direct   []FuncEffects
	starts   []uint32
	calls    []uint32
	current  int
	maxCalls int
	overflow bool
}

func NewFuncEffectCollector(functions, imported, callCap, maxFunctions, maxCalls int) *FuncEffectCollector {
	c := &FuncEffectCollector{imported: imported, direct: make([]FuncEffects, functions), maxCalls: maxCalls}
	if functions > maxFunctions {
		c.overflow = true
		return c
	}
	if callCap < 0 || callCap > maxCalls {
		callCap = maxCalls
	}
	c.starts = make([]uint32, functions+1)
	c.calls = make([]uint32, 0, callCap)
	return c
}

func (c *FuncEffectCollector) Begin(caller int) {
	if c != nil && !c.overflow && caller >= 0 && caller < len(c.direct) {
		c.current = caller
		c.starts[caller] = uint32(len(c.calls))
	}
}

func (c *FuncEffectCollector) Mark(caller int, effect FuncEffects) {
	if c != nil && caller >= 0 && caller < len(c.direct) {
		c.direct[caller] |= effect
	}
}

func (c *FuncEffectCollector) Call(caller int, globalTarget uint32) {
	if c == nil || caller < 0 || caller >= len(c.direct) {
		return
	}
	local := int64(globalTarget) - int64(c.imported)
	if local < 0 || local >= int64(len(c.direct)) {
		c.direct[caller] |= AllFuncEffects
		return
	}
	if c.overflow {
		c.direct[caller] |= AllFuncEffects
		return
	}
	if len(c.calls) == c.maxCalls {
		for i := 0; i < c.current; i++ {
			if c.starts[i] != c.starts[i+1] {
				c.direct[i] |= AllFuncEffects
			}
		}
		c.direct[caller] |= AllFuncEffects
		c.overflow = true
		c.starts = nil
		c.calls = nil
		return
	}
	c.calls = append(c.calls, uint32(local))
}

func (c *FuncEffectCollector) Finish() []FuncEffects {
	if c == nil {
		return nil
	}
	if c.overflow {
		return c.direct
	}
	c.starts[len(c.direct)] = uint32(len(c.calls))
	return PropagateFuncEffects(c.direct, c.starts, c.calls)
}

// PropagateFuncEffects computes the transitive union of direct function effects
// in direct. It reuses starts as bounded worklist storage after consuming the
// forward CSR; calls is retained unchanged.
// starts is CSR-style: calls[starts[i]:starts[i+1]] are local callees of function
// i. Unknown/imported/indirect calls must already contribute AllFuncEffects to
// direct. The reverse worklist is O(functions + direct calls) per effect bit and
// handles recursive SCCs without retaining a CFG.
func PropagateFuncEffects(direct []FuncEffects, starts []uint32, calls []uint32) []FuncEffects {
	n := len(direct)
	if n == 0 {
		return direct
	}
	if len(starts) != n+1 {
		for i := range direct {
			direct[i] |= AllFuncEffects
		}
		return direct
	}

	// Count only edges from well-formed caller ranges. offsets is converted to
	// prefix sums, then temporarily used as the fill cursor and restored in
	// place. This avoids separate count and cursor arrays.
	offsets := make([]uint32, n+1)
	for caller := 0; caller < n; caller++ {
		start, end := starts[caller], starts[caller+1]
		if start > end || int(end) > len(calls) {
			direct[caller] |= AllFuncEffects
			continue
		}
		for _, callee := range calls[start:end] {
			if int(callee) >= n {
				direct[caller] |= AllFuncEffects
				continue
			}
			offsets[callee+1]++
		}
	}
	for i := 0; i < n; i++ {
		offsets[i+1] += offsets[i]
	}
	reverse := make([]uint32, offsets[n])
	for caller := 0; caller < n; caller++ {
		start, end := starts[caller], starts[caller+1]
		if start > end || int(end) > len(calls) {
			continue
		}
		for _, callee := range calls[start:end] {
			if int(callee) >= n {
				continue
			}
			reverse[offsets[callee]] = uint32(caller)
			offsets[callee]++
		}
	}
	for i := n; i > 0; i-- {
		offsets[i] = offsets[i-1]
	}
	offsets[0] = 0

	// A function acquires each bit at most once, so starts' n+1 words are a
	// sufficient work queue for each pass after the forward graph is consumed.
	for bit := FuncEffects(1); bit != 0; bit <<= 1 {
		if AllFuncEffects&bit == 0 {
			continue
		}
		queue := starts[:0]
		for i, effect := range direct {
			if effect&bit != 0 {
				queue = append(queue, uint32(i))
			}
		}
		for head := 0; head < len(queue); head++ {
			callee := queue[head]
			for _, caller := range reverse[offsets[callee]:offsets[callee+1]] {
				if direct[caller]&bit == 0 {
					direct[caller] |= bit
					queue = append(queue, caller)
				}
			}
		}
	}
	return direct
}
