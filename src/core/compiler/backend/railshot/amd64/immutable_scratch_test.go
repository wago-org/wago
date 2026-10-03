//go:build amd64

package amd64

import (
	"fmt"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestImmutableIntegerCacheExcludesFixedBulkScratch(t *testing.T) {
	before := wideLoopIntConstEnabled
	wideLoopIntConstEnabled = true
	defer func() { wideLoopIntConstEnabled = before }()
	all := maskOf(R12, R13, R14, R15, R9, R10, R11, RDI, RSI)
	for _, operation := range []struct {
		name  string
		flags funcHintFlags
	}{
		{"ordinary", 0}, {"memory", hintUsesBulkMem}, {"table", hintMutatesTable},
	} {
		for _, candidate := range []Reg{RDI, RSI, R9, R10} {
			t.Run(fmt.Sprintf("%s/r%d", operation.name, candidate), func(t *testing.T) {
				h := funcHintView{loopIntConsts: &loopIntConstHintEntry{bits: [2]int64{0x123456789abcdef}, count: 1}}
				h.flags = operation.flags
				f := fn{a: &enc.Asm{}, reserved: all.remove(candidate), policy: currentCodegenPolicy()}
				f.preloadLoopIntConsts(&h)
				fixed := (candidate == RDI || candidate == RSI) && operation.flags.has(hintUsesBulkMem|hintMutatesTable) || candidate == R9 && operation.flags.has(hintMutatesTable)
				if fixed && f.iconstN != 0 {
					t.Fatalf("fixed scratch %v received immutable cache", candidate)
				}
				if !fixed && (f.iconstN != 1 || f.iconsts[0].reg != candidate) {
					t.Fatalf("safe candidate %v unexpectedly rejected", candidate)
				}
			})
		}
	}
}
