package shared

import "os"

// SumExperiment is a process-wide, opt-in control for the fixed-base experiment.
// Empty or invalid values retain the existing implementation byte for byte.
var SumExperiment = os.Getenv("WAGO_LOOP_SUM_EXPERIMENT")

func SumExperimentShape() (factor, accumulators int, enabled bool) {
	switch SumExperiment {
	case "A", "B":
		return 1, 1, true
	case "C":
		return 2, 1, true
	case "D":
		return 4, 1, true
	case "E":
		return 2, 2, true
	case "F":
		return 4, 4, true
	case "G":
		return 4, 2, true
	}
	return 4, 4, false
}

// ReductionForms enables only the bounded additional integer-reduction forms.
var ReductionForms = os.Getenv("WAGO_LOOP_REDUCTION_FORMS") == "1"
