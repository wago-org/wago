//go:build !wagodebug

package gc

const failureInjectionEnabled = false

func injectFailure(any, failurePoint) error { return nil }
func stressFullCollection() bool            { return false }
func isInjectedFailure(error) bool          { return false }
