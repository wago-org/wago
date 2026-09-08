//go:build !wago_gcstats

package gc

import (
	"errors"
	"io"
)

// WriteJSON requires the opt-in collector telemetry build.
func (BenchmarkTelemetryReport) WriteJSON(io.Writer) error {
	return errors.New("gc: telemetry JSON requires the wago_gcstats build tag")
}
