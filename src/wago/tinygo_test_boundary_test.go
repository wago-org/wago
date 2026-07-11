//go:build tinygo

package wago

import "testing"

func requireStandardGoTestRuntime(t *testing.T) bool {
	t.Helper()
	t.Log("test depends on standard Go testing control flow or struct layout")
	return false
}
