//go:build amd64 && wago_experimental_linear_sum_outlined_poll

package amd64

import "os"

var linearSumPollOutline = parseLinearSumPollOutline(os.Getenv("WAGO_AMD64_SUM_POLL_OUTLINE"))

func parseLinearSumPollOutline(s string) bool {
	switch s {
	case "", "0":
		return false
	case "1":
		return true
	default:
		panic("WAGO_AMD64_SUM_POLL_OUTLINE must be 0 or 1")
	}
}
