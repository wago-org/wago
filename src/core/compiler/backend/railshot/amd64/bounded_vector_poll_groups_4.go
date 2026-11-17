//go:build amd64 && !wago_experimental_vector_poll8

package amd64

// This constant affects only the explicitly enabled bounded-vector experiment.
// It bounds completed vector groups between checks, not elapsed wall time.
const boundedVectorPollGroups = 4
