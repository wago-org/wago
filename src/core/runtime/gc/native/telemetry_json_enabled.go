//go:build wago_gcstats

package gc

import (
	"encoding/json"
	"io"
)

// WriteJSON writes one newline-terminated machine-readable report.
func (r BenchmarkTelemetryReport) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
