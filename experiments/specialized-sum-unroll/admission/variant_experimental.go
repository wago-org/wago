//go:build amd64 && wago_sumunroll

package main

import _ "unsafe"

//go:linkname setSumUnrollMeasurement github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64.setSumUnrollMeasurement
func setSumUnrollMeasurement(factor int, hybrid bool, threshold int)

//go:linkname setSumUnrollMitigation github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64.setSumUnrollMitigation
func setSumUnrollMitigation(pairTail, reserve bool)

func selectVariant(v string) {
	switch v {
	case "baseline":
	case "D", "DR":
		setSumUnrollMeasurement(16, false, 0)
		setSumUnrollMitigation(false, v == "DR")
	default:
		panic("unknown variant")
	}
}
