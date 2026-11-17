//go:build amd64 && wago_experimental_linear_sum_poll64

package amd64

// Bounded guest loads, not a wall-time cancellation guarantee.
// Only the opt-in cooperative exact-sum consumer uses this policy.
const linearSumPollLoads = 64
