package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ScalarResultBank names one finite internal-ABI result register bank.
type ScalarResultBank uint8

const (
	ScalarResultInvalid ScalarResultBank = iota
	ScalarResultGP
	ScalarResultFP
)

// ScalarResultLocation maps one Wasm result to an index in its target's GP or
// FP result-register bank. The target supplies the physical register arrays.
type ScalarResultLocation struct {
	Bank  ScalarResultBank
	Index uint8
}

// ScalarResultPlan is deliberately fixed-capacity: register ABI v2 admits at
// most two scalar results per bank and four total. Every other signature keeps
// the wrapper/slot ABI.
type ScalarResultPlan struct {
	Locations [4]ScalarResultLocation
	Count     uint8
	GP        uint8
	FP        uint8
}

func PlanScalarResults(results []wasm.ValType) (ScalarResultPlan, bool) {
	var p ScalarResultPlan
	if len(results) > len(p.Locations) {
		return p, false
	}
	p.Count = uint8(len(results))
	for i, typ := range results {
		loc := &p.Locations[i]
		switch {
		case wasm.EqualValType(typ, wasm.I32), wasm.EqualValType(typ, wasm.I64):
			if p.GP == 2 {
				return ScalarResultPlan{}, false
			}
			loc.Bank, loc.Index = ScalarResultGP, p.GP
			p.GP++
		case wasm.EqualValType(typ, wasm.F32), wasm.EqualValType(typ, wasm.F64):
			if p.FP == 2 {
				return ScalarResultPlan{}, false
			}
			loc.Bank, loc.Index = ScalarResultFP, p.FP
			p.FP++
		default:
			return ScalarResultPlan{}, false
		}
	}
	return p, true
}
