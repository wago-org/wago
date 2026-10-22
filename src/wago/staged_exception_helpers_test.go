//go:build ((linux && (amd64 || riscv64)) || ((linux || darwin) && arm64)) && !tinygo

package wago

import "testing"

func compileStagedExceptionHandling(t testing.TB, data []byte) *Compiled {
	return compileStagedExceptionHandlingFeatures(t, data, false)
}

func compileStagedExceptionHandlingFeatures(t testing.TB, data []byte, exceptionReferences bool) *Compiled {
	t.Helper()
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = exceptionReferences
	features.TailCalls = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatalf("compile staged exception handling: %v", err)
	}
	return c
}

func compileStagedExceptionHandlingFeaturesForTest(data []byte, exceptionReferences bool) (*Compiled, error) {
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = exceptionReferences
	features.TailCalls = true
	return compileWithFrontendFeatures(cfg, data, features)
}
