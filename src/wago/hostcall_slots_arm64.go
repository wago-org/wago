//go:build arm64 && !tinygo

package wago

import "fmt"

// Pass the borrowed view by address to avoid copying its eight ABI slots
// again in the checked scalar accessor. The view remains callback-scoped.
func (c *HostCall) paramSlotIndex(i int, want ValType) int {
	if i == 0 && c.sig != nil && len(c.sig.Params) != 0 {
		got := c.sig.Params[0]
		if got != want {
			panic(fmt.Sprintf("wago: host parameter %d is %s, not %s", i, got, want))
		}
		return 0
	}
	got := c.paramType(i)
	if got != want {
		panic(fmt.Sprintf("wago: host parameter %d is %s, not %s", i, got, want))
	}
	// Equal logical and physical counts mean every value occupies one slot.
	// Read the current view instead of caching offsets in public signatures.
	if c.params.len() == len(c.sig.Params) {
		return i
	}
	return hostCallSlot(c.sig.Params, i)
}

func (c *HostCall) resultSlotIndex(i int, want ValType) int {
	if i == 0 && c.sig != nil && len(c.sig.Results) != 0 {
		got := c.sig.Results[0]
		if got != want {
			panic(fmt.Sprintf("wago: host result %d is %s, not %s", i, got, want))
		}
		return 0
	}
	got := c.resultType(i)
	if got != want {
		panic(fmt.Sprintf("wago: host result %d is %s, not %s", i, got, want))
	}
	if c.results.len() == len(c.sig.Results) {
		return i
	}
	return hostCallSlot(c.sig.Results, i)
}
