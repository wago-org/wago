//go:build arm64

package arm64

import "testing"

func TestPureReducePairAccumulateExactness(t *testing.T) {
	old := pureReduceAccumulateEnabled
	defer func() { pureReduceAccumulateEnabled = old }()
	for _, on := range []bool{false, true} {
		pureReduceAccumulateEnabled = on
		t.Run(map[bool]string{false: "two-instruction", true: "accumulate"}[on], func(t *testing.T) {
			TestPureReduceIndependentExecution(t)
			TestPureReducePrecisionAdmission(t)
		})
	}
}
