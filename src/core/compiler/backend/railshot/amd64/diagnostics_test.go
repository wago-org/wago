//go:build amd64

package amd64

import "testing"

// Statistics assertions require a diagnostic build. CI runs both the ordinary
// runtime suite and this suite with wago_codegenstats so neither path is hidden.
func requireCompilerDiagnostics(t testing.TB) {
	t.Helper()
	if !diagnosticsEnabled {
		t.Skip("compiler diagnostics require -tags=wago_codegenstats (also enabled by wago_profile)")
	}
}

// optionalTestStats keeps semantic execution checks active in ordinary builds.
// Callers guard only the counter assertions with diagnosticsEnabled.
func optionalTestStats(stats *ModuleStats) *ModuleStats {
	if diagnosticsEnabled {
		return stats
	}
	return nil
}
