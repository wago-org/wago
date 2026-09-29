//go:build arm64

package arm64

import "testing"

// Statistics assertions require a diagnostic build. CI runs both the ordinary
// runtime suite and this suite with wago_codegenstats so neither path is hidden.
func requireCompilerDiagnostics(t testing.TB) {
	t.Helper()
	if !diagnosticsEnabled {
		t.Skip("compiler diagnostics require -tags=wago_codegenstats (also enabled by wago_profile or wago_gcstats)")
	}
}
