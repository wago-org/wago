//go:build arm64

package arm64

import "os"

var crowdedProductsEnabled = os.Getenv("WAGO_ARM64_NO_CROWDED_PRODUCTS") != "1"

// Finish independent pure products after the caller has realized pending
// effects in bytecode order. This reduces live factors on a crowded operand
// stack. Exclude the current address/value root to preserve its consumer cover.
// The walk and pure arithmetic cover have fixed bounds; no trapping operation
// moves across another effect.
func (f *fn) finishCrowdedProducts() {
	var roots [64]*elem
	n := 0
	for root := f.s.back(); root != f.s.head && root != nil && n < len(roots); root = f.s.prev(f.s.baseOfValentBlock(root)) {
		roots[n] = root
		n++
	}
	if n < 2 {
		return
	}
	for i := n - 1; i > 0; i-- {
		root := roots[i]
		if root.isDeferred() && root.deferredOp() == opMul && pureMulAddOperand(f.s, root, 4) {
			f.materialize(root)
			f.stats.peep("crowded-product-finish")
		}
	}
}
