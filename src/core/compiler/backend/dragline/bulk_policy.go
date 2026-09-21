package dragline

import (
	"math"

	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
)

const (
	arm64MOPSProfileMinObservations uint64 = 10
	arm64MOPSProfileMinBytes        int64  = 65
	arm64MOPSProfileLargePercent    uint64 = 90
)

// arm64ProfileSelectsMOPS resolves one original-Wasm bulk-memory site. With
// no trustworthy size profile it prefers the feature-specific implementation;
// a sufficiently observed tiny-dominated site retains the compact baseline
// loop. A bucket crossing the threshold is conservatively counted as large.
func arm64ProfileSelectsMOPS(observations *compilerprofile.Module, function, offset uint32) bool {
	if observations == nil {
		return true
	}
	lo, hi := 0, len(observations.MemOpSizes)
	for lo < hi {
		mid := lo + (hi-lo)/2
		site := observations.MemOpSizes[mid].Site
		if site.Function < function || site.Function == function && site.Offset < offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(observations.MemOpSizes) || observations.MemOpSizes[lo].Site != (compilerprofile.Site{Function: function, Offset: offset}) {
		return true
	}
	var total, large uint64
	for _, bucket := range observations.MemOpSizes[lo].Buckets {
		total = saturatingProfileCount(total, bucket.Count)
		if bucket.High >= arm64MOPSProfileMinBytes {
			large = saturatingProfileCount(large, bucket.Count)
		}
	}
	if total < arm64MOPSProfileMinObservations {
		return true
	}
	return large >= profilePercentCeil(total, arm64MOPSProfileLargePercent)
}

func saturatingProfileCount(value, add uint64) uint64 {
	if math.MaxUint64-value < add {
		return math.MaxUint64
	}
	return value + add
}

func profilePercentCeil(value, percent uint64) uint64 {
	quotient, remainder := value/100, value%100
	result := quotient * percent
	if remainder != 0 {
		addition := (remainder*percent + 99) / 100
		if math.MaxUint64-result < addition {
			return math.MaxUint64
		}
		result += addition
	}
	return result
}
