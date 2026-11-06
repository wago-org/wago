//go:build wago_test_conversion_hooks

package wago

// Test hooks are installed before goroutine launch and removed after joins.
// The dedicated build tag keeps hook checks out of normal execution.
var conversionPublicFalseHook func(*Instance)
var conversionPublicEnableHook func()

func conversionPublicFalseObservation(in *Instance) {
	if conversionPublicFalseHook != nil {
		conversionPublicFalseHook(in)
	}
}
func conversionPublicEnablementObservation() {
	if conversionPublicEnableHook != nil {
		conversionPublicEnableHook()
	}
}
