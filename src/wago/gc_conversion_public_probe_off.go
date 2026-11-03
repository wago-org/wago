//go:build !wago_test_conversion_hooks

package wago

// These empty functions inline away outside the dedicated concurrency test build.
func conversionPublicFalseObservation(*Instance) {}
func conversionPublicEnablementObservation()     {}
